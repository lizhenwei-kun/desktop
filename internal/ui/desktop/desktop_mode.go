package desktop

import (
	"time"

	"github.com/lxn/walk"

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

// IconDragState 跨卡片图标拖拽状态
type IconDragState struct {
	IconDragActive      bool
	IconDragSourceCard  *ui.GroupCard
	IconDragItem        group.GroupItem
	IconDragSourceGroup string
	IconDragScreenX     int
	IconDragScreenY     int
	DropTargetCard      *ui.GroupCard
	DropInsertIdx       int
	DropToDesktop       bool

	GhostBmp         *walk.Bitmap
	LastDragMoveTime time.Time

	// injected capabilities
	iconBodyWidget         func() *walk.CustomWidget
	iconManager            func() *group.Manager
	iconCards              func() []*ui.GroupCard
	iconInvalidate         func()
	iconRefreshCard        func(card *ui.GroupCard)
	iconIsPointInUngrouped func(screenX, screenY int) bool
	iconMoveItemToDesktop  func(path string)
	iconMoveItemToGroup    func(path, group string)
	iconMoveItemWithinGroup func(group, path string, idx int)
}

func (s *IconDragState) Inject(inj IconInject) {
	s.iconBodyWidget = inj.BodyWidget
	s.iconManager = inj.Manager
	s.iconCards = inj.Cards
	s.iconInvalidate = inj.Invalidate
	s.iconRefreshCard = inj.RefreshCard
	s.iconIsPointInUngrouped = inj.IsPointInUngrouped
	s.iconMoveItemToDesktop = inj.MoveItemToDesktop
	s.iconMoveItemToGroup = inj.MoveItemToGroup
	s.iconMoveItemWithinGroup = inj.MoveItemWithinGroup
}

// IconInject 图标拖拽注入
type IconInject struct {
	BodyWidget           func() *walk.CustomWidget
	Manager              func() *group.Manager
	Cards                func() []*ui.GroupCard
	Invalidate           func()
	RefreshCard          func(card *ui.GroupCard)
	IsPointInUngrouped   func(screenX, screenY int) bool
	MoveItemToDesktop    func(path string)
	MoveItemToGroup      func(path, group string)
	MoveItemWithinGroup  func(group, path string, idx int)
}

// FreeItemDragState 未分组图标拖拽状态
type FreeItemDragState struct {
	FreeItemDragActive    bool
	FreeItemDragIdx       int
	FreeItemDragItem      group.GroupItem
	FreeItemDragPressed   bool
	FreeItemDragStartTime time.Time
	FreeItemDragStartX    int
	FreeItemDragStartY    int
	FreeItemDragMouseX    int
	FreeItemDragMouseY    int
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
}

// HoverState 悬停状态
type HoverState struct {
	HoveredFreeIdx int // 当前悬停的未分组图标索引
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
	IconDragState       // 跨卡片图标拖拽
	FreeItemDragState   // 未分组图标拖拽
	CardDragOutline     // 卡片拖拽虚框
	ResizeOutlineState  // 缩放虚框
	ContextMenuState    // 右键菜单状态
	HoverState          // 悬停状态
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
		HoverState: HoverState{
			HoveredFreeIdx: -1,
		},
	}
	logger.Debug("screen=%dx%d, workArea=(%d,%d,%d,%d), workSize=%dx%d",
		dm.ScreenW, dm.ScreenH, left, top, right, bottom, dm.WorkW, dm.WorkH)
	return dm
}
