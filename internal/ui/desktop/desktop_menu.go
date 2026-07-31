package desktop

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

const (
	rclickSubclassID  = 2
	rclickMsgID       = 0x8000 + 100 // WM_APP + 100，用于异步投递右键菜单
	rclickCmdMsgID    = 0x8000 + 101 // WM_APP + 101，后台 goroutine 返回菜单命令结果
	rclickNewCardMsg  = 0x8000 + 102 // WM_APP + 102，延迟执行新建卡片（避免 Win11 菜单残留阻塞对话框）

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
	idPersonalizeBackground = 0x6002
	idGuideLineColor       = 0x6003
)

// desktopCmdName 返回桌面菜单命令 ID 对应的可读名称（用于日志）
func desktopCmdName(cmd int) string {
	switch cmd {
	case idViewLargeIcons:
		return "查看-大图标"
	case idViewMediumIcons:
		return "查看-中图标"
	case idViewSmallIcons:
		return "查看-小图标"
	case idViewAutoArrange:
		return "查看-自动排列图标"
	case idViewAlignToGrid:
		return "查看-将图标与网格对齐"
	case idViewShowDesktopIcons:
		return "查看-显示桌面图标"
	case idSortByName:
		return "排序-名称"
	case idSortBySize:
		return "排序-大小"
	case idSortByType:
		return "排序-项目类型"
	case idSortByDate:
		return "排序-修改日期"
	case idRefresh:
		return "刷新"
	case idPaste:
		return "粘贴"
	case idPasteShortcut:
		return "粘贴快捷方式"
	case idNewFolder:
		return "新建-文件夹"
	case idNewShortcut:
		return "新建-快捷方式"
	case idNewTextDoc:
		return "新建-文本文档"
	case idNewBitmap:
		return "新建-位图图像"
	case idDisplaySettings:
		return "显示设置"
	case idPersonalize:
		return "个性化-主页"
	case idPersonalizeBackground:
		return "个性化-背景"
	case idNewCard:
		return "新建卡片"
	case idGuideLineColor:
		return "参考线颜色"
	}
	return fmt.Sprintf("未知命令(0x%x)", cmd)
}

