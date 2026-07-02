package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime/debug"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/desktop"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
)

// recoverGoroutine 在 goroutine 中捕获 panic 并写入日志
func recoverGoroutine(name string) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		logger.Error("PANIC in %s: %v\n%s", name, r, stack)
		logger.Sync()
		crashFile, err := os.OpenFile("log/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			fmt.Fprintf(crashFile, "PANIC in %s: %v\n%s\n", name, r, stack)
			crashFile.Close()
		}
	}
}

// DesktopMode 桌面模式 UI 管理器
type DesktopMode struct {
	mainWindow *walk.MainWindow
	container  *walk.Composite // 无布局容器，用于绝对定位
	manager    *group.Manager
	executor   *ProgramExecutor
	winAPI     *desktop.WindowsAPI
	lifecycle  *LifecycleManager
	cards      []*GroupCard
	bodyWidget *walk.CustomWidget

	screenW      int
	screenH      int
	workX        int
	workY        int
	workW        int
	workH        int
	wallpaperBmp *walk.Bitmap // 缓存的壁纸 bitmap

	hoveredFreeIdx int // 当前悬停的未分组图标索引

	// 图标拖拽协调（跨卡片拖放）
	iconDragActive      bool
	iconDragSourceCard  *GroupCard
	iconDragItem        group.GroupItem
	iconDragSourceGroup string
	iconDragScreenX     int
	iconDragScreenY     int
	dropTargetCard      *GroupCard
	dropInsertIdx       int
	dropToDesktop       bool

	// 未分组图标拖拽状态
	freeItemDragActive    bool
	freeItemDragIdx       int
	freeItemDragItem      group.GroupItem
	freeItemDragPressed   bool
	freeItemDragStartTime time.Time
	freeItemDragStartX    int
	freeItemDragStartY    int
	freeItemDragMouseX    int
	freeItemDragMouseY    int

	// 拖拽 ghost 缓存（避免每次重绘重新提取图标）
	ghostBmp *walk.Bitmap
}

// NewDesktopMode 创建桌面模式
func NewDesktopMode(mw *walk.MainWindow, mgr *group.Manager, winAPI *desktop.WindowsAPI, lifecycle *LifecycleManager) *DesktopMode {
	dm := &DesktopMode{
		mainWindow: mw,
		manager:    mgr,
		executor:   NewProgramExecutor(),
		winAPI:     winAPI,
		lifecycle:  lifecycle,
		hoveredFreeIdx: -1,
	}
	dm.screenW, dm.screenH = winAPI.GetScreenSize()
	left, top, right, bottom := winAPI.GetWorkAreaRect()
	dm.workX = left
	dm.workY = top
	dm.workW = right - left
	dm.workH = bottom - top
	logger.Debug("screen=%dx%d, workArea=(%d,%d,%d,%d), workSize=%dx%d",
		dm.screenW, dm.screenH, left, top, right, bottom, dm.workW, dm.workH)
	return dm
}

