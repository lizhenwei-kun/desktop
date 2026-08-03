package desktop

import (
	"os"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/logger"
	"desktop_go/internal/safego"
	"desktop_go/internal/ui"
)

// Setup 设置桌面模式 UI
func (dm *DesktopMode) Setup() error {
	logger.Debug("Setup: DPI=%d, bounds=(%d,%d,%dx%d)",
		dm.MainWindow.DPI(), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)

	// === 从持久化配置恢复状态 ===
	// 1. 恢复图标档位（覆盖 NewRunner 中 AutoSelectIconSizeByResolution 的自动选择）
	savedLevel := dm.Manager.GetIconSizeLevel()
	ui.SetDesktopIconSize(savedLevel)
	logger.Debug("Setup: restored icon size level=%d", savedLevel)

	// 2. 恢复"自动排列"开关
	dm.IsAutoArrange = dm.Manager.GetAutoArrangeEnabled()
	if dm.IsAutoArrange {
		dm.autoArrangeIcons()
	}

	// 3. 恢复"将图标与网格对齐"开关
	dm.IsAlignToGrid = dm.Manager.GetAlignToGridEnabled()
	if dm.IsAlignToGrid {
		dm.snapAllUngroupedToGrid()
	}

	// === 注入子结构体能力 ===
	dm.CardDragOutline.Inject(dm.WorkX, dm.WorkY)
	dm.CardDragOutline.SetWorkArea(dm.WorkW, dm.WorkH)
	dm.ResizeOutlineState.Inject(dm.WorkX, dm.WorkY)
	dm.ResizeOutlineState.SetWorkArea(dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
	// 应用参考线颜色（拖动和缩放共用同一颜色，从配置读取，默认红色）
	r, g, b := byte(0xFF), byte(0x00), byte(0x00)
	if gc := dm.Manager.GetConfig(); gc != nil && gc.GuideLineColor != "" {
		c := ui.ParseHexColor(gc.GuideLineColor)
		r, g, b = c.R, c.G, c.B
	}
	dm.CardDragOutline.SetGuideColor(r, g, b)
	dm.ResizeOutlineState.SetGuideColor(r, g, b)

	// 设置主窗口尺寸为工作区
	dm.MainWindow.SetBoundsPixels(walk.Rectangle{
		X: dm.WorkX, Y: dm.WorkY,
		Width: dm.WorkW, Height: dm.WorkH,
	})

	// 设置窗口背景色为深色（消除白边）
	bg, _ := walk.NewSolidColorBrush(walk.RGB(0x1A, 0x1A, 0x2E))
	dm.MainWindow.SetBackground(bg)

	// 预加载壁纸（按工作区尺寸）
	dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)

	// 创建缓存的绘制对象
	bgBrush, _ := walk.NewSolidColorBrush(walk.RGB(0x1A, 0x1A, 0x2E))
	if bgBrush != nil {
		dm.bgBrush = bgBrush
	}
	btnBrush, _ := walk.NewSolidColorBrush(walk.RGB(0x30, 0x34, 0x3C))
	if btnBrush != nil {
		dm.btnBrush = btnBrush
	}
	dm.toolbarFont, _ = walk.NewFont("Microsoft YaHei", 14, 0)

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
		safego.Go("containerSizeChanged", func() {
			time.Sleep(50 * time.Millisecond)
			dm.Post(func() {
				dm.reapplyCardPositions()
			})
		})
	})

	// 热键
	dm.setupHotkeys()

	// 鼠标事件
	dm.BodyWidget.MouseDown().Attach(dm.handleDesktopMouseDown)

	// 右键菜单子类化
	dm.ContextMenuState.InstallRightClickHandler(
		dm.BodyWidget, dm.MainWindow.Handle(), dm.Manager, dm.Executor, dm.getFreeItemPixelPos,
		func() []*ui.GroupCard { return dm.Cards },
		func(cmd int) { dm.handleContextMenuCommand(cmd) },
		dm.addNewCard,
		dm.refreshRecycleBinAfterEmpty,
	)

	// 注册外部文件拖放（从桌面/资源管理器拖文件到应用）
	dm.RegisterExternalDropTarget()

	// 安装主窗口系统消息监听（子类化 subclassProc）：拦截并处理 WM_POWERBROADCAST / WM_DISPLAYCHANGE。
	// 这是息屏不重绘、唤醒后刷新、以及最小化拦截的基础——此前该子类化从未被安装导致系统消息丢失。
	// 注意：主窗口被 SetAsDesktopChild 设为 WorkerW 子窗口后收不到顶层广播，
	// 但 PowerSettingRegisterNotification 的 PBT_POWERSETTINGCHANGE 直接发往本窗口，仍能可靠收到。
	mwHandle := dm.MainWindow.Handle()
	dm.WinAPI.InstallMinimizeBlock(mwHandle)
	dm.Lifecycle.RegisterCleanup(func() {
		dm.WinAPI.RemoveMinimizeBlock(mwHandle)
	})

	dm.BodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		if dm.DragActive {
			dm.DragMouseX = x
			dm.DragMouseY = y
			var screenPt win.POINT
			screenPt.X = int32(x)
			screenPt.Y = int32(y)
			win.ClientToScreen(dm.BodyWidget.Handle(), &screenPt)
			dm.DragScreenX = int(screenPt.X)
			dm.DragScreenY = int(screenPt.Y)
			dm.updateDropTarget(int(screenPt.X), int(screenPt.Y))
			// 移动幽灵窗口到鼠标位置
			dm.moveGhostWindow(int(screenPt.X), int(screenPt.Y))
			return
		}
		// checkItemHover 内部通过 SetHoveredPath 精准重绘变化图标，无需全量重绘
		dm.checkItemHover(x, y)
	})

	dm.BodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		dm.DragPressed = false
		if !dm.DragActive {
			return
		}
		win.ReleaseCapture()

		// 使用 GetMessagePos 获取鼠标释放时的屏幕坐标（比 ClientToScreen 更可靠）
		msgPos, _, _ := procGetMessagePos.Call()
		screenX := int(int16(msgPos & 0xFFFF))
		screenY := int(int16((msgPos >> 16) & 0xFFFF))

		// 检测释放位置是否在应用窗口外部
		mwHwnd := dm.MainWindow.Handle()
		var mwRect win.RECT
		win.GetWindowRect(mwHwnd, &mwRect)

		isOutside := screenX < int(mwRect.Left) || screenX > int(mwRect.Right) ||
			screenY < int(mwRect.Top) || screenY > int(mwRect.Bottom)

		if isOutside {
			// 拖到应用外部：将文件路径复制到剪贴板（CF_HDROP），供外部程序粘贴
			// 应用内保留原图标，不移动文件
			ui.CopyFilesToClipboard([]string{dm.DragItemPath})
			logger.Debug("MouseUp: dropped outside app(%d,%d-%dx%d) at screen(%d,%d), copied %s to clipboard",
				mwRect.Left, mwRect.Top, mwRect.Right-mwRect.Left, mwRect.Bottom-mwRect.Top,
				screenX, screenY, dm.DragItemPath)
			dm.clearDragState()
			dm.InvalidateBody()
		} else {
			// 在应用内部释放：正常内部拖放逻辑
			dm.handleIconDrop(screenX, screenY)
		}
	})

	// 创建卡片
	dm.createGroupCards()

	// 预加载所有图标缓存（分组+未分组）
	ui.GlobalIconBmpCache.LoadAllFromManager(dm.Manager)

	// 启动回收站状态定时监测
	dm.initRecycleBinMonitor()

	// 启动桌面目录文件变更监听
	dm.initDesktopWatcher()

	// 启动右键菜单缓存定时更新
	dm.initContextMenuCache()

	safego.Go("delayedSetup", dm.delayedSetup)
	return nil
}