// handleContextMenuCommand 处理桌面右键菜单命令（保留在 DesktopMode，因为涉及多处 DesktopMode 方法调用）
func (dm *DesktopMode) handleContextMenuCommand(cmd int) {
	logger.Info("菜单命令: %s (id=0x%x)", desktopCmdName(cmd), cmd)

	if cmd >= dm.CachedDesktopRegCmdStart && cmd < dm.CachedDesktopRegCmdStart+len(dm.CachedDesktopRegItems) {
		idx := cmd - dm.CachedDesktopRegCmdStart
		if idx >= 0 && idx < len(dm.CachedDesktopRegItems) {
			logger.Info("执行注册表菜单项[%d]: %q, command=%q", idx, dm.CachedDesktopRegItems[idx].Name, dm.CachedDesktopRegItems[idx].Command)
			ui.ExecuteRegistryCommand(dm.CachedDesktopRegItems[idx].Command, ui.GetDesktopPath())
		}
		return
	}
	switch cmd {
	case idViewLargeIcons:
		logger.Info("设置图标档位: 大 (48px, 11pt)")
		ui.SetDesktopIconSize(0)
		dm.Manager.SetIconSizeLevel(0)
		dm.Refresh()
	case idViewMediumIcons:
		logger.Info("设置图标档位: 中 (48px, 10pt)")
		ui.SetDesktopIconSize(1)
		dm.Manager.SetIconSizeLevel(1)
		dm.Refresh()
	case idViewSmallIcons:
		logger.Info("设置图标档位: 小 (32px, 8pt)")
		ui.SetDesktopIconSize(2)
		dm.Manager.SetIconSizeLevel(2)
		dm.Refresh()
	case idViewAutoArrange:
		// 切换自动排列：开启时立即按"从上到下、从左到右"重排
		newVal := !dm.Manager.GetAutoArrangeEnabled()
		logger.Info("切换自动排列: %v -> %v", dm.Manager.GetAutoArrangeEnabled(), newVal)
		dm.Manager.SetAutoArrangeEnabled(newVal)
		dm.IsAutoArrange = newVal
		if newVal {
			dm.autoArrangeIcons()
		}
		dm.Refresh()
	case idViewAlignToGrid:
		// 切换"将图标与网格对齐"：开启时立即将所有未分组项吸附到最近的网格
		newVal := !dm.Manager.GetAlignToGridEnabled()
		logger.Info("切换对齐网格: %v -> %v", dm.Manager.GetAlignToGridEnabled(), newVal)
		dm.Manager.SetAlignToGridEnabled(newVal)
		dm.IsAlignToGrid = newVal
		if newVal {
			dm.snapAllUngroupedToGrid()
		}
		dm.Refresh()
	case idViewShowDesktopIcons:
		dm.IsShowDesktopIcons = !dm.IsShowDesktopIcons
		logger.Info("切换显示桌面图标: %v", dm.IsShowDesktopIcons)
		dm.InvalidateBody()
	case idSortByName:
		logger.Info("排序方式: 名称")
		dm.SortBy = 0
		dm.sortAndRefresh()
	case idSortBySize:
		logger.Info("排序方式: 大小")
		dm.SortBy = 1
		dm.sortAndRefresh()
	case idSortByType:
		logger.Info("排序方式: 项目类型")
		dm.SortBy = 2
		dm.sortAndRefresh()
	case idSortByDate:
		logger.Info("排序方式: 修改日期")
		dm.SortBy = 3
		dm.sortAndRefresh()
	case idRefresh:
		logger.Info("刷新桌面")
		dm.refreshDesktop()
	case idPaste:
		logger.Info("粘贴")
		ui.PasteFromClipboard(dm.WorkX, dm.WorkY)
	case idPasteShortcut:
		logger.Info("粘贴快捷方式")
		ui.PasteShortcutFromClipboard(dm.WorkX, dm.WorkY)
	case idNewFolder:
		logger.Info("新建文件夹")
		ui.CreateNewFolder(dm.WorkX, dm.WorkY)
	case idNewShortcut:
		logger.Info("新建快捷方式")
		ui.CreateNewShortcut(dm.WorkX, dm.WorkY)
	case idNewTextDoc:
		logger.Info("新建文本文档")
		ui.CreateNewTextDocument(dm.WorkX, dm.WorkY)
	case idNewBitmap:
		logger.Info("新建位图图像")
		ui.CreateNewBitmapImage(dm.WorkX, dm.WorkY)
	case idDisplaySettings:
		logger.Info("打开显示设置 (ms-settings:display)")
		ui.OpenDisplaySettings()
	case idPersonalize:
		logger.Info("打开个性化主页 (ms-settings:personalization)")
		ui.OpenPersonalize()
	case idPersonalizeBackground:
		logger.Info("打开个性化-背景 (ms-settings:personalization-background)")
		ui.OpenPersonalizeBackground()
	case idGuideLineColor:
		dm.changeGuideLineColor()
	default:
		logger.Warn("未处理的菜单命令: id=0x%x", cmd)
	}
}

// changeGuideLineColor 修改参考线颜色（系统颜色对话框）
func (dm *DesktopMode) changeGuideLineColor() {
	logger.Info("修改参考线颜色")
	colorStr, ok := ui.ShowColorDialog(dm.MainWindow, "参考线颜色", []string{dm.Manager.GetGuideLineColor()})
	if !ok || colorStr == "" {
		return
	}
	dm.Manager.SetGuideLineColor(colorStr)
	c := ui.ParseHexColor(colorStr)
	dm.CardDragOutline.SetGuideColor(c.R, c.G, c.B)
	logger.Info("参考线颜色已改为 %s", colorStr)
}

// sortAndRefresh 排序刷新
func (dm *DesktopMode) sortAndRefresh() {
	dm.InvalidateBody()
}