// Setup 设置桌面模式 UI
func (dm *DesktopMode) Setup() error {
	logger.Debug("Setup: DPI=%d, bounds=(%d,%d,%dx%d)",
		dm.mainWindow.DPI(), dm.workX, dm.workY, dm.workW, dm.workH)

	// 设置主窗口尺寸为工作区
	dm.mainWindow.SetBoundsPixels(walk.Rectangle{
		X: dm.workX, Y: dm.workY,
		Width: dm.workW, Height: dm.workH,
	})

	// 设置窗口背景色为深色（消除白边）
	bg, _ := walk.NewSolidColorBrush(walk.RGB(0x1A, 0x1A, 0x2E))
	dm.mainWindow.SetBackground(bg)

	// 预加载壁纸
	dm.loadWallpaper()

	// 创建一个 Composite 容器放在 VBox 中，让它占满空间
	var err error
	dm.container, err = walk.NewComposite(dm.mainWindow)
	if err != nil {
		return err
	}

	// 让 container 在 VBox 中占满剩余空间
	if layout, ok := dm.mainWindow.Layout().(*walk.BoxLayout); ok {
		layout.SetStretchFactor(dm.container, 1)
	}

	// container 使用 VBox（margins/spacing 为0），walk 要求所有 Container 必须有 Layout
	containerLayout := walk.NewVBoxLayout()
	containerLayout.SetMargins(walk.Margins{})
	containerLayout.SetSpacing(0)
	dm.container.SetLayout(containerLayout)

	// 创建主绘制区域（背景 + 壁纸 + 卡片内容）放在 container 中
	dm.bodyWidget, err = walk.NewCustomWidgetPixels(dm.container, 0, dm.paintDesktop)
	if err != nil {
		return err
	}
	dm.bodyWidget.SetPaintMode(walk.PaintBuffered)
	dm.bodyWidget.SetInvalidatesOnResize(true)

	// bodyWidget 占满 container
	containerLayout.SetStretchFactor(dm.bodyWidget, 1)

	// 监听 container 尺寸变化，延迟重新应用卡片绝对定位
	// （布局是异步执行的，需要等布局完成后再覆盖）
	dm.container.SizeChanged().Attach(func() {
		go func() {
			time.Sleep(50 * time.Millisecond)
			dm.mainWindow.Synchronize(func() {
				dm.reapplyCardPositions()
			})
		}()
	})

	// 设置键盘快捷键 Alt+F6 退出全屏
	dm.setupHotkeys()

	// 鼠标事件
	dm.bodyWidget.MouseDown().Attach(dm.handleDesktopMouseDown)

	dm.bodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		// 未分组图标拖拽中
		if dm.freeItemDragActive {
			dm.freeItemDragMouseX = x
			dm.freeItemDragMouseY = y
			dm.bodyWidget.Invalidate()

			var screenPt win.POINT
			screenPt.X = int32(x)
			screenPt.Y = int32(y)
			win.ClientToScreen(dm.bodyWidget.Handle(), &screenPt)
			dm.updateDropTarget(int(screenPt.X), int(screenPt.Y))
			return
		}

		// 普通悬停检测（未分组图标）
		if dm.checkFreeItemHover(x, y) {
			dm.bodyWidget.Invalidate()
		}
	})

	dm.bodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		// 清除未分组图标长按状态（防止快速点击后误触发）
		if dm.freeItemDragPressed && !dm.freeItemDragActive {
			dm.freeItemDragPressed = false
			return
		}
		if !dm.freeItemDragActive {
			return
		}
		dm.freeItemDragActive = false
		dm.freeItemDragPressed = false
		win.ReleaseCapture()

		var screenPt win.POINT
		screenPt.X = int32(x)
		screenPt.Y = int32(y)
		win.ClientToScreen(dm.bodyWidget.Handle(), &screenPt)
		dm.handleFreeItemDrop(int(screenPt.X), int(screenPt.Y))
	})

	// 创建分组卡片（在 container 中，绝对定位）
	dm.createGroupCards()

	// 预加载未分组图标的 bitmap 缓存
	freePaths := make([]string, 0)
	for _, item := range dm.manager.GetUngroupedItems() {
		freePaths = append(freePaths, item.Path)
	}
	if len(freePaths) > 0 {
		globalIconBmpCache.LoadAll(freePaths)
	}

	// 延迟去边框：等消息循环启动后再去边框
	go dm.delayedSetup()

	return nil
}

// delayedSetup 消息循环启动后去边框、嵌入桌面层级
func (dm *DesktopMode) delayedSetup() {
	defer recoverGoroutine("delayedSetup")

	// 等待消息循环启动
	time.Sleep(300 * time.Millisecond)

	dm.mainWindow.Synchronize(func() {
		hwnd := dm.mainWindow.Handle()
		logger.Debug("delayedSetup: hwnd=%v, pos=(%d,%d,%dx%d)",
			hwnd, dm.workX, dm.workY, dm.workW, dm.workH)

		// 注意：不再调用 HideDesktopIcons，因为窗口嵌入到 shell WorkerW 后
		// 通过 Z 序置顶覆盖 SHELLDLL_DefView（桌面图标）

		// 移除菜单栏（walk MainWindow 默认创建了空菜单栏，占用顶部空间）
		dm.winAPI.RemoveWindowMenu(win.HWND(hwnd))

		// 去除边框（使用全屏尺寸，嵌入桌面层后覆盖整个屏幕）
		dm.winAPI.SetWindowBorderless(win.HWND(hwnd))

		// 将窗口嵌入桌面层级（SetParent 到 WorkerW）
		if !dm.winAPI.SetAsDesktopChild(win.HWND(hwnd)) {
			logger.Error("delayedSetup: SetAsDesktopChild failed, WorkerW not found, exiting")
			os.Exit(1)
		}
		logger.Debug("delayedSetup: SetAsDesktopChild done")

		// 嵌入后设置窗口位置铺满工作区
		dm.winAPI.MoveWindow(win.HWND(hwnd), dm.workX, dm.workY, dm.workW, dm.workH)
		logger.Debug("delayedSetup: MoveWindow done")
	})

	// 去边框后客户区变大，通过尺寸变化触发 walk 重新布局（不重绘避免闪烁）
	time.Sleep(100 * time.Millisecond)

	dm.mainWindow.Synchronize(func() {
		hwnd := dm.mainWindow.Handle()
		// +1 不重绘触发 WM_WINDOWPOSCHANGED，让 walk 用新客户区尺寸重新布局
		dm.winAPI.SetWindowPosNoRedraw(win.HWND(hwnd), dm.workX, dm.workY, dm.workW+1, dm.workH+1)
	})

	time.Sleep(50 * time.Millisecond)

	dm.mainWindow.Synchronize(func() {
		hwnd := dm.mainWindow.Handle()
		// 恢复正确尺寸
		dm.winAPI.MoveWindow(win.HWND(hwnd), dm.workX, dm.workY, dm.workW, dm.workH)
	})

	time.Sleep(100 * time.Millisecond)

	dm.mainWindow.Synchronize(func() {
		// 最终确认：强制 container 和 bodyWidget 覆盖整个客户区
		clientBounds := dm.mainWindow.ClientBoundsPixels()
		logger.Debug("delayedSetup: clientBounds=(%d,%d,%dx%d)",
			clientBounds.X, clientBounds.Y, clientBounds.Width, clientBounds.Height)

		// 强制 container 铺满整个窗口客户区
		fullH := clientBounds.Y + clientBounds.Height
		dm.container.SetBoundsPixels(walk.Rectangle{
			X: 0, Y: 0,
			Width: dm.workW, Height: fullH,
		})
		dm.bodyWidget.SetBoundsPixels(walk.Rectangle{
			X: 0, Y: 0,
			Width: dm.workW, Height: fullH,
		})

		// 强制重新应用卡片的绝对定位
		dm.reapplyCardPositions()

		// 手动触发一次完整重绘
		dm.bodyWidget.Invalidate()

		finalBounds := dm.mainWindow.BoundsPixels()
		containerBounds := dm.container.BoundsPixels()
		bodyBounds := dm.bodyWidget.BoundsPixels()
		logger.Debug("delayedSetup done: window=(%d,%d,%dx%d), container=(%d,%d,%dx%d), body=(%d,%d,%dx%d)",
			finalBounds.X, finalBounds.Y, finalBounds.Width, finalBounds.Height,
			containerBounds.X, containerBounds.Y, containerBounds.Width, containerBounds.Height,
			bodyBounds.X, bodyBounds.Y, bodyBounds.Width, bodyBounds.Height)

		dm.lifecycle.MarkReady()

		// 延迟再次确认卡片位置（防止异步布局覆盖）
		go func() {
			defer recoverGoroutine("postLayoutCardFix")
			time.Sleep(200 * time.Millisecond)
			dm.mainWindow.Synchronize(func() {
				logger.Debug("postLayoutCardFix: reapply after 200ms delay, cards=%d", len(dm.cards))
				dm.reapplyCardPositions()
			})
		}()
	})
}