// delayedSetup 消息循环启动后去边框、嵌入桌面层级
func (dm *DesktopMode) delayedSetup() {
	time.Sleep(300 * time.Millisecond)

	dm.Post(func() {
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
	dm.Post(func() {
		hwnd := dm.MainWindow.Handle()
		dm.WinAPI.SetWindowPosNoRedraw(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW+1, dm.WorkH+1)
	})
	time.Sleep(50 * time.Millisecond)
	dm.Post(func() {
		hwnd := dm.MainWindow.Handle()
		dm.WinAPI.MoveWindow(win.HWND(hwnd), dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)
	})
	time.Sleep(100 * time.Millisecond)

	dm.Post(func() {
		clientBounds := dm.MainWindow.ClientBoundsPixels()
		fullH := clientBounds.Y + clientBounds.Height
		dm.Container.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})
		dm.BodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})
		dm.reapplyCardPositions()
		dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)
		dm.InvalidateBody()

		// 注册系统事件回调（电源恢复、显示变更、会话结束等自动刷新桌面）
		dm.WinAPI.SetOnSystemEvent(func() {
			dm.Post(func() {
				logger.Debug("system event: refreshing desktop")
				dm.refreshDesktop()
			})
		})

		// 注册显示器电源通知：监听"仅显示器息屏"（系统仍在运行），
		// 息屏时标记 screenOff 抑制重绘，唤醒后再刷新。系统睡眠/唤醒由 WM_POWERBROADCAST 挂起/恢复消息覆盖。
		dm.WinAPI.RegisterMonitorPower(dm.MainWindow.Handle())
		// 退出桌面模式时注销显示器电源通知（LIFO 清理）
		dm.Lifecycle.RegisterCleanup(func() {
			dm.WinAPI.UnregisterMonitorPower()
		})

		// 注册 DPI 变化回调（窗口在不同 DPI 显示器间移动时触发）
		dm.WinAPI.SetOnDPIChanged(func(newDPI int) {
			dm.Post(func() {
				logger.Debug("DPI changed: newDPI=%d, refreshing", newDPI)
				ui.SetCurrentDPI(newDPI)
				// 重新加载壁纸（DPI 变化后位图需要重新生成）
				dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)
				dm.reapplyCardPositions()
				dm.InvalidateBody()
			})
		})

		logger.Debug("delayedSetup done: window=(%d,%d,%dx%d)", clientBounds.X, clientBounds.Y, clientBounds.Width, clientBounds.Height)
		dm.Lifecycle.MarkReady()

		safego.Go("postLayoutCardFix", func() {
			time.Sleep(200 * time.Millisecond)
			dm.Post(func() {
				dm.reapplyCardPositions()
				dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)
				// 抑制 ReloadDesktopItems 末尾触发的 notifyChange 回调（会投递 dm.Refresh() 到 UI 线程）。
				// 此处已通过下方的 InvalidateBody 主动重绘桌面，再触发 dm.Refresh() 会造成重复刷新。
				dm.Manager.SuppressNotify()
				dm.Manager.ReloadDesktopItems()
				dm.Manager.UnsuppressNotify()
				dm.InvalidateBody()
			})
		})
	})
}

// exitDesktopMode 退出桌面模式
func (dm *DesktopMode) exitDesktopMode() {
	dm.Lifecycle.MarkClosing()
	dm.Lifecycle.ExecuteCleanups()
	dm.ResizeOutlineState.resizeOutline.destroy()
	dm.CardDragOutline.destroyDragGhost()
	// 停止桌面目录监听
	dm.stopDesktopWatcher()
	// 注销外部文件拖放
	dm.UnregisterExternalDropTarget()
	// 释放缓存的绘制对象
	if dm.bgBrush != nil {
		dm.bgBrush.Dispose()
		dm.bgBrush = nil
	}
	if dm.btnBrush != nil {
		dm.btnBrush.Dispose()
		dm.btnBrush = nil
	}
	if dm.toolbarFont != nil {
		dm.toolbarFont.Dispose()
		dm.toolbarFont = nil
	}
	hwnd := dm.MainWindow.Handle()
	// 从桌面层脱离
	dm.WinAPI.DetachFromDesktop(win.HWND(hwnd))
	dm.MainWindow.Close()
}
