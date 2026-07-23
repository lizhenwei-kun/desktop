package desktop

import (
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/desktop"
	"desktop_go/internal/dowork"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// ScreenInfo 屏幕/工作区尺寸信息
type ScreenInfo struct {
	ScreenW int
	ScreenH int
	WorkX   int
	WorkY   int
	WorkW   int
	WorkH   int
}

// WallpaperState 壁纸缓存状态
type WallpaperState struct {
	WallpaperBmp *walk.Bitmap // 缓存的壁纸 bitmap
}

// WallpaperInject 壁纸注入
type WallpaperInject struct {
	DPI        func() int
	WorkWidth  func() int
	WorkHeight func() int
}

func (s *WallpaperState) Inject(WallpaperInject) {}

// UnifiedDragState 统一图标拖拽状态（未分组 + 分组内图标共用）
type UnifiedDragState struct {
	DragActive      bool   // 是否有拖拽正在进行
	DragItemPath    string // 被拖拽项目的路径
	DragItemName    string // 被拖拽项目的显示名
	DragSourceGroup string // 来源分组，"" 表示从未分组拖出
	GhostBmp        *walk.Bitmap // 拖拽幽灵 bitmap
	DragMouseX      int    // bodyWidget 客户区坐标 X
	DragMouseY      int    // bodyWidget 客户区坐标 Y
	DragScreenX     int    // 屏幕绝对坐标 X
	DragScreenY     int    // 屏幕绝对坐标 Y
	DropToDesktop   bool   // 拖拽悬停在桌面空白区域
	LastMoveTime    time.Time // 上次移动时间

	// 拖拽来源
	DragPressed     bool          // 鼠标在图标上按下（MouseDown 设，MouseUp 清）
	SourceCard      *ui.GroupCard // 非 nil 表示从卡片拖出，nil 表示从未分组拖出
	SourceItemIdx   int           // 在源卡片中的索引（-1 表示未分组）
	SourceItem      group.GroupItem // 被拖拽的项目信息

	// 双击检测（未分组图标）
	LastClickTime time.Time
	LastClickPath string

	// 拖拽幽灵顶层窗口（裸 HWND，WS_EX_LAYERED|TOPMOST，GDI 绘制）
	GhostHwnd win.HWND
}

// UnifiedSelectionState 统一选中/悬停/编辑状态
type UnifiedSelectionState struct {
	SelectedPath  string   // 当前选中的项目路径，"" 表示无
	HoveredPath   string   // 当前悬停的项目路径，"" 表示无
	EditingPath   string   // 当前正在编辑标题的项目路径，"" 表示无
	EditHwnd      win.HWND // 原生编辑框窗口句柄
}

// CardDragOutline 卡片拖拽虚框状态
type CardDragOutline struct {
	DragOutlineX    int
	DragOutlineY    int
	DragOutlineCard *ui.GroupCard
	DragOutlineW    int
	DragOutlineH    int

	// 拖拽幽灵窗口（WS_EX_LAYERED 显示卡片快照）
	DragGhostHwnd   win.HWND
	DragGhostDib    win.HBITMAP
	DragGhostDibBits unsafe.Pointer
	DragGhostW      int
	DragGhostH      int
	workX           int
	workY           int
}

func (s *CardDragOutline) Inject(workX, workY int) {
	s.workX = workX
	s.workY = workY
}

// ResizeOutlineState 卡片缩放虚框状态
type ResizeOutlineState struct {
	ResizeOutlineCard *ui.GroupCard
	ResizeOutlineX    int
	ResizeOutlineY    int
	ResizeOutlineW    int
	ResizeOutlineH    int
	PrevResizeX       int
	PrevResizeY       int
	PrevResizeW       int
	PrevResizeH       int

	// 弹出式边框窗口（替代 XOR 屏幕绘制）
	resizeOutline popupFrameOverlay

	resizeWorkX func() int
	resizeWorkY func() int
}

func (s *ResizeOutlineState) Inject(workX, workY int) {
	s.resizeWorkX = func() int { return workX }
	s.resizeWorkY = func() int { return workY }
}

// ContextMenuState 右键菜单状态与缓存
type ContextMenuState struct {
	IsAutoArrange      bool
	IsAlignToGrid      bool
	IsShowDesktopIcons bool
	SortBy             int

	CachedDesktopRegItems    []ui.RegistryShellItem
	CachedDesktopRegCmdStart int
	CachedFileRegItems       []ui.RegistryShellItem
	CachedFileRegCmdStart    int

	// CachedIconMenuItems 缓存的图标右键菜单项（通过 COM IContextMenu 获取）
	// 包含注册表项 + COM 扩展处理器（如 7-Zip、VS Code 等第三方软件的菜单）
	CachedIconMenuItems    []ui.RegistryShellItem
	CachedIconMenuCmdStart int

	RClickCB uintptr // 右键窗口子类化回调地址

	registryCacheTime time.Time // 注册表上次读取时间，用于缓存

	// 缓存上一次读取的结果，用于比较是否发生变化
	lastDesktopRegItems []ui.RegistryShellItem
	lastFileRegItems    []ui.RegistryShellItem
	lastIconMenuItems   []ui.RegistryShellItem

	// 异步右键菜单：在 WM_RBUTTONDOWN 中只记录信息，PostMessage 延迟弹出
	rclickBodyWidget *walk.CustomWidget // bodyWidget 句柄，用于 PostMessage
	rclickMainWindow win.HWND
	rclickManager    *group.Manager
	rclickExecutor   *ui.ProgramExecutor
	rclickGetPixelPos func(string, int) (int, int)
	rclickGetCards    func() []*ui.GroupCard
	rclickOnDesktopCmd func(cmd int)
	rclickOnNewCard   func()
	rclickScreenX    int
	rclickScreenY    int
	rclickClientX    int
	rclickClientY    int
	rclickHitItem    *group.GroupItem // 非 nil 表示点击在图标上
	rclickIsIconMenu bool             // 当前显示的是图标菜单（true）还是桌面菜单（false）
}

// DesktopMode 桌面模式 UI 管理器
type DesktopMode struct {
	MainWindow *walk.MainWindow
	Container  *walk.Composite // 无布局容器，用于绝对定位
	Manager    *group.Manager
	Executor   *ui.ProgramExecutor
	WinAPI     *desktop.WindowsAPI
	Lifecycle  *ui.LifecycleManager
	Work       *dowork.GoWork    // 定时器工作器
	Cards      []*ui.GroupCard
	BodyWidget *walk.CustomWidget

	ScreenInfo          // 屏幕/工作区尺寸
	WallpaperState      // 壁纸缓存
	UnifiedDragState    // 统一图标拖拽
	UnifiedSelectionState // 统一选中/悬停/编辑
	CardDragOutline     // 卡片拖拽虚框
	ResizeOutlineState  // 缩放虚框
	ContextMenuState    // 右键菜单状态
	RecycleBinState     // 回收站状态
	DesktopWatcherState // 桌面目录文件变更监听

	// 外部拖放状态
	ExternalDropRegistered bool // 是否已注册 DragAcceptFiles
	ExternalDropHwnd       win.HWND // 被子类化的窗口句柄

	// Healthcheck 状态缓存，避免每次轮询都执行耗时检测
	healthLastVisible bool      // 上次检测时的窗口可见性
	healthLastParent  win.HWND  // 上次检测时的父窗口句柄
}

// NewDesktopMode 创建桌面模式
func NewDesktopMode(mw *walk.MainWindow, mgr *group.Manager, winAPI *desktop.WindowsAPI, lifecycle *ui.LifecycleManager, work *dowork.GoWork) *DesktopMode {
	screenW, screenH := winAPI.GetScreenSize()
	left, top, right, bottom := winAPI.GetWorkAreaRect()
	dm := &DesktopMode{
		MainWindow: mw,
		Manager:    mgr,
		Executor:   ui.NewProgramExecutor(),
		WinAPI:     winAPI,
		Lifecycle:  lifecycle,
		Work:       work,
		ScreenInfo: ScreenInfo{
			ScreenW: screenW,
			ScreenH: screenH,
			WorkX:   left,
			WorkY:   top,
			WorkW:   right - left,
			WorkH:   bottom - top,
		},
	}
	logger.Debug("screen=%dx%d, workArea=(%d,%d,%d,%d), workSize=%dx%d",
		dm.ScreenW, dm.ScreenH, left, top, right, bottom, dm.WorkW, dm.WorkH)
	return dm
}

// Post 将 fn 投递到主 UI 线程执行，等同于 mw.Synchronize
func (dm *DesktopMode) Post(fn func()) {
	dm.MainWindow.Synchronize(fn)
}

// InvalidateBody 使 BodyWidget 无效化并触发重绘。
// 窗口不可见时跳过，避免触发 WM_PAINT 导致隐藏的窗口意外显示。
// 窗口重新显示时 showDesktopMode() 会调用 ReapplyCardPositionsAndRefresh() 完成完整刷新。
func (dm *DesktopMode) InvalidateBody() {
	if !dm.MainWindow.Visible() {
		return
	}
	dm.BodyWidget.Invalidate()
}

// HealthLastVisible 返回上次 healthcheck 检测到的窗口可见性
func (dm *DesktopMode) HealthLastVisible() bool { return dm.healthLastVisible }

// SetHealthLastVisible 设置上次 healthcheck 检测到的窗口可见性
func (dm *DesktopMode) SetHealthLastVisible(v bool) { dm.healthLastVisible = v }

// HealthLastParent 返回上次 healthcheck 检测到的父窗口句柄
func (dm *DesktopMode) HealthLastParent() win.HWND { return dm.healthLastParent }

// SetHealthLastParent 设置上次 healthcheck 检测到的父窗口句柄
func (dm *DesktopMode) SetHealthLastParent(h win.HWND) { dm.healthLastParent = h }