// reapplyCardPositions 重新应用所有卡片的绝对定位，并确保卡片 Z-order 在 bodyWidget 上方
func (dm *DesktopMode) reapplyCardPositions() {
	for i, card := range dm.cards {
		card.ReapplyBounds()
		// 确保卡片在 Z-order 顶部（在 bodyWidget 上方）
		win.SetWindowPos(card.Container().Handle(), win.HWND_TOP, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		if i == 0 {
			b := card.Container().BoundsPixels()
			parentHwnd := win.GetParent(card.Container().Handle())
			logger.Debug("reapplyCardPositions: card[0] bounds=(%d,%d,%dx%d), visible=%v, hwnd=%v, parent=%v, containerHwnd=%v",
				b.X, b.Y, b.Width, b.Height, card.Container().Visible(),
				card.Container().Handle(), parentHwnd, dm.container.Handle())
		}
	}
}

// paintDesktop 绘制桌面内容
var paintCount int

func (dm *DesktopMode) paintDesktop(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := dm.bodyWidget.ClientBoundsPixels()
	paintCount++
	if paintCount <= 3 {
		logger.Debug("paintDesktop #%d: bounds=(%d,%d,%dx%d), wallpaperBmp=%v",
			paintCount, bounds.X, bounds.Y, bounds.Width, bounds.Height, dm.wallpaperBmp != nil)
	}

	// 1. 绘制深色背景
	dm.paintBackground(canvas, bounds)

	// 2. 绘制壁纸
	dm.paintWallpaper(canvas, bounds)

	// 3. 绘制工具栏
	dm.paintToolbar(canvas, bounds)

	// 4. 绘制未分组的桌面图标
	dm.paintFreeItems(canvas, bounds)

	// 5. 未分组区域拖放高亮
	if dm.dropToDesktop {
		dm.paintDesktopDropHighlight(canvas, bounds)
	}

	// 6. 未分组图标拖拽 ghost
	if dm.freeItemDragActive {
		dm.paintFreeItemDragGhost(canvas, bounds)
	}

	// 7. 卡片图标拖拽 ghost（在桌面区域也可见）
	if dm.iconDragActive && dm.iconDragSourceCard != nil && !dm.freeItemDragActive {
		dm.paintCardItemDragGhost(canvas, bounds)
	}

	return nil
}

// paintDesktopDropHighlight 绘制桌面（未分组区域）拖放高亮
func (dm *DesktopMode) paintDesktopDropHighlight(canvas *walk.Canvas, bounds walk.Rectangle) {
	startX := bounds.Width - desktopIconItemWidth - 24
	startY := 56
	w := desktopIconItemWidth + 8
	items := dm.manager.GetUngroupedItems()
	h := len(items)*desktopIconItemHeight + 8
	if h < 60 {
		h = desktopIconItemHeight + 8
	}

	rect := walk.Rectangle{
		X: startX, Y: startY,
		Width: w, Height: h,
	}

	// 绘制实线高亮框
	pen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x4A, 0xA0, 0xFF))
	if err != nil {
		return
	}
	defer pen.Dispose()

	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y + rect.Height}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X + rect.Width, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
}

