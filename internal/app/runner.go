package app

import (
	"flag"
	"image"
	"image/color"
	"os"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/desktop"
	"desktop_go/internal/dowork"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
	desktopmode "desktop_go/internal/ui/desktop"
)

// 运行模式常量
const (
	ModeWindow  = "window"
	ModeDesktop = "desktop"
)

// Runner 应用运行器
type Runner struct {
	mode      string
	manager   *group.Manager
	winAPI    *desktop.WindowsAPI
	lifecycle *ui.LifecycleManager
	mw        *walk.MainWindow
	ni        *walk.NotifyIcon
	work      *dowork.GoWork // 定时器工作器
	dm        *desktopmode.DesktopMode // 桌面模式 UI（仅桌面模式非 nil）
}

// NewRunner 创建应用运行器
func NewRunner() (*Runner, error) {
	// 初始化日志
	logger.Init("debug", "./log/desktop_go.log")

	r := &Runner{
		manager:   group.NewManager(),
		winAPI:    desktop.NewWindowsAPI(),
		lifecycle: ui.NewLifecycleManager(),
	}

	// 初始化字体配置（从 config 读取）
	cfg := r.manager.GetConfig()
	ui.InitIconFont(cfg.IconFontName, cfg.IconFontSize)
	ui.InitCardFont(cfg.CardFontName, cfg.CardFontSize)

	// 检测运行模式
	r.mode = r.detectMode()

	// 初始加载桌面项目
	r.manager.ReloadDesktopItems()

	// 初始化系统桌面项（首次运行自动添加）
	r.initSystemItems()

	return r, nil
}

// detectMode 检测运行模式
func (r *Runner) detectMode() string {
	// 1. 命令行参数（最高优先级）
	var windowMode, desktopMode bool
	flag.BoolVar(&windowMode, "window", false, "窗口模式")
	flag.BoolVar(&windowMode, "w", false, "窗口模式（简写）")
	flag.BoolVar(&desktopMode, "desktop", false, "桌面模式")
	flag.BoolVar(&desktopMode, "d", false, "桌面模式（简写）")
	flag.Parse()

	if desktopMode {
		return ModeDesktop
	}
	if windowMode {
		return ModeWindow
	}

	// 2. 环境变量
	if os.Getenv("DESKTOPGO_MODE") == "desktop" {
		return ModeDesktop
	}

	// 3. 默认桌面模式（暂时）
	return ModeDesktop
}

// Run 运行应用
func (r *Runner) Run() error {
	switch r.mode {
	case ModeDesktop:
		return r.runDesktopMode()
	default:
		return r.runWindowMode()
	}
}

// runDesktopMode 运行桌面模式
func (r *Runner) runDesktopMode() error {
	// 使用工作区尺寸（排除任务栏），不遮挡任务栏
	left, top, right, bottom := r.winAPI.GetWorkAreaRect()
	workW := right - left
	workH := bottom - top
	_, _ = left, top

	var mw *walk.MainWindow

	cfg := MainWindow{
		AssignTo: &mw,
		Title:    "DesktopGo",
		Size:     Size{Width: workW, Height: workH},
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
	}

	if err := cfg.Create(); err != nil {
		return err
	}
	r.mw = mw
	r.setWindowIcon(mw)

	// 关闭事件 - 必须在 Setup 之前绑定，防止 Setup 中触发关闭
	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		// 关闭时隐藏到托盘
		*canceled = true
		mw.SetVisible(false)
	})

	// 设置桌面模式 UI
	r.work = dowork.NewGoWork()

	dm := desktopmode.NewDesktopMode(mw, r.manager, r.winAPI, r.lifecycle, r.work)
	r.dm = dm
	if err := dm.Setup(); err != nil {
		return err
	}

	// 设置系统托盘
	r.setupNotifyIcon()

	// 监听数据变更
	r.manager.SetOnChange(func() {
		dm.Post(func() {
			dm.Refresh()
		})
	})

	// 注册清理
	r.lifecycle.RegisterCleanup(func() {
		if r.ni != nil {
			r.ni.Dispose()
		}
	})

	// 启动定时器（在清理时优先停止）
	r.startTimer()

	mw.Run()
	return nil
}

// runWindowMode 运行窗口模式
func (r *Runner) runWindowMode() error {
	var mw *walk.MainWindow

	cfg := MainWindow{
		AssignTo: &mw,
		Title:    "DesktopGo",
		MinSize:  Size{Width: 800, Height: 600},
		Size:     Size{Width: 1000, Height: 700},
		Layout:   VBox{MarginsZero: true},
	}

	if err := cfg.Create(); err != nil {
		return err
	}
	r.mw = mw
	r.setWindowIcon(mw)

	// 设置窗口模式 UI
	r.setupWindowModeUI(mw)

	// 设置系统托盘
	r.setupNotifyIcon()

	// 关闭事件 - 隐藏到托盘
	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		mw.SetVisible(false)
	})

	r.lifecycle.MarkReady()

	// 启动定时器（在清理时优先停止）
	r.startTimer()

	mw.Run()
	return nil
}