// autoArrangeIcons 自动排列图标（列优先：从上到下、从左到右）
//
// 行为：
//  1. 收集所有未分组项的现有索引（去重后排序）
//  2. 按现有索引升序排列项目，保持用户的相对位置不变
//  3. 重新分配 0,1,2,... 的连续索引，使网格紧凑无空洞
//  4. 持久化每个项的新索引
//
// 由于网格布局是列优先（先填满一列再换下一列），连续的索引 0,1,2,3...
// 即为"从上到下、再从左到右"的视觉顺序。
func (dm *DesktopMode) autoArrangeIcons() {
	items := dm.Manager.GetUngroupedItems()
	if len(items) == 0 {
		return
	}

	// 收集所有当前索引（只取 >= 0 的项，-1 视为待分配）
	type entry struct {
		path    string
		curIdx  int
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		idx := dm.Manager.GetFreeItemIndex(item.Path)
		if idx < 0 {
			idx = 1 << 30 // 待分配项放最末尾
		}
		entries = append(entries, entry{path: item.Path, curIdx: idx})
	}

	// 按当前索引升序排序（保持相对位置）
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].curIdx < entries[i].curIdx {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// 重新分配 0,1,2,... 紧凑索引
	for i, e := range entries {
		dm.Manager.SetFreeItemIndex(e.path, i)
	}

	logger.Info("自动排列: 重排 %d 个未分组项（从上到下、从左到右）", len(entries))
}

// snapAllUngroupedToGrid 将所有未分组项吸附到最近的网格（当用户开启"将图标与网格对齐"时调用）
//
// 行为：遍历所有未分组项，重新计算其网格索引，确保与磁贴边界对齐。
// 由于图标位置由索引直接决定（gridToPixel），本操作实际上等价于：
//   1) 重新从 UngroupedItems 顺序取出（按当前 SortBy 排序）
//   2) 重新分配连续索引 0,1,2,...
func (dm *DesktopMode) snapAllUngroupedToGrid() {
	items := dm.Manager.GetUngroupedItems()
	if len(items) == 0 {
		return
	}

	// 使用与 autoArrangeIcons 相同的"保持现有相对位置"逻辑
	type entry struct {
		path   string
		curIdx int
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		idx := dm.Manager.GetFreeItemIndex(item.Path)
		if idx < 0 {
			idx = 1 << 30
		}
		entries = append(entries, entry{path: item.Path, curIdx: idx})
	}

	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].curIdx < entries[i].curIdx {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	for i, e := range entries {
		dm.Manager.SetFreeItemIndex(e.path, i)
	}

	logger.Info("对齐网格: 吸附 %d 个未分组项到最近网格", len(entries))
}

var refreshDesktopPending bool

// refreshDesktop 刷新桌面（整体刷新：壁纸 + 桌面项同步 + 重建卡片 + 图标缓存 + 重绘）
// 耗时操作（文件 I/O、图片解码、COM 调用）在后台 strand 中执行，避免阻塞 UI 主线程
// 连续多次调用会被合并为一次，防止竞态导致卡片状态混乱和背景跳动
func (dm *DesktopMode) refreshDesktop() {
	if refreshDesktopPending {
		logger.Debug("refreshDesktop: already pending, skipping")
		return
	}
	refreshDesktopPending = true

	// 在 UI 线程捕获当前工作区尺寸快照，避免后台 goroutine 执行时尺寸已被其他逻辑（DPI/分辨率变化）修改，
	// 导致加载的壁纸尺寸与绘制 bounds 不一致而跳动
	workW, workH := dm.WorkW, dm.WorkH
	dpiFn := dm.MainWindow.DPI

	dm.Work.Post(func() {
		defer func() { refreshDesktopPending = false }()

		// 抑制 ReloadDesktopItems 触发的 onChange 回调（会投递 dm.Refresh() 到 UI 线程），
		// 避免与下面 dm.Post(refreshCards) 重复刷新卡片导致闪烁。
		// refreshCards 内部已完成全量刷新，无需额外的 dm.Refresh()。
		dm.Manager.SuppressNotify()

		// 重新同步桌面项（以系统桌面目录为准）
		dm.Manager.ReloadDesktopItems()

		dm.Manager.UnsuppressNotify()

		// 预加载所有图标缓存（分组+未分组）
		ui.GlobalIconBmpCache.LoadAllFromManager(dm.Manager)

		// 重新加载壁纸（按工作区尺寸快照）
		dm.WallpaperState.LoadWallpaper(dpiFn, workW, workH)

		// 重建卡片必须在 UI 主线程执行（操作 walk 控件树）
		dm.Post(func() {
			dm.refreshCards()
		})
	})
}
