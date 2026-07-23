package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

const (
	rclickSubclassID = 2
	rclickMsgID      = 0x8000 + 100 // WM_APP + 100，用于异步投递右键菜单
	rclickCmdMsgID   = 0x8000 + 101 // WM_APP + 101，后台 goroutine 返回菜单命令结果

	MF_STRING       = 0x00000000
	MF_POPUP        = 0x00000010
	MF_SEPARATOR    = 0x00000800
	MF_CHECKED      = 0x00000008
	MF_UNCHECKED    = 0x00000000
	MF_BYCOMMAND    = 0x00000000
	MF_BYPOSITION   = 0x00000400
	TPM_RETURNCMD   = 0x00000100
	TPM_LEFTALIGN   = 0x00000000
	TPM_LEFTBUTTON  = 0x00000000
	TPM_RIGHTBUTTON = 0x00000002
)

type hMenu uintptr

func createPopupMenu() hMenu {
	ret, _, _ := procCreatePopupMenu.Call()
	return hMenu(ret)
}

func destroyMenu(menu hMenu) {
	procDestroyMenu.Call(uintptr(menu))
}

func appendMenu(menu hMenu, flags uintptr, idOrPopup uintptr, text *uint16) bool {
	ret, _, _ := procAppendMenuW.Call(uintptr(menu), flags, idOrPopup, uintptr(unsafe.Pointer(text)))
	return ret != 0
}

func appendMenuSeparator(menu hMenu) {
	procAppendMenuW.Call(uintptr(menu), MF_SEPARATOR, 0, 0)
}

func trackPopupMenu(menu hMenu, flags uintptr, x, y int, hwnd win.HWND) uintptr {
	// 使用 TPM_RETURNCMD 让 TrackPopupMenu 返回命令 ID
	ret, _, _ := procTrackPopupMenu.Call(uintptr(menu), flags, uintptr(x), uintptr(y), 0, uintptr(hwnd), 0)
	return ret
}

func trackPopupMenuNoReturn(menu hMenu, flags uintptr, x, y int, hwnd win.HWND) {
	// 不带 TPM_RETURNCMD，通过 WM_COMMAND 消息获取结果
	procTrackPopupMenu.Call(uintptr(menu), uintptr(flags&^TPM_RETURNCMD), uintptr(x), uintptr(y), 0, uintptr(hwnd), 0)
}

func checkMenuItem(menu hMenu, id uintptr, flags uintptr) {
	procCheckMenuItem.Call(uintptr(menu), id, flags)
}

func checkMenuRadioItem(menu hMenu, first, last, selected uintptr) {
	procCheckMenuRadioItem.Call(uintptr(menu), first, last, selected, MF_BYCOMMAND)
}

func getMenuItemCount(menu hMenu) int {
	ret, _, _ := procGetMenuItemCount.Call(uintptr(menu))
	return int(ret)
}

func insertMenu(menu hMenu, position uint32, flags uintptr, idOrPopup uintptr, text *uint16) bool {
	ret, _, _ := procInsertMenuW.Call(uintptr(menu), uintptr(position), flags, idOrPopup, uintptr(unsafe.Pointer(text)))
	return ret != 0
}

var (
	// user32 菜单
	user32Menu             = syscall.NewLazyDLL("user32.dll")
	procCreatePopupMenu    = user32Menu.NewProc("CreatePopupMenu")
	procDestroyMenu        = user32Menu.NewProc("DestroyMenu")
	procAppendMenuW        = user32Menu.NewProc("AppendMenuW")
	procTrackPopupMenu     = user32Menu.NewProc("TrackPopupMenu")
	procCheckMenuItem      = user32Menu.NewProc("CheckMenuItem")
	procCheckMenuRadioItem = user32Menu.NewProc("CheckMenuRadioItem")
	procGetMenuItemCount   = user32Menu.NewProc("GetMenuItemCount")
	procInsertMenuW        = user32Menu.NewProc("InsertMenuW")

	// shell32 COM
	shell32Menu                   = syscall.NewLazyDLL("shell32.dll")
	procSHParseDisplayName        = shell32Menu.NewProc("SHParseDisplayName")
	procSHBindToParent            = shell32Menu.NewProc("SHBindToParent")
	procILFree                    = shell32Menu.NewProc("ILFree")
	procSHGetDesktopFolder        = shell32Menu.NewProc("SHGetDesktopFolder")
	procSHGetKnownFolderIDList    = shell32Menu.NewProc("SHGetKnownFolderIDList")

	// ole32
	ole32Menu          = syscall.NewLazyDLL("ole32.dll")
	procCoUninitialize = ole32Menu.NewProc("CoUninitialize")

	// comctl32 子类化
	comctl32DLL              = syscall.NewLazyDLL("comctl32.dll")
	procSetWindowSubclass    = comctl32DLL.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32DLL.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32DLL.NewProc("DefSubclassProc")
)

// COM 接口类型
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type cmInvokeCommandInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       uintptr
	lpParameters uintptr
	lpDirectory  uintptr
	nShow        int32
	dwHotKey     uint32
	hIcon        uintptr
}

type iContextMenuVtbl struct {
	QueryInterface   uintptr
	AddRef           uintptr
	Release          uintptr
	QueryContextMenu uintptr
	InvokeCommand    uintptr
	GetCommandString uintptr
}

type iContextMenu struct {
	vtbl *iContextMenuVtbl
}

