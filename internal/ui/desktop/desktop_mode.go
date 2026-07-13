package desktop

import (
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/desktop"
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

// UnifiedDragState 统一图标拖拽状态
type UnifiedDragState struct {
	DragActive      bool
	DragItemPath    string
	DragItemName    string
	DragSourceGroup string // "" 表示从未分组拖出
	GhostBmp        *walk.Bitmap
	DragMouseX      int // bodyWidget 客户区坐标
	DragMouseY      int
	DragScreenX     int
	DragScreenY     int
	DropToDesktop   bool // 拖拽悬停在桌面空白区域
	LastMoveTime    time.Time
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

	outlineInvalidate func()
}

func (s *CardDragOutline) Inject(invalidate func()) { s.outlineInvalidate = invalidate }

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

	RClickCB uintptr // 右键窗口子类化回调地址

	registryCacheTime time.Time // 注册表上次读取时间，用于缓存

	// 异步右键菜单：在 WM_RBUTTONDOWN 中只记录信息，PostMessage 延迟弹出
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
}

// DesktopMode 桌面模式 UI 管理器
type DesktopMode struct {
	MainWindow *walk.MainWindow
	Container  *walk.Composite // 无布局容器，用于绝对定位
	Manager    *group.Manager
	Executor   *ui.ProgramExecutor
	WinAPI     *desktop.WindowsAPI
	Lifecycle  *ui.LifecycleManager
	Cards      []*ui.GroupCard
	BodyWidget *walk.CustomWidget

	ScreenInfo          // 屏幕/工作区尺寸
	WallpaperState      // 壁纸缓存
	UnifiedDragState    // 统一图标拖拽
	UnifiedSelectionState // 统一选中/悬停/编辑
	CardDragOutline     // 卡片拖拽虚框
	ResizeOutlineState  // 缩放虚框
	ContextMenuState    // 右键菜单状态

	dragPressed  bool      // 鼠标在桌面图标上按下（MouseDown 设，MouseUp 清）
	lastClickTime time.Time // 未分组图标上次点击时间（双击检测）
	lastClickPath string   // 未分组图标上次点击路径（双击检测）
}

// NewDesktopMode 创建桌面模式
func NewDesktopMode(mw *walk.MainWindow, mgr *group.Manager, winAPI *desktop.WindowsAPI, lifecycle *ui.LifecycleManager) *DesktopMode {
	screenW, screenH := winAPI.GetScreenSize()
	left, top, right, bottom := winAPI.GetWorkAreaRect()
	dm := &DesktopMode{
		MainWindow: mw,
		Manager:    mgr,
		Executor:   ui.NewProgramExecutor(),
		WinAPI:     winAPI,
		Lifecycle:  lifecycle,
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
