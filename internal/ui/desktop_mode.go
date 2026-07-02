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

	// 鼠标双击事件（打开项目）
	dm.bodyWidget.MouseDown().Attach(dm.handleMouseDown)

	// 鼠标移动事件（检测自由图标悬停）
	dm.bodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		bounds := dm.bodyWidget.ClientBoundsPixels()
		items := dm.manager.GetUngroupedItems()
		startX := bounds.Width - desktopIconItemWidth - 20
		startY := 60
		newIdx := -1
		for i := range items {
			iy := startY + i*desktopIconItemHeight
			if x >= startX && x <= startX+desktopIconItemWidth &&
				y >= iy && y <= iy+desktopIconItemHeight {
				newIdx = i
				break
			}
		}
		if newIdx != dm.hoveredFreeIdx {
			dm.hoveredFreeIdx = newIdx
			dm.bodyWidget.Invalidate()
		}
	})

	// 创建分组卡片（在 container 中，绝对定位）
	dm.createGroupCards()

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

	return nil
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

// paintFreeItems 绘制未分组的桌面图标
func (dm *DesktopMode) paintFreeItems(canvas *walk.Canvas, bounds walk.Rectangle) {
	items := dm.manager.GetUngroupedItems()
	if len(items) == 0 {
		return
	}

	startX := bounds.Width - desktopIconItemWidth - 20
	startY := 60

	for i, item := range items {
		y := startY + i*desktopIconItemHeight
		if y+desktopIconItemHeight > bounds.Height {
			break
		}
		gc := &GroupCard{executor: dm.executor}
		gc.paintIconTile(canvas, item, startX, y, i == dm.hoveredFreeIdx)
	}
}

// handleMouseDown 处理鼠标按下事件
func (dm *DesktopMode) handleMouseDown(x, y int, button walk.MouseButton) {
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

	// 检查双击打开项目（未分组项）
	items := dm.manager.GetUngroupedItems()
	startX := bounds.Width - desktopIconItemWidth - 20
	startY := 60
	for i, item := range items {
		iy := startY + i*desktopIconItemHeight
		if x >= startX && x <= startX+desktopIconItemWidth &&
			y >= iy && y <= iy+desktopIconItemHeight {
			dm.executor.Execute(item.Path)
			return
		}
	}
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

// setupCardActions 设置卡片操作按钮回调
func (dm *DesktopMode) setupCardActions(card *GroupCard, grp config.Group) {
	card.SetOnPositionChanged(func(name string, x, y float64) {
		dm.manager.UpdateGroupPosition(name, x, y)
	})
	card.SetOnSizeChanged(func(name string, w, h float64) {
		dm.manager.UpdateGroupSize(name, w, h)
	})
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