// setupWindowModeUI 设置窗口模式 UI
func (r *Runner) setupWindowModeUI(mw *walk.MainWindow) {
	// 工具栏
	toolbar, _ := walk.NewComposite(mw)
	toolbar.SetLayout(walk.NewHBoxLayout())

	// 标题
	titleLabel, _ := walk.NewLabel(toolbar)
	titleLabel.SetText("DesktopGo")
	font, _ := walk.NewFont("Microsoft YaHei", 16, walk.FontBold)
	if font != nil {
		titleLabel.SetFont(font)
	}

	// 新建分组按钮
	addBtn, _ := walk.NewPushButton(toolbar)
	addBtn.SetText("+ 新建分组")
	addBtn.Clicked().Attach(func() {
		name, ok := ui.ShowInputDialog(mw, "新建分组", "请输入分组名称：", "")
		if ok && name != "" {
			r.manager.CreateGroup(name, "#30343CBD")
			r.refreshWindowUI(mw)
		}
	})

	// 分组卡片区域（使用 CustomWidget 绘制网格）
	body, _ := walk.NewCustomWidgetPixels(mw, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return r.paintWindowModeBody(canvas, mw)
	})
	body.SetPaintMode(walk.PaintBuffered)
	body.SetInvalidatesOnResize(true)

	// 让 body 占满剩余空间
	if layout, ok := mw.Layout().(*walk.BoxLayout); ok {
		layout.SetStretchFactor(body, 1)
	}

	// 数据变更时刷新
	r.manager.SetOnChange(func() {
		mw.Synchronize(func() {
			body.Invalidate()
		})
	})
}

// paintWindowModeBody 绘制窗口模式的分组卡片网格
func (r *Runner) paintWindowModeBody(canvas *walk.Canvas, mw *walk.MainWindow) error {
	groups := r.manager.GetGroups()

	// 获取绘制区域的实际宽度
	var areaWidth int
	if children := mw.Children(); children != nil && children.Len() > 1 {
		// body 是第二个子控件
		if body := children.At(children.Len() - 1); body != nil {
			areaWidth = body.BoundsPixels().Width
		}
	}
	if areaWidth <= 0 {
		areaWidth = mw.ClientBoundsPixels().Width
	}

	// 3列网格布局
	cols := 3
	padding := 16
	cardW := (areaWidth - padding*(cols+1)) / cols
	cardH := 200
	startY := padding

	for i, grp := range groups {
		col := i % cols
		row := i / cols
		x := padding + col*(cardW+padding)
		y := startY + row*(cardH+padding)

		r.paintWindowCard(canvas, grp, x, y, cardW, cardH)
	}

	return nil
}