// paintFreeItemDragGhost 绘制未分组图标拖拽 ghost（使用缓存的 bitmap）
func (dm *DesktopMode) paintFreeItemDragGhost(canvas *walk.Canvas, _ walk.Rectangle) {
	if dm.ghostBmp == nil {
		return
	}
	ghostX := dm.freeItemDragMouseX - desktopIconItemWidth/2
	ghostY := dm.freeItemDragMouseY - desktopIconItemHeight/2

	iconX := ghostX + (desktopIconItemWidth-desktopIconSize)/2
	iconY := ghostY + desktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.ghostBmp, walk.Rectangle{
		X: iconX, Y: iconY, Width: desktopIconSize, Height: desktopIconSize,
	}, 128)

	font := GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := dm.freeItemDragItem.Name
		lines := splitTextToLines(displayName, 4)
		labelTop := ghostY + desktopIconLabelTop
		for i, line := range lines {
			if i >= 2 {
				break
			}
			if i == 1 && len(lines) > 2 {
				line = TruncateText(line, 3)
			}
			lineY := labelTop + i*desktopIconLineHeight
			textBounds := walk.Rectangle{
				X: ghostX, Y: lineY,
				Width: desktopIconItemWidth, Height: desktopIconLineHeight,
			}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds,
				walk.TextCenter|walk.TextSingleLine)
		}
	}
}

// paintCardItemDragGhost 绘制卡片图标拖拽 ghost（桌面区域可见）
func (dm *DesktopMode) paintCardItemDragGhost(canvas *walk.Canvas, _ walk.Rectangle) {
	if dm.ghostBmp == nil {
		return
	}
	// 屏幕坐标转 bodyWidget 客户区坐标
	var pt win.POINT
	pt.X = int32(dm.iconDragScreenX)
	pt.Y = int32(dm.iconDragScreenY)
	win.ScreenToClient(dm.bodyWidget.Handle(), &pt)

	ghostX := int(pt.X) - desktopIconItemWidth/2
	ghostY := int(pt.Y) - desktopIconItemHeight/2

	iconX := ghostX + (desktopIconItemWidth-desktopIconSize)/2
	iconY := ghostY + desktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.ghostBmp, walk.Rectangle{
		X: iconX, Y: iconY, Width: desktopIconSize, Height: desktopIconSize,
	}, 128)

	font := GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := dm.iconDragItem.Name
		lines := splitTextToLines(displayName, 4)
		labelTop := ghostY + desktopIconLabelTop
		for i, line := range lines {
			if i >= 2 {
				break
			}
			if i == 1 && len(lines) > 2 {
				line = TruncateText(line, 3)
			}
			lineY := labelTop + i*desktopIconLineHeight
			textBounds := walk.Rectangle{
				X: ghostX, Y: lineY,
				Width: desktopIconItemWidth, Height: desktopIconLineHeight,
			}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds,
				walk.TextCenter|walk.TextSingleLine)
		}
	}
}

// loadDragGhostBmp 预加载拖拽 ghost bitmap（避免每次重绘重新提取图标）
func (dm *DesktopMode) loadDragGhostBmp(filePath string) {
	if dm.ghostBmp != nil {
		dm.ghostBmp.Dispose()
		dm.ghostBmp = nil
	}
	extractor := NewIconExtractor()
	iconImg, _ := extractor.GetIconImage(filePath)
	if iconImg == nil {
		return
	}
	rgbaImg, ok := iconImg.(*image.RGBA)
	if !ok {
		b := iconImg.Bounds()
		rgbaImg = image.NewRGBA(b)
		for iy := b.Min.Y; iy < b.Max.Y; iy++ {
			for ix := b.Min.X; ix < b.Max.X; ix++ {
				rgbaImg.Set(ix, iy, iconImg.At(ix, iy))
			}
		}
	}
	bmp, err := walk.NewBitmapFromImage(rgbaImg)
	if err != nil {
		return
	}
	dm.ghostBmp = bmp
}

// disposeDragGhostBmp 释放拖拽 ghost bitmap
func (dm *DesktopMode) disposeDragGhostBmp() {
	if dm.ghostBmp != nil {
		dm.ghostBmp.Dispose()
		dm.ghostBmp = nil
	}
}

// paintBackground 绘制深色背景
func (dm *DesktopMode) paintBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	bgColor := color.RGBA{R: 0x1A, G: 0x1A, B: 0x2E, A: 0xFF}
	bgImg := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			bgImg.SetRGBA(x, y, bgColor)
		}
	}
	bmp, err := walk.NewBitmapFromImage(bgImg)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
}

// loadWallpaper 预加载壁纸
func (dm *DesktopMode) loadWallpaper() {
	wallpaperPath := GetCurrentWallpaper()
	logger.Debug("loadWallpaper: path=%q", wallpaperPath)
	if wallpaperPath == "" {
		logger.Debug("loadWallpaper: 壁纸路径为空，跳过")
		return
	}

	// 使用 Go 标准库加载壁纸，按 Fill 模式裁剪到工作区尺寸
	img := LoadWallpaperImage(dm.workW, dm.workH)
	if img == nil {
		logger.Debug("loadWallpaper: LoadWallpaperImage 返回 nil，回退到 GDI+ 加载")
		dpi := dm.mainWindow.DPI()
		if dpi <= 0 {
			dpi = 96
		}
		bmp, err := walk.NewBitmapFromFileForDPI(wallpaperPath, dpi)
		if err != nil {
			logger.Debug("loadWallpaper: GDI+ 也失败: %v", err)
			return
		}
		size := bmp.Size()
		logger.Debug("loadWallpaper: GDI+ 加载成功, bmpSize=%dx%d", size.Width, size.Height)
		dm.wallpaperBmp = bmp
		return
	}

	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		logger.Debug("loadWallpaper: NewBitmapFromImageForDPI failed: %v", err)
		return
	}
	size := bmp.Size()
	logger.Debug("loadWallpaper: 加载成功, bmpSize=%dx%d", size.Width, size.Height)
	dm.wallpaperBmp = bmp
}

