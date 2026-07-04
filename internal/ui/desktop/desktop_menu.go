package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/ui"
)

const (
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
	ret, _, _ := procTrackPopupMenu.Call(uintptr(menu), flags, uintptr(x), uintptr(y), 0, uintptr(hwnd), 0)
	return ret
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

var (
	user32Menu                   = syscall.NewLazyDLL("user32.dll")
	procCreatePopupMenu          = user32Menu.NewProc("CreatePopupMenu")
	procDestroyMenu              = user32Menu.NewProc("DestroyMenu")
	procAppendMenuW              = user32Menu.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32Menu.NewProc("TrackPopupMenu")
	procCheckMenuItem            = user32Menu.NewProc("CheckMenuItem")
	procCheckMenuRadioItem       = user32Menu.NewProc("CheckMenuRadioItem")
	procGetMenuItemCount         = user32Menu.NewProc("GetMenuItemCount")

	shell32Menu                  = syscall.NewLazyDLL("shell32.dll")
	procSHParseDisplayName       = shell32Menu.NewProc("SHParseDisplayName")
	procSHBindToParent           = shell32Menu.NewProc("SHBindToParent")
	procILFree                   = shell32Menu.NewProc("ILFree")

	ole32Menu                    = syscall.NewLazyDLL("ole32.dll")
	procCoUninitialize           = ole32Menu.NewProc("CoUninitialize")
)

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
)

func (dm *DesktopMode) ShowDesktopContextMenu(hwnd win.HWND, x, y int) {
	hMenu := createPopupMenu()
	if hMenu == 0 {
		return
	}
	defer destroyMenu(hMenu)
	viewMenu := createPopupMenu()
	if viewMenu != 0 {
		appendMenu(viewMenu, MF_STRING, idViewLargeIcons, syscall.StringToUTF16Ptr("大图标"))
		appendMenu(viewMenu, MF_STRING, idViewMediumIcons, syscall.StringToUTF16Ptr("中图标"))
		appendMenu(viewMenu, MF_STRING, idViewSmallIcons, syscall.StringToUTF16Ptr("小图标"))
		curSize := ui.GetDesktopIconSize()
		switch curSize {
		case 0:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewLargeIcons)
		case 1:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewMediumIcons)
		case 2:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewSmallIcons)
		}
		appendMenuSeparator(viewMenu)
		appendMenu(viewMenu, MF_STRING, idViewAutoArrange, syscall.StringToUTF16Ptr("自动排列图标"))
		appendMenu(viewMenu, MF_STRING, idViewAlignToGrid, syscall.StringToUTF16Ptr("将图标与网格对齐"))
		appendMenuSeparator(viewMenu)
		appendMenu(viewMenu, MF_STRING, idViewShowDesktopIcons, syscall.StringToUTF16Ptr("显示桌面图标"))
		if dm.IsAutoArrange {
			checkMenuItem(viewMenu, idViewAutoArrange, MF_CHECKED)
		}
		if dm.IsAlignToGrid {
			checkMenuItem(viewMenu, idViewAlignToGrid, MF_CHECKED)
		}
		if dm.IsShowDesktopIcons {
			checkMenuItem(viewMenu, idViewShowDesktopIcons, MF_CHECKED)
		}
		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(viewMenu), syscall.StringToUTF16Ptr("查看(&V)"))
	}
	sortMenu := createPopupMenu()
	if sortMenu != 0 {
		appendMenu(sortMenu, MF_STRING, idSortByName, syscall.StringToUTF16Ptr("名称"))
		appendMenu(sortMenu, MF_STRING, idSortBySize, syscall.StringToUTF16Ptr("大小"))
		appendMenu(sortMenu, MF_STRING, idSortByType, syscall.StringToUTF16Ptr("项目类型"))
		appendMenu(sortMenu, MF_STRING, idSortByDate, syscall.StringToUTF16Ptr("修改日期"))
		switch dm.SortBy {
		case 0:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByName)
		case 1:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortBySize)
		case 2:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByType)
		case 3:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByDate)
		}
		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(sortMenu), syscall.StringToUTF16Ptr("排序方式(&O)"))
	}
	appendMenuSeparator(hMenu)
	appendMenu(hMenu, MF_STRING, idRefresh, syscall.StringToUTF16Ptr("刷新(&E)"))
	appendMenuSeparator(hMenu)
	appendMenu(hMenu, MF_STRING, idPaste, syscall.StringToUTF16Ptr("粘贴(&P)"))
	appendMenu(hMenu, MF_STRING, idPasteShortcut, syscall.StringToUTF16Ptr("粘贴快捷方式(&S)"))
	appendMenuSeparator(hMenu)
	newMenu := createPopupMenu()
	if newMenu != 0 {
		appendMenu(newMenu, MF_STRING, idNewFolder, syscall.StringToUTF16Ptr("文件夹(&F)"))
		appendMenu(newMenu, MF_STRING, idNewShortcut, syscall.StringToUTF16Ptr("快捷方式(&S)"))
		appendMenuSeparator(newMenu)
		appendMenu(newMenu, MF_STRING, idNewTextDoc, syscall.StringToUTF16Ptr("文本文档(&T)"))
		appendMenu(newMenu, MF_STRING, idNewBitmap, syscall.StringToUTF16Ptr("位图图像(&B)"))
		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(newMenu), syscall.StringToUTF16Ptr("新建(&W)"))
	}
	regItems := ui.ReadDesktopRegistryMenu()
	if len(regItems) > 0 {
		appendMenuSeparator(hMenu)
		nextID := ui.MaxCmdIDDynamic
		for _, item := range regItems {
			appendMenu(hMenu, MF_STRING, uintptr(nextID), syscall.StringToUTF16Ptr(item.Name))
			nextID++
		}
		dm.CachedDesktopRegItems = regItems
		dm.CachedDesktopRegCmdStart = ui.MaxCmdIDDynamic
	}
	appendMenuSeparator(hMenu)
	appendMenu(hMenu, MF_STRING, idDisplaySettings, syscall.StringToUTF16Ptr("显示设置(&D)"))
	appendMenu(hMenu, MF_STRING, idPersonalize, syscall.StringToUTF16Ptr("个性化(&R)"))
	itemCount := getMenuItemCount(hMenu)
	if itemCount == 0 {
		return
	}
	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return
	}
	dm.handleContextMenuCommand(int(cmd))
}

