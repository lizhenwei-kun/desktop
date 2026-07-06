package desktop

import (
	"bytes"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
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

// ShowSystemDesktopContextMenu 显示系统桌面右键菜单（含自定义项）
// 菜单结构：新建卡片 → 刷新 → 查看 → 排序方式 → 系统菜单
// 返回命令 ID（0 表示取消，其他由调用者处理）
func (s *ContextMenuState) ShowSystemDesktopContextMenu(hwnd win.HWND, x, y int) uintptr {
	ui.ComInitThread()
	defer procCoUninitialize.Call()

	// 获取桌面 IShellFolder
	var pDesktopShellFolder uintptr
	hr, _, _ := procSHGetDesktopFolder.Call(uintptr(unsafe.Pointer(&pDesktopShellFolder)))
	if hr != 0 || pDesktopShellFolder == 0 {
		return 0
	}
	sf := (*iShellFolder)(unsafe.Pointer(pDesktopShellFolder))
	defer syscall.SyscallN(sf.vtbl.Release, pDesktopShellFolder)

	var pContextMenu uintptr
	hr, _, _ = syscall.SyscallN(sf.vtbl.CreateViewObject, pDesktopShellFolder, uintptr(hwnd), uintptr(unsafe.Pointer(&IID_IContextMenu)), uintptr(unsafe.Pointer(&pContextMenu)))
	if hr != 0 || pContextMenu == 0 {
		return 0
	}
	cm := (*iContextMenu)(unsafe.Pointer(pContextMenu))
	defer syscall.SyscallN(cm.vtbl.Release, pContextMenu)

	hMenu := createPopupMenu()
	if hMenu == 0 {
		return 0
	}
	defer destroyMenu(hMenu)

	// 系统菜单项 cmd ID 从 2 开始（保留 1 给自定义项）
	const CMF_NORMAL = 0x00000000
	const CMF_EXPLORE = 0x00000004
	hr, _, _ = syscall.SyscallN(cm.vtbl.QueryContextMenu, pContextMenu, uintptr(hMenu), 0, 2, 0xFFFF, CMF_NORMAL|CMF_EXPLORE)
	if int32(hr) < 0 {
		return 0
	}

	// 构建"查看"子菜单
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
	}

	// 构建"排序方式"子菜单
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
	}

	// 在系统菜单前插入我们的自定义项（倒序插入，位置正确后为正序）
	insertMenu(hMenu, 0, MF_BYPOSITION|MF_SEPARATOR, 0, nil)                              // pos0=自定义与系统分隔线
	if sortMenu != 0 {
		insertMenu(hMenu, 0, MF_BYPOSITION|MF_POPUP, uintptr(sortMenu), syscall.StringToUTF16Ptr("排序方式(&O)"))
	}
	if viewMenu != 0 {
		insertMenu(hMenu, 0, MF_BYPOSITION|MF_POPUP, uintptr(viewMenu), syscall.StringToUTF16Ptr("查看(&V)"))
	}
	insertMenu(hMenu, 0, MF_BYPOSITION|MF_STRING, idRefresh, syscall.StringToUTF16Ptr("刷新(&E)"))
	insertMenu(hMenu, 0, MF_BYPOSITION|MF_SEPARATOR, 0, nil)                              // 新建卡片与自定义菜单分隔线
	insertMenu(hMenu, 0, MF_BYPOSITION|MF_STRING, idNewCard, syscall.StringToUTF16Ptr("新建卡片"))

	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return 0
	}

	if cmd == idNewCard {
		// 仅记录"新建卡片"意图，不在 COM 上下文内直接调用 addNewCard
		// 由调用者（deferredShowContextMenu）在 COM 反初始化后执行
		return cmd
	}

	if cmd >= 2 {
		// 系统菜单命令
		desktopPath := ui.GetDesktopPath()
		logger.Debug("ShowSystemDesktopContextMenu: invoking system cmd=%d, desktopPath=%q", cmd, desktopPath)
		if desktopPath == "" {
			logger.Error("ShowSystemDesktopContextMenu: desktopPath is empty, system cmd=%d may fail with invalid directory", cmd)
			return idRefresh
		}

		// 获取 verb 名称：先用 GCS_VERBA (0x0004)，但扩展可能返回 UTF-16LE 数据
		// 所以同时读取 byte 和 uint16 两个视角
		verbA := make([]byte, 512)
		var verbStr string
		var gotVerb bool

		hrA, _, _ := syscall.SyscallN(cm.vtbl.GetCommandString, pContextMenu, uintptr(cmd-2), 0x0004, 0, uintptr(unsafe.Pointer(&verbA[0])), uintptr(len(verbA)))
		if int32(hrA) >= 0 {
			// 尝试当 UTF-16LE 解析（扩展可能返回 Unicode 数据）
			if len(verbA) >= 2 {
				_ = verbA[1] // 边界检查
			}
			verbW := unsafe.Slice((*uint16)(unsafe.Pointer(&verbA[0])), len(verbA)/2)
			verbName := syscall.UTF16ToString(verbW)
			if verbName != "" {
				verbStr = verbName
				gotVerb = true
				logger.Debug("ShowSystemDesktopContextMenu: cmd=%d -> verbA(as UTF-16)=%q (idx=%d, hr=0x%08x)", cmd, verbStr, cmd-2, hrA)
			} else {
				// 回退：按 ANSI 解析
				n := bytes.IndexByte(verbA, 0)
				if n < 0 {
					n = len(verbA)
				}
				if n > 0 {
					verbStr = string(verbA[:n])
					gotVerb = true
					logger.Debug("ShowSystemDesktopContextMenu: cmd=%d -> verbA(as ANSI)=%q (idx=%d, hr=0x%08x)", cmd, verbStr, cmd-2, hrA)
				}
			}
		}

		if !gotVerb {
			// 尝试 GCS_VERBW (0x0044)
			verbW := make([]uint16, 256)
			hrW, _, _ := syscall.SyscallN(cm.vtbl.GetCommandString, pContextMenu, uintptr(cmd-2), 0x0044, 0, uintptr(unsafe.Pointer(&verbW[0])), uintptr(len(verbW)))
			if int32(hrW) >= 0 {
				verbStr = syscall.UTF16ToString(verbW)
				gotVerb = true
				logger.Debug("ShowSystemDesktopContextMenu: cmd=%d -> verbW=%q (idx=%d, hr=0x%08x)", cmd, verbStr, cmd-2, hrW)
			}
		}

		if gotVerb && verbStr != "" {
			// 使用 string verb + CMIC_MASK_UNICODE
			verbPtr, _ := syscall.UTF16PtrFromString(verbStr)
			const CMIC_MASK_UNICODE = 0x00004000
			var ici cmInvokeCommandInfo
			ici.cbSize = uint32(unsafe.Sizeof(ici))
			ici.fMask = CMIC_MASK_UNICODE
			ici.hwnd = uintptr(hwnd)
			ici.lpVerb = uintptr(unsafe.Pointer(verbPtr))
			ici.nShow = 1
			ici.lpDirectory = 0
			ret, _, _ := syscall.SyscallN(cm.vtbl.InvokeCommand, pContextMenu, uintptr(unsafe.Pointer(&ici)))
			logger.Debug("ShowSystemDesktopContextMenu: string verb InvokeCommand returned %d for cmd=%d, verb=%q", ret, cmd, verbStr)
		} else {
			logger.Warn("ShowSystemDesktopContextMenu: GetCommandString failed (A=0x%08x), trying integer verb", hrA)
			// 回退：整数索引方式
			const CMIC_MASK_FLAG_NO_UI = 0x00000400
			var ici cmInvokeCommandInfo
			ici.cbSize = uint32(unsafe.Sizeof(ici))
			ici.fMask = CMIC_MASK_FLAG_NO_UI
			ici.hwnd = uintptr(hwnd)
			ici.lpVerb = uintptr(cmd - 2)
			ici.nShow = 1
			ici.lpDirectory = 0
			ret, _, _ := syscall.SyscallN(cm.vtbl.InvokeCommand, pContextMenu, uintptr(unsafe.Pointer(&ici)))
			logger.Debug("ShowSystemDesktopContextMenu: integer verb InvokeCommand returned %d for cmd=%d", ret, cmd)
		}
		return idRefresh
	}

	// 自定义命令（1 已被 idNewCard 用，2+ 为系统命令，走到这里 cmd=idRefresh/view*/sort*）
	return cmd
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
func (s *ContextMenuState) InstallRightClickHandler(bodyWidget *walk.CustomWidget, mainWindow win.HWND, manager *group.Manager, executor *ui.ProgramExecutor, getPixelPos func(string, int) (int, int), onDesktopCmd func(cmd int), onNewCard func()) {
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

			// PostMessage 立即返回，不做任何其他操作，彻底避免 WM_RBUTTONDOWN 阻塞导致转圈
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

	// 在自定义消息中做命中检测（移到此处避免 WM_RBUTTONDOWN 中执行导致转圈）
	items := manager.GetUngroupedItems()
	s.rclickHitItem = nil
	for i, item := range items {
		ix, iy := s.rclickGetPixelPos(item.Path, i)
		if s.rclickClientX >= ix && s.rclickClientX <= ix+ui.TileWidth() &&
			s.rclickClientY >= iy && s.rclickClientY <= iy+ui.TileHeight() {
			s.rclickHitItem = &item
			break
		}
	}

	if s.rclickHitItem != nil {
		s.ShowIconContextMenu(hwnd, manager, executor, *s.rclickHitItem, s.rclickScreenX, s.rclickScreenY)
	} else {
		cmd := s.ShowSystemDesktopContextMenu(hwnd, s.rclickScreenX, s.rclickScreenY)
		if cmd != 0 && onDesktopCmd != nil {
			if int(cmd) == idNewCard {
				// "新建卡片"在 COM 反初始化后执行，避免影响卡片重建
				if s.rclickOnNewCard != nil {
					s.rclickOnNewCard()
				}
			} else {
				onDesktopCmd(int(cmd))
			}
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
