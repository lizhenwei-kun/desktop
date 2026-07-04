package desktop

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/ui"
)

// ShowDesktopContextMenu 显示桌面右键菜单，返回命令 ID 供调用者执行具体操作
func (s *ContextMenuState) ShowDesktopContextMenu(hwnd win.HWND, x, y int) uintptr {
	hMenu := createPopupMenu()
	if hMenu == 0 {
		return 0
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
		if s.IsAutoArrange {
			checkMenuItem(viewMenu, idViewAutoArrange, MF_CHECKED)
		}
		if s.IsAlignToGrid {
			checkMenuItem(viewMenu, idViewAlignToGrid, MF_CHECKED)
		}
		if s.IsShowDesktopIcons {
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
		switch s.SortBy {
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
	// 使用缓存的注册表菜单项，10 秒内不重新读取（避免每次右键都读注册表）
	regItems := s.CachedDesktopRegItems
	if s.registryCacheTime.IsZero() || time.Since(s.registryCacheTime) > 10*time.Second {
		regItems = ui.ReadDesktopRegistryMenu()
		s.registryCacheTime = time.Now()
		s.CachedDesktopRegItems = regItems
		s.CachedDesktopRegCmdStart = ui.MaxCmdIDDynamic
	}
	if len(regItems) > 0 {
		appendMenuSeparator(hMenu)
		nextID := ui.MaxCmdIDDynamic
		for _, item := range regItems {
			appendMenu(hMenu, MF_STRING, uintptr(nextID), syscall.StringToUTF16Ptr(item.Name))
			nextID++
		}
	}
	appendMenuSeparator(hMenu)
	appendMenu(hMenu, MF_STRING, idDisplaySettings, syscall.StringToUTF16Ptr("显示设置(&D)"))
	appendMenu(hMenu, MF_STRING, idPersonalize, syscall.StringToUTF16Ptr("个性化(&R)"))
	itemCount := getMenuItemCount(hMenu)
	if itemCount == 0 {
		return 0
	}
	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return 0
	}
	// 处理菜单命令（更新状态），返回命令 ID 供 DesktopMode 执行刷新操作
	s.handleDesktopCmd(int(cmd))
	return cmd
}

// handleDesktopCmd 处理桌面菜单命令的状态更新
func (s *ContextMenuState) handleDesktopCmd(cmd int) {
	if cmd >= s.CachedDesktopRegCmdStart && cmd < s.CachedDesktopRegCmdStart+len(s.CachedDesktopRegItems) {
		idx := cmd - s.CachedDesktopRegCmdStart
		if idx >= 0 && idx < len(s.CachedDesktopRegItems) {
			ui.ExecuteRegistryCommand(s.CachedDesktopRegItems[idx].Command, ui.GetDesktopPath())
		}
		return
	}
	switch cmd {
	case idViewAutoArrange:
		s.IsAutoArrange = !s.IsAutoArrange
	case idViewAlignToGrid:
		s.IsAlignToGrid = !s.IsAlignToGrid
	case idViewShowDesktopIcons:
		s.IsShowDesktopIcons = !s.IsShowDesktopIcons
	case idSortByName:
		s.SortBy = 0
	case idSortBySize:
		s.SortBy = 1
	case idSortByType:
		s.SortBy = 2
	case idSortByDate:
		s.SortBy = 3
	}
}

// ShowIconContextMenu 显示文件图标右键菜单（Shell 扩展菜单）
func (s *ContextMenuState) ShowIconContextMenu(hwnd win.HWND, mgr *group.Manager, executor *ui.ProgramExecutor, item group.GroupItem, x, y int) {
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
	ici.lpDirectory = 0 // NULL，让 Shell 扩展自行处理目录
	syscall.SyscallN(cm.vtbl.InvokeCommand, pContextMenu, uintptr(unsafe.Pointer(&ici)))
}

// InstallRightClickHandler 安装右键菜单子类化
func (s *ContextMenuState) InstallRightClickHandler(bodyWidget *walk.CustomWidget, mainWindow win.HWND, manager *group.Manager, executor *ui.ProgramExecutor, getPixelPos func(string, int) (int, int), onDesktopCmd func(cmd int)) {
	hwnd := bodyWidget.Handle()
	if hwnd == 0 {
		return
	}

	// 保存依赖，供异步消息处理器使用
	s.rclickMainWindow = mainWindow
	s.rclickManager = manager
	s.rclickExecutor = executor
	s.rclickGetPixelPos = getPixelPos
	s.rclickOnDesktopCmd = onDesktopCmd

	s.RClickCB = syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
		if msg == win.WM_RBUTTONDOWN {
			x := int(win.GET_X_LPARAM(lParam))
			y := int(win.GET_Y_LPARAM(lParam))
			var pt win.POINT
			pt.X = int32(x)
			pt.Y = int32(y)
			win.ClientToScreen(win.HWND(hwnd), &pt)
			screenX := int(pt.X)
			screenY := int(pt.Y)

			// 快速命中检测，判断点击在图标上还是桌面空白处
			items := manager.GetUngroupedItems()
			s.rclickHitItem = nil
			for i, item := range items {
				ix, iy := getPixelPos(item.Path, i)
				if x >= ix && x <= ix+ui.TileWidth() &&
					y >= iy && y <= iy+ui.TileHeight() {
					s.rclickHitItem = &item
					break
				}
			}
			s.rclickScreenX = screenX
			s.rclickScreenY = screenY

			// PostMessage 立即返回，避免 WM_RBUTTONDOWN 阻塞导致转圈
			win.PostMessage(win.HWND(hwnd), rclickMsgID, 0, 0)
			return 0
		}

		if msg == rclickMsgID {
			s.deferredShowContextMenu()
			return 0
		}

		ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})
	procSetWindowSubclass.Call(
		uintptr(hwnd),
		s.RClickCB,
		rclickSubclassID,
		0,
	)
}

// deferredShowContextMenu 异步延迟弹出的右键菜单（在 PostMessage 的自定义消息中执行）
func (s *ContextMenuState) deferredShowContextMenu() {
	hwnd := s.rclickMainWindow
	manager := s.rclickManager
	executor := s.rclickExecutor
	onDesktopCmd := s.rclickOnDesktopCmd

	if s.rclickHitItem != nil {
		s.ShowIconContextMenu(hwnd, manager, executor, *s.rclickHitItem, s.rclickScreenX, s.rclickScreenY)
	} else {
		cmd := s.ShowDesktopContextMenu(hwnd, s.rclickScreenX, s.rclickScreenY)
		if cmd != 0 && onDesktopCmd != nil {
			onDesktopCmd(int(cmd))
		}
	}
}

// UninstallRightClickHandler 卸载右键菜单子类化
func (s *ContextMenuState) UninstallRightClickHandler(bodyWidget *walk.CustomWidget) {
	if s.RClickCB == 0 {
		return
	}
	hwnd := bodyWidget.Handle()
	if hwnd == 0 {
		return
	}
	procRemoveWindowSubclass.Call(
		uintptr(hwnd),
		s.RClickCB,
		rclickSubclassID,
	)
}
