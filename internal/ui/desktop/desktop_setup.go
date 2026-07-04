package desktop

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

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

	// 设置主窗口尺寸为工作区
	dm.MainWindow.SetBoundsPixels(walk.Rectangle{
		X: dm.WorkX, Y: dm.WorkY,
		Width: dm.WorkW, Height: dm.WorkH,
	})

	// 设置窗口背景色为深色（消除白边）
	bg, _ := walk.NewSolidColorBrush(walk.RGB(0x1A, 0x1A, 0x2E))
	dm.MainWindow.SetBackground(bg)

	// 预加载壁纸
	dm.loadWallpaper()

	// 创建一个 Composite 容器放在 VBox 中，让它占满空间
	var err error
	dm.Container, err = walk.NewComposite(dm.MainWindow)
	if err != nil {
		return err
	}

	// 让 container 在 VBox 中占满剩余空间
	if layout, ok := dm.MainWindow.Layout().(*walk.BoxLayout); ok {
		layout.SetStretchFactor(dm.Container, 1)
	}

	// container 使用 VBox（margins/spacing 为0），walk 要求所有 Container 必须有 Layout
	containerLayout := walk.NewVBoxLayout()
	containerLayout.SetMargins(walk.Margins{})
	containerLayout.SetSpacing(0)
	dm.Container.SetLayout(containerLayout)

	// 创建主绘制区域（背景 + 壁纸 + 卡片内容）放在 container 中
	dm.BodyWidget, err = walk.NewCustomWidgetPixels(dm.Container, 0, dm.paintDesktop)
	if err != nil {
		return err
	}
	dm.BodyWidget.SetPaintMode(walk.PaintBuffered)
	dm.BodyWidget.SetInvalidatesOnResize(true)

	// bodyWidget 占满 container
	containerLayout.SetStretchFactor(dm.BodyWidget, 1)

	// 监听 container 尺寸变化，延迟重新应用卡片绝对定位
	// （布局是异步执行的，需要等布局完成后再覆盖）
	dm.Container.SizeChanged().Attach(func() {
		go func() {
			time.Sleep(50 * time.Millisecond)
			dm.MainWindow.Synchronize(func() {
				dm.reapplyCardPositions()
			})
		}()
	})

	// 设置键盘快捷键 Alt+F6 退出全屏
	dm.setupHotkeys()

	// 鼠标事件
	dm.BodyWidget.MouseDown().Attach(dm.handleDesktopMouseDown)

	// 安装窗口子类化，拦截 WM_RBUTTONDOWN 处理右键菜单
	// （walk 的 MouseDown 事件对右击返回的 button=0，无法正确区分）
	dm.installRightClickHandler()

	dm.BodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		// 光标在桌面上，清除所有卡片的悬停状态
		for _, c := range dm.Cards {
			c.ClearHover()
		}
		// 未分组图标拖拽中
		if dm.FreeItemDragActive {
			dm.FreeItemDragMouseX = x
			dm.FreeItemDragMouseY = y

			var screenPt win.POINT
			screenPt.X = int32(x)
			screenPt.Y = int32(y)
			win.ClientToScreen(dm.BodyWidget.Handle(), &screenPt)
			dm.updateDropTarget(int(screenPt.X), int(screenPt.Y))
			return
		}

		// 普通悬停检测（未分组图标）
		if dm.checkFreeItemHover(x, y) {
			dm.BodyWidget.Invalidate()
		}
	})

	dm.BodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		// 清除未分组图标长按状态（防止快速点击后误触发）
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

	// 创建分组卡片（在 container 中，绝对定位）
	dm.createGroupCards()

	// 预加载未分组图标的 bitmap 缓存
	freePaths := make([]string, 0)
	for _, item := range dm.Manager.GetUngroupedItems() {
		freePaths = append(freePaths, item.Path)
	}
	if len(freePaths) > 0 {
		ui.GlobalIconBmpCache.LoadAll(freePaths)
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

	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		logger.Debug("delayedSetup: hwnd=%v, pos=(%d,%d,%dx%d)",
			hwnd, dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)

		// 移除菜单栏（walk MainWindow 默认创建了空菜单栏，占用顶部空间）
		dm.WinAPI.RemoveWindowMenu(win.HWND(hwnd))

		// 去除边框（使用全屏尺寸，嵌入桌面层后覆盖整个屏幕）
		dm.WinAPI.SetWindowBorderless(win.HWND(hwnd))

		// 将窗口嵌入桌面层级（SetParent 到 WorkerW）
		if !dm.WinAPI.SetAsDesktopChild(win.HWND(hwnd)) {
			logger.Error("delayedSetup: SetAsDesktopChild failed, WorkerW not found, exiting")
			os.Exit(1)
		}
		logger.Debug("delayedSetup: SetAsDesktopChild done")

		// 嵌入后设置窗口位置铺满工作区
		dm.WinAPI.MoveWindow(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
		logger.Debug("delayedSetup: MoveWindow done")
	})

	// 去边框后客户区变大，通过尺寸变化触发 walk 重新布局（不重绘避免闪烁）
	time.Sleep(100 * time.Millisecond)

	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		// +1 不重绘触发 WM_WINDOWPOSCHANGED，让 walk 用新客户区尺寸重新布局
		dm.WinAPI.SetWindowPosNoRedraw(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW+1, dm.WorkH+1)
	})

	time.Sleep(50 * time.Millisecond)

	dm.MainWindow.Synchronize(func() {
		hwnd := dm.MainWindow.Handle()
		// 恢复正确尺寸
		dm.WinAPI.MoveWindow(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
	})

	time.Sleep(100 * time.Millisecond)

	dm.MainWindow.Synchronize(func() {
		// 最终确认：强制 container 和 bodyWidget 覆盖整个客户区
		clientBounds := dm.MainWindow.ClientBoundsPixels()
		logger.Debug("delayedSetup: clientBounds=(%d,%d,%dx%d)",
			clientBounds.X, clientBounds.Y, clientBounds.Width, clientBounds.Height)

		// 强制 container 铺满整个窗口客户区
		fullH := clientBounds.Y + clientBounds.Height
		dm.Container.SetBoundsPixels(walk.Rectangle{
			X: 0, Y: 0,
			Width: dm.WorkW, Height: fullH,
		})
		dm.BodyWidget.SetBoundsPixels(walk.Rectangle{
			X: 0, Y: 0,
			Width: dm.WorkW, Height: fullH,
		})

		// 强制重新应用卡片的绝对定位
		dm.reapplyCardPositions()

		// 重新加载壁纸（窗口尺寸已最终确定，确保壁纸按正确尺寸加载）
		dm.loadWallpaper()

		// 手动触发一次完整重绘
		dm.BodyWidget.Invalidate()

		finalBounds := dm.MainWindow.BoundsPixels()
		containerBounds := dm.Container.BoundsPixels()
		bodyBounds := dm.BodyWidget.BoundsPixels()
		logger.Debug("delayedSetup done: window=(%d,%d,%dx%d), container=(%d,%d,%dx%d), body=(%d,%d,%dx%d)",
			finalBounds.X, finalBounds.Y, finalBounds.Width, finalBounds.Height,
			containerBounds.X, containerBounds.Y, containerBounds.Width, containerBounds.Height,
			bodyBounds.X, bodyBounds.Y, bodyBounds.Width, bodyBounds.Height)

		dm.Lifecycle.MarkReady()

		// 延迟再次确认卡片位置并刷新桌面（防止异步布局覆盖，确保未分组图标正确显示）
		go func() {
			defer recoverGoroutine("postLayoutCardFix")
			time.Sleep(200 * time.Millisecond)
			dm.MainWindow.Synchronize(func() {
				logger.Debug("postLayoutCardFix: reapply after 200ms delay, cards=%d", len(dm.Cards))
				dm.reapplyCardPositions()
				// 重新加载壁纸和桌面项（与右键刷新效果一致），确保初始显示完整
				dm.loadWallpaper()
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