// paintWallpaper 绘制壁纸
func (dm *DesktopMode) paintWallpaper(canvas *walk.Canvas, bounds walk.Rectangle) {
	if dm.wallpaperBmp == nil {
		return
	}
	canvas.DrawBitmapWithOpacityPixels(dm.wallpaperBmp, bounds, 255)
}

// paintToolbar 绘制工具栏
func (dm *DesktopMode) paintToolbar(canvas *walk.Canvas, bounds walk.Rectangle) {
	font, _ := walk.NewFont("Microsoft YaHei", 14, 0)
	if font == nil {
		return
	}
	defer font.Dispose()

	// "+ 添加卡片" 按钮区域
	toolbarBounds := walk.Rectangle{
		X: bounds.Width - 140, Y: 10,
		Width: 120, Height: 30,
	}

	// 绘制按钮背景
	btnColor := color.RGBA{R: 0x30, G: 0x34, B: 0x3C, A: 0xBD}
	btnImg := image.NewRGBA(image.Rect(0, 0, toolbarBounds.Width, toolbarBounds.Height))
	for y := 0; y < toolbarBounds.Height; y++ {
		for x := 0; x < toolbarBounds.Width; x++ {
			btnImg.SetRGBA(x, y, btnColor)
		}
	}
	btnBmp, err := walk.NewBitmapFromImage(btnImg)
	if err == nil {
		defer btnBmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(btnBmp, toolbarBounds, byte(btnColor.A))
	}

	canvas.DrawTextPixels("+ 添加卡片", font, walk.RGB(0xFF, 0xFF, 0xFF),
		toolbarBounds, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
}

// ==================== 未分组图标网格布局 ====================

const (
	freeGridLeft = 20  // 网格左边距（像素）
	freeGridTop  = 60  // 网格上边距（像素）
)

// freeCellW 未分组图标网格单元格宽度（像素）
func freeCellW() int { return desktopIconItemWidth + desktopIconGap }

// freeCellH 未分组图标网格单元格高度（像素）
func freeCellH() int { return desktopIconItemHeight + desktopIconGap }

// gridToPixel 将网格坐标转为像素坐标
func gridToPixel(col, row int) (int, int) {
	return freeGridLeft + col*freeCellW(), freeGridTop + row*freeCellH()
}

// pixelToGrid 将像素坐标转为最近的网格坐标
func pixelToGrid(px, py int) (int, int) {
	col := (px - freeGridLeft + freeCellW()/2) / freeCellW()
	row := (py - freeGridTop + freeCellH()/2) / freeCellH()
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	return col, row
}

// posToGrid 将相对坐标转为网格坐标
func (dm *DesktopMode) posToGrid(pos config.Position) (int, int) {
	px := int(pos.X * float64(dm.workW))
	py := int(pos.Y * float64(dm.workH))
	return pixelToGrid(px, py)
}

// gridToRel 将网格坐标转为相对坐标
func (dm *DesktopMode) gridToRel(col, row int) config.Position {
	px, py := gridToPixel(col, row)
	return config.Position{
		X: float64(px) / float64(dm.workW),
		Y: float64(py) / float64(dm.workH),
	}
}

// getOccupiedCells 获取所有已占用的网格单元格（不含 exceptPath）
func (dm *DesktopMode) getOccupiedCells(exceptPath string) map[[2]int]bool {
	items := dm.manager.GetUngroupedItems()
	cells := make(map[[2]int]bool)
	for _, item := range items {
		if item.Path == exceptPath {
			continue
		}
		pos := dm.manager.GetFreeItemPosition(item.Path)
		col, row := dm.posToGrid(pos)
		if col < 0 || row < 0 {
			continue
		}
		cell := [2]int{col, row}
		if !cells[cell] {
			cells[cell] = true
		}
	}
	return cells
}

// getFreeItemPixelPos 获取未分组项的像素位置（基于网格，吸附对齐）
func (dm *DesktopMode) getFreeItemPixelPos(path string, fallbackIdx int) (int, int) {
	pos := dm.manager.GetFreeItemPosition(path)
	if pos.X < 0 || pos.Y < 0 {
		// 待分配：从左上角(0,0)开始，从上向下再从左到右找第一个空位
		col, row := dm.findFreeGridCell("", 0, fallbackIdx)
		// 保存分配的位置
		relPos := dm.gridToRel(col, row)
		dm.manager.SetFreeItemPosition(path, relPos)
		return gridToPixel(col, row)
	}
	col, row := dm.posToGrid(pos)
	return gridToPixel(col, row)
}

// findFreeGridCell 查找空闲网格，从 wantCol,wantRow 开始，遇占用先向下再向右偏移
func (dm *DesktopMode) findFreeGridCell(exceptPath string, wantCol, wantRow int) (int, int) {
	occupied := dm.getOccupiedCells(exceptPath)
	bounds := dm.bodyWidget.ClientBoundsPixels()
	maxCol := bounds.Width / freeCellW()
	if maxCol < 1 {
		maxCol = 1
	}
	maxRow := bounds.Height / freeCellH()
	if maxRow < 1 {
		maxRow = 1
	}

	for attempt := 0; attempt < 500; attempt++ {
		cell := [2]int{wantCol, wantRow}
		if !occupied[cell] {
			return wantCol, wantRow
		}
		// 先向下（同一列中的下一行）
		wantRow++
		if wantRow >= maxRow {
			wantRow = 0
			wantCol++ // 然后向右（下一列）
		}
		if wantCol >= maxCol {
			wantCol = 0
		}
	}
	return wantCol, wantRow
}

// paintFreeItems 绘制未分组的桌面图标（网格对齐）
func (dm *DesktopMode) paintFreeItems(canvas *walk.Canvas, bounds walk.Rectangle) {
	items := dm.manager.GetUngroupedItems()
	if len(items) == 0 {
		return
	}

	for idx, item := range items {
		px, py := dm.getFreeItemPixelPos(item.Path, idx)
		if py+desktopIconItemHeight > bounds.Height {
			continue
		}
		gc := &GroupCard{executor: dm.executor}
		gc.paintIconTile(canvas, item, px, py, idx == dm.hoveredFreeIdx)
	}
}

// handleDesktopMouseDown 处理桌面鼠标按下事件
func (dm *DesktopMode) handleDesktopMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}

	// 检查是否点击了 "+ 添加卡片" 按钮
	bounds := dm.bodyWidget.ClientBoundsPixels()
	btnRect := walk.Rectangle{
		X: bounds.Width - 140, Y: 10,
		Width: 120, Height: 30,
	}
	if x >= btnRect.X && x <= btnRect.X+btnRect.Width &&
		y >= btnRect.Y && y <= btnRect.Y+btnRect.Height {
		dm.addNewCard()
		return
	}

	// 检查点击未分组图标（使用保存的位置，启动长按拖拽）
	items := dm.manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+desktopIconItemWidth &&
			y >= iy && y <= iy+desktopIconItemHeight {
			dm.freeItemDragPressed = true
			dm.freeItemDragIdx = i
			dm.freeItemDragItem = item
			dm.freeItemDragStartX = x
			dm.freeItemDragStartY = y
			dm.freeItemDragStartTime = time.Now()
			go dm.checkFreeItemDragStart()
			return
		}
	}
}