// paintWindowCard 绘制窗口模式的单个卡片
func (r *Runner) paintWindowCard(canvas *walk.Canvas, grp config.Group, x, y, w, h int) {
	// 绘制卡片背景
	c := ui.ParseHexColor(grp.Color)
	gc := &ui.GroupCard{}
	_ = gc // 避免循环依赖，直接绘制

	bgImg := createSolidImage(w, h, c)
	bmp, err := walk.NewBitmapFromImage(bgImg)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{X: x, Y: y, Width: w, Height: h}, c.A)
	}

	// 标题
	font := ui.GetCardTitleFont()
	if font != nil {
		defer font.Dispose()
		titleBounds := walk.Rectangle{X: x + 8, Y: y + 4, Width: w - 16, Height: 28}
		canvas.DrawTextPixels(grp.Name, font, walk.RGB(0xFF, 0xFF, 0xFF), titleBounds, walk.TextSingleLine|walk.TextVCenter)
	}

	// 分隔线
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0xFF, 0xFF, 0xFF))
	if pen != nil {
		defer pen.Dispose()
		canvas.DrawLinePixels(pen, walk.Point{X: x + 4, Y: y + 30}, walk.Point{X: x + w - 4, Y: y + 30})
	}

	// 绘制项目图标
	items := r.manager.GetGroupItems(grp.Name)
	colWidth := ui.TileWidth()
	maxCols := (w - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}

	itemFont := ui.GetIconFont()
	if itemFont != nil {
		defer itemFont.Dispose()
	}

	for idx, item := range items {
		col := idx % maxCols
		row := idx / maxCols
		ix := x + 4 + col*colWidth
		iy := y + 34 + row*ui.TileHeight()

		if iy+ui.TileHeight() > y+h {
			break
		}

		// 显示名称
		if itemFont != nil {
			name := ui.TruncateText(item.Name, 7)
			textBounds := walk.Rectangle{X: ix, Y: iy + 50, Width: colWidth, Height: 16}
			canvas.DrawTextPixels(name, itemFont, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
	}
}

// refreshWindowUI 刷新窗口模式 UI
func (r *Runner) refreshWindowUI(mw *walk.MainWindow) {
	mw.Invalidate()
}

// setWindowIcon 设置窗口标题栏图标
func (r *Runner) setWindowIcon(mw *walk.MainWindow) {
	// 尝试从 exe 嵌入资源加载（rsrc 使用 ID 7 作为 app icon）
	if icon, err := walk.NewIconFromResourceId(7); err == nil {
		mw.SetIcon(icon)
		return
	}

	// 回退：从生成的 ico 文件加载
	iconPath := ui.SaveAppIconToFile()
	if iconPath != "" {
		icon, err := walk.NewIconFromFile(iconPath)
		if err == nil {
			mw.SetIcon(icon)
			return
		}
	}

	// 最终回退：使用系统默认应用图标
	mw.SetIcon(walk.IconApplication())
}

// 托盘右键菜单命令 ID
const (
	trayCmdShowHide = 0x6001
	trayCmdExit     = 0x6002
)

// Win32 菜单 API
var (
	user32Tray         = syscall.NewLazyDLL("user32.dll")
	procCreatePopupMenuTray = user32Tray.NewProc("CreatePopupMenu")
	procDestroyMenuTray     = user32Tray.NewProc("DestroyMenu")
	procAppendMenuW_tray    = user32Tray.NewProc("AppendMenuW")
	procTrackPopupMenuTray  = user32Tray.NewProc("TrackPopupMenu")
	procSetForegroundTray   = user32Tray.NewProc("SetForegroundWindow")
)

const (
	mfString    = 0x00000000
	mfSeparator = 0x00000800
	tpmReturnCmd = 0x00000100
)

// showTrayContextMenu 显示托盘右键菜单（使用 Win32 直接创建，绕过 walk 内部消息路由）
func (r *Runner) showTrayContextMenu() {
	hMenu, _, _ := procCreatePopupMenuTray.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenuTray.Call(hMenu)

	showHideText := "显示/隐藏"
	if !r.mw.Visible() {
		showHideText = "显示/隐藏"
	}
	procAppendMenuW_tray.Call(hMenu, mfString, trayCmdShowHide, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(showHideText))))
	procAppendMenuW_tray.Call(hMenu, mfSeparator, 0, 0)
	procAppendMenuW_tray.Call(hMenu, mfString, trayCmdExit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("退出"))))

	procSetForegroundTray.Call(uintptr(r.mw.Handle()))

	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return
	}

	cmd, _, _ := procTrackPopupMenuTray.Call(hMenu, tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(r.mw.Handle()), 0)
	if cmd == 0 {
		return
	}

	switch uintptr(cmd) {
	case trayCmdShowHide:
		if r.mw.Visible() {
			r.mw.SetVisible(false)
		} else {
			r.mw.SetVisible(true)
			if r.mode == ModeDesktop {
				r.showDesktopMode()
			} else {
				r.winAPI.ForceShowAndRaise(r.mw.Handle())
			}
		}
	case trayCmdExit:
		r.lifecycle.MarkClosing()
		r.lifecycle.ExecuteCleanups()
		if r.mode == ModeDesktop {
			r.winAPI.DetachFromDesktop(r.mw.Handle())
			r.winAPI.ShowDesktopIcons()
		}
		if r.ni != nil {
			r.ni.Dispose()
		}
		walk.App().Exit(0)
	}
}

// setupNotifyIcon 设置系统托盘图标
func (r *Runner) setupNotifyIcon() {
	var err error
	r.ni, err = walk.NewNotifyIcon(r.mw)
	if err != nil {
		return
	}

	// 设置托盘图标 - 优先从 exe 嵌入资源加载（rsrc 使用 ID 7）
	trayIconSet := false
	if icon, err := walk.NewIconFromResourceId(7); err == nil {
		r.ni.SetIcon(icon)
		trayIconSet = true
	}
	if !trayIconSet {
		iconPath := ui.SaveTrayIconToFile()
		if iconPath != "" {
			if icon, err := walk.NewIconFromFile(iconPath); err == nil {
				r.ni.SetIcon(icon)
				trayIconSet = true
			}
		}
	}
	if !trayIconSet {
		r.ni.SetIcon(walk.IconApplication())
	}

	r.ni.SetToolTip("DesktopGo - 桌面分组管理")
	r.ni.SetVisible(true)

	// 注意：不使用 walk 内置的 ContextMenu（窗口嵌入桌面后消息路由失效），
	// 右键菜单在 MouseUp 中通过 Win32 直接显示

	// 双击托盘图标显示窗口
	r.ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			r.mw.SetVisible(true)
			if r.mode == ModeDesktop {
				r.showDesktopMode()
			} else {
				r.winAPI.ForceShowAndRaise(r.mw.Handle())
			}
		}
	})

	// 右键弹出菜单（使用 Win32 直接创建，绕过 walk 消息路由）
	r.ni.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.RightButton {
			r.showTrayContextMenu()
		}
	})
}