type iShellFolderVtbl struct {
	QueryInterface   uintptr
	AddRef           uintptr
	Release          uintptr
	ParseDisplayName uintptr
	EnumObjects      uintptr
	BindToObject     uintptr
	BindToStorage    uintptr
	CompareIDs       uintptr
	CreateViewObject uintptr
	GetAttributesOf  uintptr
	GetUIObjectOf    uintptr
	GetDisplayNameOf uintptr
	SetNameOf        uintptr
}

type iShellFolder struct {
	vtbl *iShellFolderVtbl
}

var (
	IID_IShellFolder = comGUID{0x000214E6, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	IID_IContextMenu = comGUID{0x000214E4, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// 菜单命令 ID
const (
	idViewLargeIcons       = 0x1001
	idViewMediumIcons      = 0x1002
	idViewSmallIcons       = 0x1003
	idViewAutoArrange      = 0x1004
	idViewAlignToGrid      = 0x1005
	idViewShowDesktopIcons = 0x1006
	idSortByName           = 0x1011
	idSortBySize           = 0x1012
	idSortByType           = 0x1013
	idSortByDate           = 0x1014
	idRefresh              = 0x1021
	idPaste                = 0x1031
	idPasteShortcut        = 0x1032
	idNewFolder            = 0x1041
	idNewShortcut          = 0x1042
	idNewTextDoc           = 0x1043
	idNewBitmap            = 0x1044
	idDisplaySettings      = 0x1051
	idPersonalize          = 0x1052
	idNewCard              = 0x6001
)

// handleContextMenuCommand 处理桌面右键菜单命令（保留在 DesktopMode，因为涉及多处 DesktopMode 方法调用）
func (dm *DesktopMode) handleContextMenuCommand(cmd int) {
	if cmd >= dm.CachedDesktopRegCmdStart && cmd < dm.CachedDesktopRegCmdStart+len(dm.CachedDesktopRegItems) {
		idx := cmd - dm.CachedDesktopRegCmdStart
		if idx >= 0 && idx < len(dm.CachedDesktopRegItems) {
			ui.ExecuteRegistryCommand(dm.CachedDesktopRegItems[idx].Command, ui.GetDesktopPath())
		}
		return
	}
	switch cmd {
	case idViewLargeIcons:
		ui.SetDesktopIconSize(0)
		dm.Refresh()
	case idViewMediumIcons:
		ui.SetDesktopIconSize(1)
		dm.Refresh()
	case idViewSmallIcons:
		ui.SetDesktopIconSize(2)
		dm.Refresh()
	case idViewAutoArrange:
		dm.IsAutoArrange = !dm.IsAutoArrange
		if dm.IsAutoArrange {
			dm.autoArrangeIcons()
		}
		dm.Refresh()
	case idViewAlignToGrid:
		dm.IsAlignToGrid = !dm.IsAlignToGrid
		dm.Refresh()
	case idViewShowDesktopIcons:
		dm.IsShowDesktopIcons = !dm.IsShowDesktopIcons
		dm.InvalidateBody()
	case idSortByName:
		dm.SortBy = 0
		dm.sortAndRefresh()
	case idSortBySize:
		dm.SortBy = 1
		dm.sortAndRefresh()
	case idSortByType:
		dm.SortBy = 2
		dm.sortAndRefresh()
	case idSortByDate:
		dm.SortBy = 3
		dm.sortAndRefresh()
	case idRefresh:
		dm.refreshDesktop()
	case idPaste:
		ui.PasteFromClipboard(dm.WorkX, dm.WorkY)
	case idPasteShortcut:
		ui.PasteShortcutFromClipboard(dm.WorkX, dm.WorkY)
	case idNewFolder:
		ui.CreateNewFolder(dm.WorkX, dm.WorkY)
	case idNewShortcut:
		ui.CreateNewShortcut(dm.WorkX, dm.WorkY)
	case idNewTextDoc:
		ui.CreateNewTextDocument(dm.WorkX, dm.WorkY)
	case idNewBitmap:
		ui.CreateNewBitmapImage(dm.WorkX, dm.WorkY)
	case idDisplaySettings:
		ui.OpenDisplaySettings()
	case idPersonalize:
		ui.OpenPersonalize()
	}
}

// sortAndRefresh 排序刷新
func (dm *DesktopMode) sortAndRefresh() {
	dm.InvalidateBody()
}

// autoArrangeIcons 自动排列图标（列优先：从上到下、从左到右）
func (dm *DesktopMode) autoArrangeIcons() {
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		// 直接按列表顺序分配索引 0,1,2,...（列优先布局）
		dm.Manager.SetFreeItemIndex(item.Path, i)
	}
}

// refreshDesktop 刷新桌面（整体刷新：壁纸 + 桌面项同步 + 重建卡片 + 图标缓存 + 重绘）
// 耗时操作（文件 I/O、图片解码、COM 调用）在后台 strand 中执行，避免阻塞 UI 主线程
func (dm *DesktopMode) refreshDesktop() {
	dm.Work.Post(func() {
		// 重新同步桌面项（以系统桌面目录为准）
		dm.Manager.ReloadDesktopItems()

		// 预加载未分组图标缓存
		freePaths := make([]string, 0)
		for _, item := range dm.Manager.GetUngroupedItems() {
			freePaths = append(freePaths, item.Path)
		}
		if len(freePaths) > 0 {
			ui.GlobalIconBmpCache.LoadAll(freePaths)
		}

		// 重新加载壁纸
		dm.WallpaperState.LoadWallpaper(dm.MainWindow.DPI, dm.WorkW, dm.WorkH)

		// 重建卡片必须在 UI 主线程执行（操作 walk 控件树）
		dm.Post(func() {
			dm.refreshCards()
		})
	})
}