// checkFreeItemDragStart 检测未分组图标长按开始拖拽
func (dm *DesktopMode) checkFreeItemDragStart() {
	defer recoverGoroutine("checkFreeItemDragStart")
	time.Sleep(longPressDragDelay)
	dm.bodyWidget.Synchronize(func() {
		if !dm.freeItemDragPressed || dm.freeItemDragActive {
			return
		}
		dm.freeItemDragActive = true
		dm.freeItemDragMouseX = dm.freeItemDragStartX
		dm.freeItemDragMouseY = dm.freeItemDragStartY
		dm.loadDragGhostBmp(dm.freeItemDragItem.Path)
		dm.bodyWidget.Invalidate()

		win.SetCapture(dm.bodyWidget.Handle())

		dm.iconDragActive = true
		dm.iconDragItem = dm.freeItemDragItem
		dm.iconDragSourceGroup = ""

		var screenPt win.POINT
		screenPt.X = int32(dm.freeItemDragStartX)
		screenPt.Y = int32(dm.freeItemDragStartY)
		win.ClientToScreen(dm.bodyWidget.Handle(), &screenPt)
		dm.iconDragScreenX = int(screenPt.X)
		dm.iconDragScreenY = int(screenPt.Y)
	})
}

// handleFreeItemDrop 处理未分组图标拖放（吸附到网格）
func (dm *DesktopMode) handleFreeItemDrop(screenX, screenY int) {
	dm.iconDragActive = false
	dm.disposeDragGhostBmp()
	defer dm.bodyWidget.Invalidate()

	// 查找目标卡片
	targetCard := dm.findCardAtPoint(screenX, screenY)
	if targetCard != nil {
		// 拖入卡片
		dm.manager.MoveItemToGroup(dm.freeItemDragItem.Path, targetCard.groupName)
		targetCard.Refresh()
	} else {
		// 拖到空白桌面：吸附到最近的网格格子
		var pt win.POINT
		pt.X = int32(screenX)
		pt.Y = int32(screenY)
		win.ScreenToClient(dm.bodyWidget.Handle(), &pt)
		px := int(pt.X) - desktopIconItemWidth/2
		py := int(pt.Y) - desktopIconItemHeight/2
		wantCol, wantRow := pixelToGrid(px, py)
		col, row := dm.findFreeGridCell(dm.freeItemDragItem.Path, wantCol, wantRow)
		relPos := dm.gridToRel(col, row)
		dm.manager.SetFreeItemPosition(dm.freeItemDragItem.Path, relPos)
	}
	dm.clearDropState()
}

