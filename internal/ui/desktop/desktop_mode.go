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

	GhostBmp         *walk.Bitmap // 拖拽 ghost 缓存（避免每次重绘重新提取图标）
	LastDragMoveTime time.Time    // 拖拽重绘节流（避免每秒几十次完整重绘）
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
}

// ResizeOutlineState 卡片缩放虚框状态（DC 绘制在屏幕上）
type ResizeOutlineState struct {
	ResizeOutlineCard *ui.GroupCard
	ResizeOutlineX    int
	ResizeOutlineY    int
	ResizeOutlineW    int
	ResizeOutlineH    int
	PrevResizeX       int // 上一帧位置
	PrevResizeY       int
	PrevResizeW       int
	PrevResizeH       int
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

	RClickCB uintptr // 右键窗口子类化回调地址（用于卸载）
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
