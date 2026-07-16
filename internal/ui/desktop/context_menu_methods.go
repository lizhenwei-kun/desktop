package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// InstallRightClickHandler 安装右键菜单子类化
func (s *ContextMenuState) InstallRightClickHandler(bodyWidget *walk.CustomWidget, mainWindow win.HWND, manager *group.Manager, executor *ui.ProgramExecutor, getPixelPos func(string, int) (int, int), getCards func() []*ui.GroupCard, onDesktopCmd func(cmd int), onNewCard func()) {
	hwnd := bodyWidget.Handle()
	if hwnd == 0 {
		return
	}

	// 保存依赖，供异步消息处理器使用
	s.rclickMainWindow = mainWindow
	s.rclickManager = manager
	s.rclickExecutor = executor
	s.rclickGetPixelPos = getPixelPos
	s.rclickGetCards = getCards
	s.rclickOnDesktopCmd = onDesktopCmd
	s.rclickOnNewCard = onNewCard

	s.RClickCB = syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
		if msg == win.WM_RBUTTONDOWN {
			x := int(win.GET_X_LPARAM(lParam))
			y := int(win.GET_Y_LPARAM(lParam))
			var pt win.POINT
			pt.X = int32(x)
			pt.Y = int32(y)
			win.ClientToScreen(win.HWND(hwnd), &pt)
			s.rclickScreenX = int(pt.X)
			s.rclickScreenY = int(pt.Y)
			s.rclickClientX = x
			s.rclickClientY = y

			logger.Debug("rightClick: WM_RBUTTONDOWN at (%d,%d) screen(%d,%d), posting msg", x, y, s.rclickScreenX, s.rclickScreenY)
			win.PostMessage(win.HWND(hwnd), rclickMsgID, 0, 0)
			return 0
		}

		if msg == rclickMsgID {
			logger.Debug("rightClick: received rclickMsgID, showing context menu")
			s.deferredShowContextMenu()
			return 0
		}

		// 捕获 WM_COMMAND 处理桌面菜单命令
		if msg == win.WM_COMMAND {
			cmdID := win.LOWORD(uint32(wParam))
			if cmdID >= 0x1000 && !s.rclickIsIconMenu {
				logger.Debug("rightClick: WM_COMMAND desktop cmd=%d", cmdID)
				if cmdID == idNewCard {
					if s.rclickOnNewCard != nil {
						s.rclickOnNewCard()
					}
				} else if s.rclickOnDesktopCmd != nil {
					s.rclickOnDesktopCmd(int(cmdID))
				}
				s.handleDesktopCmd(int(cmdID))
				return 0
			}
		}

		// 捕获 rclickCmdMsgID（图标菜单后台 goroutine 回传的命令结果）
		if msg == rclickCmdMsgID && wParam != 0 {
			cmdID := int(wParam)
			logger.Debug("rightClick: icon menu cmd=%d", cmdID)
			if s.rclickOnDesktopCmd != nil {
				s.rclickOnDesktopCmd(cmdID)
			}
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

// deferredShowContextMenu 异步延迟弹出的右键菜单
// 桌面菜单：UI 主线程用缓存，通过 WM_COMMAND 获取结果
// 图标菜单：后台 goroutine 实时 COM，通过 PostMessage 回传结果
func (s *ContextMenuState) deferredShowContextMenu() {
	hwnd := s.rclickMainWindow
	manager := s.rclickManager
	executor := s.rclickExecutor

	logger.Debug("rightClick: deferredShowContextMenu, hwnd=%v, client=(%d,%d) screen=(%d,%d)",
		hwnd, s.rclickClientX, s.rclickClientY, s.rclickScreenX, s.rclickScreenY)

	// 命中检测
	allItems := manager.GetAllItems()
	s.rclickHitItem = nil
	for _, item := range allItems {
		var ix, iy int
		if item.GroupName == "" {
			ungrouped := manager.GetUngroupedItems()
			for i, ui := range ungrouped {
				if ui.Path == item.Path {
					ix, iy = s.rclickGetPixelPos(item.Path, i)
					break
				}
			}
		} else {
			for _, card := range s.rclickGetCards() {
				if card.GroupName() == item.GroupName {
					sb := card.ScreenBounds()
					ix = sb.X
					iy = sb.Y
					break
				}
			}
		}
		if s.rclickClientX >= ix && s.rclickClientX <= ix+ui.TileWidth() &&
			s.rclickClientY >= iy && s.rclickClientY <= iy+ui.TileHeight() {
			itemCopy := group.GroupItem{Path: item.Path, Name: item.Name}
			s.rclickHitItem = &itemCopy
			break
		}
	}

	if s.rclickHitItem != nil {
		// 图标菜单：UI 主线程实时 COM（与系统桌面一致，COM 内部有超时保护）
		s.rclickIsIconMenu = true
		logger.Debug("rightClick: hit item %q, showing real icon context menu", s.rclickHitItem.Name)
		showIconContextMenuReal(hwnd, executor, *s.rclickHitItem, s.rclickScreenX, s.rclickScreenY)
	} else {
		// 桌面菜单：UI 主线程，用缓存
		s.rclickIsIconMenu = false
		logger.Debug("rightClick: no hit, showing desktop context menu, cachedDesktopItems=%d", len(s.CachedDesktopRegItems))
		s.showCachedDesktopContextMenu(hwnd, s.rclickScreenX, s.rclickScreenY)
	}
}

// showIconContextMenuReal 在 UI 主线程中执行实时 COM 图标右键菜单
func showIconContextMenuReal(hwnd win.HWND, executor *ui.ProgramExecutor, item group.GroupItem, x, y int) {
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
	ici.lpDirectory = 0
	syscall.SyscallN(cm.vtbl.InvokeCommand, pContextMenu, uintptr(unsafe.Pointer(&ici)))
}

// showCachedDesktopContextMenu 使用缓存的菜单数据显示桌面右键菜单（UI 主线程）
func (s *ContextMenuState) showCachedDesktopContextMenu(hwnd win.HWND, x, y int) {
	logger.Debug("rightClick: showCachedDesktopContextMenu at (%d,%d)", x, y)

	hMenu := createPopupMenu()
	if hMenu == 0 {
		logger.Warn("rightClick: createPopupMenu failed")
		return
	}
	defer destroyMenu(hMenu)

	// 新建卡片
	appendMenu(hMenu, MF_STRING, idNewCard, syscall.StringToUTF16Ptr("新建卡片"))
	appendMenuSeparator(hMenu)

	// 查看子菜单
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

	// 缓存的注册表菜单项
	regItems := s.CachedDesktopRegItems
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
		return
	}

	logger.Debug("rightClick: desktop menu built, itemCount=%d, calling TrackPopupMenu", itemCount)
	trackPopupMenuNoReturn(hMenu, TPM_LEFTALIGN|TPM_LEFTBUTTON, x, y, hwnd)
	logger.Debug("rightClick: TrackPopupMenu done")
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