// checkFreeItemHover 检测未分组图标悬停，返回 true 表示 hover 状态变化
func (dm *DesktopMode) checkFreeItemHover(x, y int) bool {
	items := dm.manager.GetUngroupedItems()
	newIdx := -1
	for i := range items {
		ix, iy := dm.getFreeItemPixelPos(items[i].Path, i)
		if x >= ix && x <= ix+desktopIconItemWidth &&
			y >= iy && y <= iy+desktopIconItemHeight {
			newIdx = i
			break
		}
	}
	if newIdx != dm.hoveredFreeIdx {
		dm.hoveredFreeIdx = newIdx
		return true
	}
	return false
}

// ==================== 图标拖拽协调（跨卡片） ====================

// onCardIconDragStart 卡片内图标拖拽开始
func (dm *DesktopMode) onCardIconDragStart(card *GroupCard, idx int, item group.GroupItem) {
	dm.iconDragActive = true
	dm.iconDragSourceCard = card
	dm.iconDragItem = item
	dm.iconDragSourceGroup = card.groupName
	dm.loadDragGhostBmp(item.Path)
	// 同步 ghost 屏幕位置（拖拽开始时等于点击位置）
	var cardPt win.POINT
	cardPt.X = int32(card.iconDragStartX)
	cardPt.Y = int32(card.iconDragStartY)
	win.ClientToScreen(card.bodyWidget.Handle(), &cardPt)
	dm.iconDragScreenX = int(cardPt.X)
	dm.iconDragScreenY = int(cardPt.Y)
	dm.bodyWidget.Invalidate()
}

// onCardIconDragMove 卡片内图标拖拽移动
func (dm *DesktopMode) onCardIconDragMove(card *GroupCard, screenX, screenY int) {
	dm.iconDragScreenX = screenX
	dm.iconDragScreenY = screenY
	dm.updateDropTarget(screenX, screenY)
}

// onCardIconDragEnd 卡片内图标拖拽结束
func (dm *DesktopMode) onCardIconDragEnd(card *GroupCard, screenX, screenY int) {
	dm.iconDragActive = false
	dm.disposeDragGhostBmp()
	dm.bodyWidget.Invalidate()

	// 查找目标卡片
	targetCard := dm.findCardAtPoint(screenX, screenY)
	sourceGroup := card.groupName

	if dm.isPointInUngroupedArea(screenX, screenY) {
		// 拖到未分组区域
		dm.manager.MoveItemToDesktop(card.iconDragItem.Path)
		card.refreshItems()
	} else if targetCard != nil && targetCard != card {
		// 拖到其他卡片
		dm.manager.MoveItemToGroup(card.iconDragItem.Path, targetCard.groupName)
		card.refreshItems()
		targetCard.Refresh()
	} else if targetCard == card {
		// 同卡片内拖拽排序
		insertIdx := card.GetDropIndexAt(card.iconDragMouseX, card.iconDragMouseY)
		if insertIdx >= 0 && insertIdx <= len(card.items) {
			dm.manager.MoveItemWithinGroup(sourceGroup, card.iconDragItem.Path, insertIdx)
		}
		card.refreshItems()
	} else {
		// 拖到卡片外空白区域 → 移回桌面（未分组）
		dm.manager.MoveItemToDesktop(card.iconDragItem.Path)
		card.refreshItems()
	}

	dm.clearDropState()
}

// updateDropTarget 更新当前拖放目标
func (dm *DesktopMode) updateDropTarget(screenX, screenY int) {
	// 清除旧目标的高亮
	if dm.dropTargetCard != nil {
		dm.dropTargetCard.SetIsDropTarget(false)
	}

	dm.dropTargetCard = nil
	dm.dropToDesktop = false

	// 检查是否在某张卡片上
	for _, c := range dm.cards {
		if dm.isPointInCard(c, screenX, screenY) {
			dm.dropTargetCard = c
			c.SetIsDropTarget(true)
			break
		}
	}

	// 检查是否在未分组区域
	if dm.dropTargetCard == nil && dm.isPointInUngroupedArea(screenX, screenY) {
		dm.dropToDesktop = true
	}

	dm.bodyWidget.Invalidate()
}

// clearDropState 清除拖放状态
func (dm *DesktopMode) clearDropState() {
	if dm.dropTargetCard != nil {
		dm.dropTargetCard.SetIsDropTarget(false)
	}
	dm.dropTargetCard = nil
	dm.dropToDesktop = false
	dm.iconDragSourceCard = nil
	dm.iconDragSourceGroup = ""
}

// findCardAtPoint 查找屏幕坐标点所在的卡片
func (dm *DesktopMode) findCardAtPoint(screenX, screenY int) *GroupCard {
	for _, c := range dm.cards {
		if dm.isPointInCard(c, screenX, screenY) {
			return c
		}
	}
	return nil
}