// initSystemItems 初始化系统桌面项（首次运行自动添加）
func (r *Runner) initSystemItems() {
	if !r.manager.HasSystemItem("MyComputerFolder") {
		r.manager.AddSystemItem("MyComputerFolder", "此电脑")
	}
	if !r.manager.HasSystemItem("NetworkFolder") {
		r.manager.AddSystemItem("NetworkFolder", "网络")
	}
	if !r.manager.HasSystemItem("RecycleBinFolder") {
		r.manager.AddSystemItem("RecycleBinFolder", "回收站")
	}
}

// showDesktopMode 桌面模式下窗口从隐藏变为可见时触发完整刷新
// 隐藏期间数据变更（桌面文件新增/删除、分组调整等）被跳过，显示时需要全部重载
func (r *Runner) showDesktopMode() {
	if r.dm == nil {
		return
	}
	dm := r.dm
	hwnd := r.mw.Handle()

	// 确保窗口位置正确（嵌入式桌面可能需要重新定位）
	r.winAPI.MoveWindow(hwnd, dm.WorkX, dm.WorkY, dm.WorkW, dm.WorkH)

	// 重新设置 Container 和 BodyWidget 的 bounds（确保布局正确）
	clientBounds := r.mw.ClientBoundsPixels()
	fullH := clientBounds.Y + clientBounds.Height
	dm.Container.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})
	dm.BodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: dm.WorkW, Height: fullH})

	// 重新加载桌面项目（同步隐藏期间可能错过的文件变更）
	r.manager.ReloadDesktopItems()

	// 重新应用卡片位置并完整刷新
	dm.ReapplyCardPositionsAndRefresh()
}

// startTimer 启动定时器，注册清理时优先停止
func (r *Runner) startTimer() {
	if r.work == nil {
		r.work = dowork.NewGoWork()
	}
	r.work.Run()

	// 桌面模式下添加定时自检恢复（每 30 秒检测窗口状态）
	// 使用缓存状态判断，无变化时跳过耗时检测，避免不必要的 UI 线程开销
	if r.mode == ModeDesktop && r.dm != nil {
		r.work.AddTimer(30*1000, func() {
			r.dm.Post(func() {
				hwnd := r.mw.Handle()

				// 检测窗口可见性
				visible := r.winAPI.IsWindowVisible(hwnd)
				if visible != r.dm.HealthLastVisible() {
					r.dm.SetHealthLastVisible(visible)
					if !visible {
						logger.Debug("healthcheck: window not visible, restoring...")
						r.mw.SetVisible(true)
						r.winAPI.MoveWindow(hwnd, r.dm.WorkX, r.dm.WorkY, r.dm.WorkW, r.dm.WorkH)
						r.dm.ReapplyCardPositionsAndRefresh()
					}
				}

				// 检测父窗口是否仍是 WorkerW
				parent := win.GetParent(hwnd)
				if parent != r.dm.HealthLastParent() {
					r.dm.SetHealthLastParent(parent)
					shellWorkerW := r.winAPI.FindShellWorkerW()
					if shellWorkerW != 0 && parent != shellWorkerW {
						logger.Debug("healthcheck: parent changed (parent=%v, shellWorkerW=%v), re-embedding...", parent, shellWorkerW)
						if !r.winAPI.SetAsDesktopChild(hwnd) {
							logger.Error("healthcheck: SetAsDesktopChild failed, cannot re-embed into WorkerW")
						}
						r.winAPI.MoveWindow(hwnd, r.dm.WorkX, r.dm.WorkY, r.dm.WorkW, r.dm.WorkH)
						r.dm.ReapplyCardPositionsAndRefresh()
					}
				}
			})
		})
	}

	// 注册清理（后注册先执行，确保定时器优先停止）
	r.lifecycle.RegisterCleanup(func() {
		if r.work != nil {
			logger.Debug("停止定时器...")
			r.work.Stop()
			r.work = nil
		}
	})
}

// createSolidImage 创建纯色图像
func createSolidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
