package desktop

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
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

// Setup 设置桌面模式 UI
func (dm *DesktopMode) Setup() error {
	logger.Debug("Setup: DPI=%d, bounds=(%d,%d,%dx%d)",
		dm.MainWindow.DPI(), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)

	// === 注入子结构体能力 ===
	dm.IconDragState.Inject(IconInject{
		BodyWidget:          func() *walk.CustomWidget { return dm.BodyWidget },
		Manager:             func() *group.Manager { return dm.Manager },
		Cards:               func() []*ui.GroupCard { return dm.Cards },
		Invalidate:          func() { dm.BodyWidget.Invalidate() },
		RefreshCard:         func(card *ui.GroupCard) { card.Refresh() },
		IsPointInUngrouped:  func(sx, sy int) bool { return dm.HoverState.IsPointInUngroupedArea(sx, sy, dm.BodyWidget, dm.Manager.GetUngroupedItems(), dm.getFreeItemPixelPos) },
		MoveItemToDesktop:   dm.Manager.MoveItemToDesktop,
		MoveItemToGroup:     dm.Manager.MoveItemToGroup,
		MoveItemWithinGroup: dm.Manager.MoveItemWithinGroup,
	})
	dm.CardDragOutline.Inject(func() { dm.BodyWidget.Invalidate() })
	dm.ResizeOutlineState.Inject(dm.WorkX, dm.WorkY)

	// 设置主窗口尺寸为工作区
	dm.MainWindow.SetBoundsPixels(walk.Rectangle{
		X: dm.WorkX, Y: dm.WorkY,
		Width: dm.WorkW, Height: dm.WorkH,
	})

	// 设置窗口背景色为深色（消除白边）
	bg, _ := walk.NewSolidColorBrush(walk.RGB(0x1A, 0x1A, 0x2E))
	dm.MainWindow.SetBackground(bg)

	// 预加载壁纸
	dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)

	// 创建 Composite 容器
	var err error
	dm.Container, err = walk.NewComposite(dm.MainWindow)
	if err != nil {
		return err
	}
	if layout, ok := dm.MainWindow.Layout().(*walk.BoxLayout); ok {
		layout.SetStretchFactor(dm.Container, 1)
	}
	containerLayout := walk.NewVBoxLayout()
	containerLayout.SetMargins(walk.Margins{})
	containerLayout.SetSpacing(0)
	dm.Container.SetLayout(containerLayout)

	// 创建绘图区域
	dm.BodyWidget, err = walk.NewCustomWidgetPixels(dm.Container, 0, dm.paintDesktop)
	if err != nil {
		return err
	}
	dm.BodyWidget.SetPaintMode(walk.PaintBuffered)
	dm.BodyWidget.SetInvalidatesOnResize(true)
	containerLayout.SetStretchFactor(dm.BodyWidget, 1)

	// 监听尺寸变化
	dm.Container.SizeChanged().Attach(func() {
		go func() {
			time.Sleep(50 * time.Millisecond)
			dm.MainWindow.Synchronize(func() {
				dm.reapplyCardPositions()
			})
		}()
	})

	// 热键
	dm.setupHotkeys()

	// 鼠标事件
	dm.BodyWidget.MouseDown().Attach(dm.handleDesktopMouseDown)

	// 右键菜单子类化
	dm.ContextMenuState.InstallRightClickHandler(
		dm.BodyWidget, dm.MainWindow.Handle(), dm.Manager, dm.Executor, dm.getFreeItemPixelPos,
		func(cmd int) { dm.handleContextMenuCommand(cmd) },
	)

	dm.BodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		for _, c := range dm.Cards {
			c.ClearHover()
		}
		if dm.FreeItemDragActive {
			dm.FreeItemDragMouseX = x
			dm.FreeItemDragMouseY = y
			var screenPt win.POINT
			screenPt.X = int32(x)
			screenPt.Y = int32(y)
			win.ClientToScreen(dm.BodyWidget.Handle(), &screenPt)
			dm.IconDragState.UpdateDropTarget(int(screenPt.X), int(screenPt.Y))
			return
		}
		if dm.checkFreeItemHover(x, y) {
			dm.BodyWidget.Invalidate()
		}
	})

	dm.BodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if dm.FreeItemDragPressed && !dm.FreeItemDragActive {
			dm.FreeItemDragPressed = false
			return
		}
		if !dm.FreeItemDragActive {
			return
		}
		dm.FreeItemDragActive = false
		dm.FreeItemDragPressed = false
		win.ReleaseCapture()
		var screenPt win.POINT
		screenPt.X = int32(x)
		screenPt.Y = int32(y)
		win.ClientToScreen(dm.BodyWidget.Handle(), &screenPt)
		dm.handleFreeItemDrop(int(screenPt.X), int(screenPt.Y))
	})

	// 创建卡片
	dm.createGroupCards()

	// 预加载图标缓存
	freePaths := make([]string, 0)
	for _, item := range dm.Manager.GetUngroupedItems() {
		freePaths = append(freePaths, item.Path)
	}
	if len(freePaths) > 0 {
		ui.GlobalIconBmpCache.LoadAll(freePaths)
	}

	go dm.delayedSetup()
	return nil
}

// delayedSetup 消息循环启动后去边框、嵌入桌面层级
func (dm *DesktopMode) delayedSetup() {
	defer recoverGoroutine("delayedSetup")
	time.Sleep(300 * time.Millisecond)

	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		logger.Debug("delayedSetup: hwnd=%v, pos=(%d,%d,%dx%d)", hwnd, dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
		dm.WinAPI.RemoveWindowMenu(win.HWND(hwnd))
		dm.WinAPI.SetWindowBorderless(win.HWND(hwnd))
		if !dm.WinAPI.SetAsDesktopChild(win.HWND(hwnd)) {
			logger.Error("delayedSetup: SetAsDesktopChild failed, WorkerW not found, exiting")
			os.Exit(1)
		}
		dm.WinAPI.MoveWindow(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
	})

	time.Sleep(100 * time.Millisecond)
	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		dm.WinAPI.SetWindowPosNoRedraw(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW+1, dm.WorkH+1)
	})
	time.Sleep(50 * time.Millisecond)
	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		dm.WinAPI.MoveWindow(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
	})
	time.Sleep(100 * time.Millisecond)

	dm.MainWindow.Synchronize(func() {
		clientBounds := dm.MainWindow.ClientBoundsPixels()
		fullH := clientBounds.Y + clientBounds.Height
		dm.Container.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})
		dm.BodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})
		dm.reapplyCardPositions()
		dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)
		dm.BodyWidget.Invalidate()

		logger.Debug("delayedSetup done: window=(%d,%d,%dx%d)", clientBounds.X, clientBounds.Y, clientBounds.Width, clientBounds.Height)
		dm.Lifecycle.MarkReady()

		go func() {
			defer recoverGoroutine("postLayoutCardFix")
			time.Sleep(200 * time.Millisecond)
			dm.MainWindow.Synchronize(func() {
				dm.reapplyCardPositions()
				dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)
				dm.Manager.ReloadDesktopItems()
				dm.BodyWidget.Invalidate()
			})
		}()
	})
}

// exitDesktopMode 退出桌面模式
func (dm *DesktopMode) exitDesktopMode() {
	dm.Lifecycle.MarkClosing()
	dm.Lifecycle.ExecuteCleanups()
	hwnd := dm.MainWindow.Handle()
	// 从桌面层脱离
	dm.WinAPI.DetachFromDesktop(win.HWND(hwnd))
	dm.MainWindow.Close()
}