func (dm *DesktopMode) handleContextMenuCommand(cmd int) {
	if cmd >= dm.CachedDesktopRegCmdStart && cmd < dm.CachedDesktopRegCmdStart+len(dm.CachedDesktopRegItems) {
		idx := cmd - dm.CachedDesktopRegCmdStart
		if idx >= 0 && idx < len(dm.CachedDesktopRegItems) {
			ui.ExecuteRegistryCommand(dm.CachedDesktopRegItems[idx].Command, "")
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
		dm.BodyWidget.Invalidate()
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

func (dm *DesktopMode) ShowIconContextMenu(hwnd win.HWND, mgr *group.Manager, executor *ui.ProgramExecutor, item group.GroupItem, x, y int) {
	ui.ComInitThread()
	defer procCoUninitialize.Call()
	filePath := item.Path
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	var pidl uintptr
	hr, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if hr != 0 || pidl == 0 {
		return
	}
	defer procILFree.Call(pidl)
	var pShellFolder uintptr
	var pidlChild uintptr
	hr, _, _ = procSHBindToParent.Call(pidl, uintptr(unsafe.Pointer(&IID_IShellFolder)), uintptr(unsafe.Pointer(&pShellFolder)), uintptr(unsafe.Pointer(&pidlChild)))
	if hr != 0 || pShellFolder == 0 {
		return
	}
	sf := (*iShellFolder)(unsafe.Pointer(pShellFolder))
	defer syscall.SyscallN(sf.vtbl.Release, pShellFolder)
	var pContextMenu uintptr
	hr, _, _ = syscall.SyscallN(sf.vtbl.GetUIObjectOf, pShellFolder, uintptr(hwnd), 1, uintptr(unsafe.Pointer(&pidlChild)), uintptr(unsafe.Pointer(&IID_IContextMenu)), 0, uintptr(unsafe.Pointer(&pContextMenu)))
	if hr != 0 || pContextMenu == 0 {
		return
	}
	cm := (*iContextMenu)(unsafe.Pointer(pContextMenu))
	defer syscall.SyscallN(cm.vtbl.Release, pContextMenu)
	hMenu := createPopupMenu()
	if hMenu == 0 {
		return
	}
	defer destroyMenu(hMenu)
	const CMF_NORMAL = 0x00000000
	hr, _, _ = syscall.SyscallN(cm.vtbl.QueryContextMenu, pContextMenu, uintptr(hMenu), 0, 1, 0x7FFF, CMF_NORMAL)
	if hr < 0 {
		return
	}
	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON|TPM_RIGHTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return
	}
	var ici cmInvokeCommandInfo
	ici.cbSize = uint32(unsafe.Sizeof(ici))
	ici.hwnd = uintptr(hwnd)
	ici.lpVerb = uintptr(cmd - 1)
	ici.nShow = 1
	ici.lpDirectory = uintptr(unsafe.Pointer(pathPtr))
	syscall.SyscallN(cm.vtbl.InvokeCommand, pContextMenu, uintptr(unsafe.Pointer(&ici)))
}

func (dm *DesktopMode) handleIconContextMenuCommand(mgr *group.Manager, executor *ui.ProgramExecutor, item group.GroupItem, cmd int) {}

func (dm *DesktopMode) sortAndRefresh() {
	dm.BodyWidget.Invalidate()
}

func (dm *DesktopMode) autoArrangeIcons() {
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		col := i % 8
		row := i / 8
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(item.Path, relPos)
	}
}

func (dm *DesktopMode) refreshDesktop() {
	dm.loadWallpaper()
	dm.Manager.ReloadDesktopItems()
	dm.BodyWidget.Invalidate()
}