// isPointInCard 判断屏幕坐标点是否在卡片内
func (dm *DesktopMode) isPointInCard(card *GroupCard, screenX, screenY int) bool {
	sb := card.ScreenBounds()
	return screenX >= sb.X && screenX <= sb.X+sb.Width &&
		screenY >= sb.Y && screenY <= sb.Y+sb.Height
}

// isPointInUngroupedArea 判断屏幕坐标点是否在未分组图标区域
func (dm *DesktopMode) isPointInUngroupedArea(screenX, screenY int) bool {
	// 转成 bodyWidget 客户区坐标
	var pt win.POINT
	pt.X = int32(screenX)
	pt.Y = int32(screenY)
	win.ScreenToClient(dm.bodyWidget.Handle(), &pt)

	cx := int(pt.X)
	cy := int(pt.Y)

	items := dm.manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if cx >= ix && cx <= ix+desktopIconItemWidth &&
			cy >= iy && cy <= iy+desktopIconItemHeight {
			return true
		}
	}
	return false
}

// addNewCard 添加新卡片
func (dm *DesktopMode) addNewCard() {
	name, ok := ShowInputDialog(dm.mainWindow, "新建分组", "请输入分组名称：", "")
	if !ok || name == "" {
		return
	}
	dm.manager.CreateGroup(name, "#30343CBD")
	dm.refreshCards()
}

// createGroupCards 创建所有分组卡片
// 卡片创建在 mainWindow 中（与 container 同级），避免被 container 的 VBox 影响
func (dm *DesktopMode) createGroupCards() {
	groups := dm.manager.GetGroups()
	logger.Debug("createGroupCards: %d groups", len(groups))
	for i, grp := range groups {
		card, err := NewGroupCard(dm.mainWindow, grp, dm.manager, dm.executor, dm.mainWindow, dm.workW, dm.workH)
		if err != nil {
			logger.Debug("createGroupCards: card[%d] %q error: %v", i, grp.Name, err)
			continue
		}
		b := card.Container().BoundsPixels()
		logger.Debug("createGroupCards: card[%d] %q bounds=(%d,%d,%dx%d) visible=%v handle=%v",
			i, grp.Name, b.X, b.Y, b.Width, b.Height, card.Container().Visible(), card.Container().Handle())
		dm.setupCardActions(card, grp)
		dm.cards = append(dm.cards, card)
	}
}

// setupCardActions 设置卡片操作按钮和拖拽回调
func (dm *DesktopMode) setupCardActions(card *GroupCard, grp config.Group) {
	card.SetOnPositionChanged(func(name string, x, y float64) {
		dm.manager.UpdateGroupPosition(name, x, y)
	})
	card.SetOnSizeChanged(func(name string, w, h float64) {
		dm.manager.UpdateGroupSize(name, w, h)
	})

	// 图标拖拽回调
	card.SetOnIconDragStart(dm.onCardIconDragStart)
	card.SetOnIconDragMove(dm.onCardIconDragMove)
	card.SetOnIconDragEnd(dm.onCardIconDragEnd)
	card.SetOnRename(func(name string) {
		newName, ok := ShowInputDialog(dm.mainWindow, "重命名分组", "请输入新名称：", name)
		if ok && newName != "" && newName != name {
			dm.manager.RenameGroup(name, newName)
			dm.refreshCards()
		}
	})
	card.SetOnColor(func(name string) {
		color, ok := ShowColorDialog(dm.mainWindow, "修改颜色", PresetColors)
		if ok && color != "" {
			dm.manager.UpdateGroupColor(name, color)
			dm.refreshCards()
		}
	})
	card.SetOnDelete(func(name string) {
		if ShowConfirmDialog(dm.mainWindow, "删除分组", "确定要删除分组「"+name+"」吗？\n分组内的项目将移回桌面。") {
			dm.manager.DeleteGroup(name)
			dm.refreshCards()
		}
	})
}

// refreshCards 刷新所有卡片
func (dm *DesktopMode) refreshCards() {
	// 移除旧卡片
	for _, card := range dm.cards {
		card.Cleanup()
		card.Container().Dispose()
	}
	dm.cards = nil

	// 重新创建
	dm.createGroupCards()
	dm.bodyWidget.Invalidate()
}

// setupHotkeys 设置快捷键
func (dm *DesktopMode) setupHotkeys() {
	// Alt+F6 退出全屏模式
	dm.mainWindow.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyF6 {
			// 检查 Alt 是否按下
			if win.GetKeyState(int32(win.VK_MENU)) < 0 {
				dm.exitDesktopMode()
			}
		}
	})
}

// exitDesktopMode 退出桌面模式
func (dm *DesktopMode) exitDesktopMode() {
	dm.lifecycle.MarkClosing()
	dm.lifecycle.ExecuteCleanups()
	hwnd := dm.mainWindow.Handle()
	// 从桌面层脱离
	dm.winAPI.DetachFromDesktop(win.HWND(hwnd))
	dm.mainWindow.Close()
}

// Refresh 刷新桌面模式
func (dm *DesktopMode) Refresh() {
	for _, card := range dm.cards {
		card.Refresh()
	}
	dm.bodyWidget.Invalidate()
}
