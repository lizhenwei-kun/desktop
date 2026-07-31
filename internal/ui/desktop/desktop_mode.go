package desktop

import (
	"sync"
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

type ScreenInfo struct {
	ScreenW int
	ScreenH int
	WorkX   int
	WorkY   int
	WorkW   int
	WorkH   int
}

type WallpaperState struct {
	mu          sync.Mutex
	WallpaperBmp *walk.Bitmap
}

type WallpaperInject struct {
	DPI        func() int
	WorkWidth  func() int
	WorkHeight func() int
}

func (s *WallpaperState) Inject(WallpaperInject) {}

type UnifiedDragState struct {
	DragActive      bool
	DragItemPath    string
	DragItemName    string
	DragSourceGroup string
	GhostBmp        *walk.Bitmap
	DragMouseX      int
	DragMouseY      int
	DragScreenX     int
	DragScreenY     int
	DropToDesktop   bool
	LastMoveTime    time.Time

	DragPressed     bool
	SourceCard      *ui.GroupCard
	SourceItemIdx   int
	SourceItem      group.GroupItem

	LastClickTime time.Time
	LastClickPath string

	GhostHwnd win.HWND
}

type UnifiedSelectionState struct {
	SelectedPath  string
	HoveredPath   string
	EditingPath   string
	EditHwnd      win.HWND
}

type CardDragOutline struct {
	DragOutlineX    int
	DragOutlineY    int
	DragOutlineCard *ui.GroupCard
	DragOutlineW    int
	DragOutlineH    int

	DragGhostHwnd   win.HWND
	DragGhostDib    win.HBITMAP
	DragGhostDibBits unsafe.Pointer
	DragGhostW      int
	DragGhostH      int
	workX           int
	workY           int
	workW           int
	workH           int

	guide          guideLineWindow // 拖动参考线（左上角对齐）
	guideLastCheck int64
	guideLastX     int
	guideLastY     int
}

func (s *CardDragOutline) Inject(workX, workY int) {
	s.workX = workX
	s.workY = workY
	s.guide.setColor(0xFF, 0x00, 0x00) // 默认红色
}

// SetWorkArea 设置工作区尺寸（参考线窗口覆盖整个工作区）
func (s *CardDragOutline) SetWorkArea(w, h int) {
	s.workW = w
	s.workH = h
	s.guide.setArea(s.workX, s.workY, w, h)
}

// SetGuideColor 设置参考线颜色（r,g,b 各 0-255）
func (s *CardDragOutline) SetGuideColor(r, g, b byte) {
	s.guide.setColor(r, g, b)
}

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

	resizeOutline popupFrameOverlay

	guide          guideLineWindow // 缩放参考线（右下角对齐）
	guideLastCheck int64

	resizeWorkX func() int
	resizeWorkY func() int
}

func (s *ResizeOutlineState) Inject(workX, workY int) {
	s.resizeWorkX = func() int { return workX }
	s.resizeWorkY = func() int { return workY }
}

// SetWorkArea 设置缩放参考线工作区尺寸
func (s *ResizeOutlineState) SetWorkArea(x, y, w, h int) {
	s.guide.setArea(x, y, w, h)
}

// SetGuideColor 设置缩放参考线颜色
func (s *ResizeOutlineState) SetGuideColor(r, g, b byte) {
	s.guide.setColor(r, g, b)
}

type ContextMenuState struct {
	IsAutoArrange      bool
	IsAlignToGrid      bool
	IsShowDesktopIcons bool
	SortBy             int

	CachedDesktopRegItems    []ui.RegistryShellItem
	CachedDesktopRegCmdStart int
	CachedFileRegItems       []ui.RegistryShellItem
	CachedFileRegCmdStart    int

	CachedIconMenuItems    []ui.RegistryShellItem
	CachedIconMenuCmdStart int

	RClickCB uintptr

	registryCacheTime time.Time

	lastDesktopRegItems []ui.RegistryShellItem
	lastFileRegItems    []ui.RegistryShellItem
	lastIconMenuItems   []ui.RegistryShellItem

	rclickBodyWidget *walk.CustomWidget
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
	rclickHitItem    *group.GroupItem
	rclickIsIconMenu bool
}

type DesktopMode struct {
	MainWindow *walk.MainWindow
	Container  *walk.Composite
	Manager    *group.Manager
	Executor   *ui.ProgramExecutor
	WinAPI     *desktop.WindowsAPI
	Lifecycle  *ui.LifecycleManager
	Work       *dowork.GoWork
	Cards      []*ui.GroupCard
	BodyWidget *walk.CustomWidget

	// redrawQueue 待重绘的卡片列表：收缩等动作先收集需要重绘的卡片，结束后统一重绘
	redrawQueue []*ui.GroupCard

	ScreenInfo
	WallpaperState
	UnifiedDragState
	UnifiedSelectionState
	CardDragOutline
	ResizeOutlineState
	ContextMenuState
	RecycleBinState
	DesktopWatcherState

	ExternalDropRegistered bool
	ExternalDropHwnd       win.HWND

	healthLastVisible bool
	healthLastParent  win.HWND

	ghostDibBmp win.HBITMAP
	ghostDibW   int
	ghostDibH   int
	ghostDibBits unsafe.Pointer

	bgBrush     walk.Brush
	btnBrush    walk.Brush
	toolbarFont *walk.Font

	paintDirty bool
}

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

func (dm *DesktopMode) Post(fn func()) {
	dm.MainWindow.Synchronize(fn)
}

func (dm *DesktopMode) SetPaintDirty() {
	dm.paintDirty = true
}

var invalidateCount int

func (dm *DesktopMode) InvalidateBody() {
	if !dm.MainWindow.Visible() {
		return
	}
	dm.paintDirty = true
	invalidateCount++
	if invalidateCount <= 5 {
		logger.Debug("DesktopMode.InvalidateBody #%d: calling BodyWidget.Invalidate", invalidateCount)
	}
	dm.BodyWidget.Invalidate()
}

func (dm *DesktopMode) HealthLastVisible() bool { return dm.healthLastVisible }

func (dm *DesktopMode) SetHealthLastVisible(v bool) { dm.healthLastVisible = v }

func (dm *DesktopMode) HealthLastParent() win.HWND { return dm.healthLastParent }

func (dm *DesktopMode) SetHealthLastParent(h win.HWND) { dm.healthLastParent = h }
