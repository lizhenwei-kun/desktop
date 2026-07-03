package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
)

// ============================================================
// Win32 API 声明
// ============================================================

var (
	user32Ctx                  = syscall.NewLazyDLL("user32.dll")
	shell32Ctx                 = syscall.NewLazyDLL("shell32.dll")
	advapi32Ctx                = syscall.NewLazyDLL("advapi32.dll")
	comctl32Ctx                = syscall.NewLazyDLL("comctl32.dll")

	procCreatePopupMenu        = user32Ctx.NewProc("CreatePopupMenu")
	procDestroyMenu            = user32Ctx.NewProc("DestroyMenu")
	procAppendMenuW            = user32Ctx.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32Ctx.NewProc("TrackPopupMenu")
	procCheckMenuItem          = user32Ctx.NewProc("CheckMenuItem")
	procCheckMenuRadioItem     = user32Ctx.NewProc("CheckMenuRadioItem")
	procGetMenuItemCount       = user32Ctx.NewProc("GetMenuItemCount")
	procMonitorFromPoint       = user32Ctx.NewProc("MonitorFromPoint")
	procGetMonitorInfoW        = user32Ctx.NewProc("GetMonitorInfoW")
	procOpenClipboard          = user32Ctx.NewProc("OpenClipboard")
	procCloseClipboard         = user32Ctx.NewProc("CloseClipboard")
	procGetClipboardData       = user32Ctx.NewProc("GetClipboardData")
	procRegisterClipboardFormatW = user32Ctx.NewProc("RegisterClipboardFormatW")

	procDragQueryFile          = shell32Ctx.NewProc("DragQueryFileW")
	procDragFinish             = shell32Ctx.NewProc("DragFinish")

	// 注册表 API
	procCtxRegOpenKeyExW          = advapi32Ctx.NewProc("RegOpenKeyExW")
	procCtxRegEnumKeyExW          = advapi32Ctx.NewProc("RegEnumKeyExW")
	procCtxRegQueryValueExW       = advapi32Ctx.NewProc("RegQueryValueExW")
	procCtxRegCloseKey            = advapi32Ctx.NewProc("RegCloseKey")

	// 窗口子类化 API（comctl32.dll）
	procSetWindowSubclass      = comctl32Ctx.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass   = comctl32Ctx.NewProc("RemoveWindowSubclass")
	procDefSubclassProc        = comctl32Ctx.NewProc("DefSubclassProc")
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

	CF_HDROP        = 15

	MONITOR_DEFAULTTONULL    = 0x00000000
	MONITOR_DEFAULTTOPRIMARY = 0x00000001
	MONITOR_DEFAULTTONEAREST = 0x00000002
)

// ============================================================
// Win32 辅助封装
// ============================================================

// hMenu 类型
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

// getMonitorInfoAt 获取指定坐标所在显示器的信息
type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor struct {
		left, top, right, bottom int32
	}
	rcWork struct {
		left, top, right, bottom int32
	}
	dwFlags  uint32
	szDevice [32]uint16
}

func getMonitorInfoAt(x, y int) *monitorInfoEx {
	// 使用 MonitorFromPoint 获取坐标所在显示器
	pt := uint32(uint16(x)) | (uint32(uint16(y)) << 16)
	hMonitor, _, _ := procMonitorFromPoint.Call(uintptr(pt), MONITOR_DEFAULTTONEAREST)
	if hMonitor == 0 {
		return nil
	}

	var mi monitorInfoEx
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return nil
	}
	return &mi
}

// ============================================================
// 注册表 Shell 菜单项读取
// ============================================================

const (
	regHKEY_CLASSES_ROOT = 0x80000000
	regHKEY_CURRENT_USER = 0x80000001
	KEY_READ            = 0x00020019
	KEY_ENUMERATE_SUB_KEYS = 0x0008
	REG_SZ              = 1
	REG_EXPAND_SZ       = 2
	REG_MULTI_SZ        = 7

	maxCmdIDDynamic     = 0x3000 // 动态命令ID起始值（桌面）
	maxCmdIDIconDynamic = 0x4000 // 动态命令ID起始值（图标）
	maxCmdIDSendTo      = 0x5000 // 发送到动态命令ID起始值
)

// registryShellItem 注册表shell菜单项
type registryShellItem struct {
	verb     string // 动词名（如 "open", "edit"）
	name     string // 显示名称
	command  string // 执行命令
	isDir    bool   // 是否文件夹类型路径
	cmdID    int    // 动态分配的命令ID（桌面/图标菜单共用）
}

// regKey 注册表键句柄
type regKey uintptr

// regOpenKey 打开注册表键
func regOpenKey(hKey uintptr, subKey string) regKey {
	p, _ := syscall.UTF16PtrFromString(subKey)
	var h regKey
	ret, _, _ := procCtxRegOpenKeyExW.Call(
		hKey,
		uintptr(unsafe.Pointer(p)),
		0,
		KEY_READ|KEY_ENUMERATE_SUB_KEYS,
		uintptr(unsafe.Pointer(&h)),
	)
	if ret != 0 {
		return 0
	}
	return h
}

// regCloseKey 关闭注册表键
func regCloseKey(hKey regKey) {
	procCtxRegCloseKey.Call(uintptr(hKey))
}

// regEnumSubKeys 枚举所有子键名
func regEnumSubKeys(hKey regKey) []string {
	var names []string
	buf := make([]uint16, 256)
	for i := 0; ; i++ {
		nameSize := uint32(len(buf))
		ret, _, _ := procCtxRegEnumKeyExW.Call(
			uintptr(hKey),
			uintptr(i),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&nameSize)),
			0, 0, 0, 0,
		)
		if ret != 0 {
			break
		}
		name := syscall.UTF16ToString(buf[:nameSize])
		names = append(names, name)
	}
	return names
}

// regQueryStringValue 查询字符串值
func regQueryStringValue(hKey regKey, valueName string) string {
	var vn *uint16
	if valueName != "" {
		vn, _ = syscall.UTF16PtrFromString(valueName)
	}
	buf := make([]uint16, 1024)
	bufSize := uint32(len(buf) * 2)
	ret, _, _ := procCtxRegQueryValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(vn)),
		0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// resolveMUIVerb 解析 MUIVerb（支持 @path,-resID 格式）
func resolveMUIVerb(muiVerb string) string {
	if muiVerb == "" {
		return ""
	}
	if !strings.HasPrefix(muiVerb, "@") {
		return muiVerb
	}
	// 格式: @C:\Windows\System32\shell32.dll,-21770
	muiVerb = muiVerb[1:]
	parts := strings.SplitN(muiVerb, ",", 2)
	if len(parts) != 2 {
		return muiVerb
	}
	dllPath := parts[0]
	resID := ""
	if len(parts) > 1 {
		resID = parts[1]
	}

	// 使用 LoadString 从 DLL 读取字符串
	hMod, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryW").Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(dllPath))),
	)
	if hMod == 0 {
		return muiVerb
	}
	defer syscall.NewLazyDLL("kernel32.dll").NewProc("FreeLibrary").Call(hMod)

	// LoadString(hModule, uID, lpBuffer, nBufferMax)
	var id int
	fmt.Sscanf(resID, "%d", &id)
	if id == 0 {
		return muiVerb
	}

	buf := make([]uint16, 512)
	ret, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("LoadStringW").Call(
		hMod, uintptr(uint32(id)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
	)
	if ret == 0 {
		return muiVerb
	}
	return syscall.UTF16ToString(buf)
}

// readRegistryShellItems 读取指定注册表路径下的 shell 菜单项
// 格式: HKCR\Directory\Background\shell\<verb>\command\(Default)
func readRegistryShellItems(regRoot uintptr, subKey string) []registryShellItem {
	var items []registryShellItem

	hKey := regOpenKey(regRoot, subKey)
	if hKey == 0 {
		return nil
	}
	defer regCloseKey(hKey)

	verbs := regEnumSubKeys(hKey)
	for _, verb := range verbs {
		// 跳过扩展动词（需要 Shift 键）
		if strings.HasSuffix(verb, "_extended") || verb == "extended" {
			continue
		}

		verbKey := subKey + "\\" + verb
		hVerb := regOpenKey(regRoot, verbKey)
		if hVerb == 0 {
			continue
		}

		// 读取显示名称：优先 MUIVerb，再取 (Default)
		itemName := resolveMUIVerb(regQueryStringValue(hVerb, "MUIVerb"))
		if itemName == "" {
			itemName = regQueryStringValue(hVerb, "")
		}
		if itemName == "" {
			// 回退：使用动词名
			itemName = verb
		}

		// 读取命令
		hCmd := regOpenKey(regRoot, verbKey+"\\command")
		if hCmd == 0 {
			// 可能有子菜单（级联菜单），暂不处理
			regCloseKey(hVerb)
			continue
		}
		cmdLine := regQueryStringValue(hCmd, "")
		regCloseKey(hCmd)
		regCloseKey(hVerb)

		if cmdLine == "" {
			continue
		}

		items = append(items, registryShellItem{
			verb:    verb,
			name:    itemName,
			command: cmdLine,
			isDir:   false,
		})
	}
	return items
}

// appendRegistryMenuItems 将注册表菜单项附加到弹出菜单
// 返回添加的最后一个命令ID（供后续使用）
func appendRegistryMenuItems(menu hMenu, items []registryShellItem, cmdIDStart int) int {
	if len(items) == 0 {
		return cmdIDStart
	}

	appendMenuSeparator(menu)

	nextID := cmdIDStart
	for _, item := range items {
		if item.name == "" || item.name == "-" {
			appendMenuSeparator(menu)
			continue
		}
		item.cmdID = nextID
		appendMenu(menu, MF_STRING, uintptr(nextID), syscall.StringToUTF16Ptr(item.name))
		nextID++
	}
	return nextID
}

// executeRegistryCommand 执行注册表菜单命令
func executeRegistryCommand(cmdLine string, filePath string) {
	if cmdLine == "" {
		return
	}

	// 替换占位符
	cmdLine = strings.ReplaceAll(cmdLine, "%1", filePath)
	cmdLine = strings.ReplaceAll(cmdLine, "%L", filePath)
	cmdLine = strings.ReplaceAll(cmdLine, "%V", filePath)
	// 移除其余未替换的占位符（保证不报错）
	parts := strings.Fields(cmdLine)
	if len(parts) == 0 {
		return
	}

	prog := parts[0]
	var args []string
	for _, p := range parts[1:] {
		// 移除残留的 % 占位符
		if strings.HasPrefix(p, "%") && len(p) > 1 {
			continue
		}
		args = append(args, p)
	}

	cmd := exec.Command(prog, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	if err := cmd.Start(); err != nil {
		logger.Warn("executeRegistryCommand failed: %v (prog=%s)", err, prog)
	}
}

// readDesktopRegistryMenu 读取桌面背景的注册表菜单项
func readDesktopRegistryMenu() []registryShellItem {
	// 先尝试当前用户设置，再尝试全局设置
	items := readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\Directory\Background\shell`)
	if len(items) == 0 {
		items = readRegistryShellItems(regHKEY_CLASSES_ROOT, `Directory\Background\shell`)
	}
	return items
}

// readFileRegistryMenu 读取文件类型的注册表菜单项
func readFileRegistryMenu(filePath string) []registryShellItem {
	var items []registryShellItem

	// 1. 所有文件通用 (HKCU 优先)
	items = readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\*\shell`)
	if len(items) == 0 {
		items = readRegistryShellItems(regHKEY_CLASSES_ROOT, `*\shell`)
	}

	// 2. 特定扩展名菜单
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != "" {
		// 获取扩展名关联的 ProgID
		extItems := readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\`+ext+`\shell`)
		if len(extItems) == 0 {
			extItems = readRegistryShellItems(regHKEY_CLASSES_ROOT, ext+`\shell`)
		}
		items = append(items, extItems...)
	}

	// 3. 如果是文件夹，额外读取文件夹菜单
	if info, err := os.Stat(filePath); err == nil && info.IsDir() {
		dirItems := readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\Directory\shell`)
		if len(dirItems) == 0 {
			dirItems = readRegistryShellItems(regHKEY_CLASSES_ROOT, `Directory\shell`)
		}
		items = append(items, dirItems...)
	}

	// 4. AllFilesystemObjects
	afsItems := readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\AllFilesystemObjects\shell`)
	if len(afsItems) == 0 {
		afsItems = readRegistryShellItems(regHKEY_CLASSES_ROOT, `AllFilesystemObjects\shell`)
	}
	items = append(items, afsItems...)

	return items
}

// readSendToMenu 读取"发送到"菜单项
func readSendToMenu() []registryShellItem {
	// 从 ShellNew\SendTo 或实际的 SendTo 文件夹读取
	sendToPath := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\SendTo`)
	var items []registryShellItem
	entries, err := os.ReadDir(sendToPath)
	if err != nil {
		return nil
	}
	cmdID := maxCmdIDSendTo
	for _, entry := range entries {
		name := entry.Name()
		// 跳过 desktop.ini
		if strings.EqualFold(name, "desktop.ini") {
			continue
		}
		displayName := strings.TrimSuffix(name, filepath.Ext(name))
		fullPath := filepath.Join(sendToPath, name)
		items = append(items, registryShellItem{
			verb:    name,
			name:    displayName,
			command: fullPath,
			cmdID:   cmdID,
		})
		cmdID++
	}
	return items
}

// executeSendToMenu 执行发送到命令
func executeSendToMenu(sendToPath, filePath string) {
	// 如果是快捷方式，复制文件到目标目录
	if strings.EqualFold(filepath.Ext(sendToPath), ".lnk") {
		// 解析快捷方式目标
		createShortcut(filePath, filepath.Join(filepath.Dir(sendToPath), filepath.Base(filePath)+".lnk"))
		// 实际使用 copy 命令
		copyFileOrDir(filePath, filepath.Dir(sendToPath))
	} else {
		copyFileOrDir(filePath, sendToPath)
	}
}

// ============================================================
// 桌面右键菜单 —— 与 Windows 系统桌面右键菜单功能列表一致
// ============================================================

// ShowDesktopContextMenu 在指定屏幕坐标显示桌面右键菜单
// hwnd: 菜单消息的父窗口句柄；x, y: 屏幕坐标
func (dm *DesktopMode) ShowDesktopContextMenu(hwnd win.HWND, x, y int) {
	// 创建弹出菜单
	hMenu := createPopupMenu()
	if hMenu == 0 {
		return
	}
	defer destroyMenu(hMenu)

	// ===== 第1区：查看 =====
	viewMenu := createPopupMenu()
	if viewMenu != 0 {
		appendMenu(viewMenu, MF_STRING, idViewLargeIcons, syscall.StringToUTF16Ptr("大图标"))
		appendMenu(viewMenu, MF_STRING, idViewMediumIcons, syscall.StringToUTF16Ptr("中图标"))
		appendMenu(viewMenu, MF_STRING, idViewSmallIcons, syscall.StringToUTF16Ptr("小图标"))

		// 当前图标大小对应的勾选
		curSize := getDesktopIconSize()
		switch curSize {
		case iconSizeLarge:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewLargeIcons)
		case iconSizeMedium:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewMediumIcons)
		case iconSizeSmall:
			checkMenuRadioItem(viewMenu, idViewLargeIcons, idViewSmallIcons, idViewSmallIcons)
		}

		appendMenuSeparator(viewMenu)
		appendMenu(viewMenu, MF_STRING, idViewAutoArrange, syscall.StringToUTF16Ptr("自动排列图标"))
		appendMenu(viewMenu, MF_STRING, idViewAlignToGrid, syscall.StringToUTF16Ptr("将图标与网格对齐"))
		appendMenuSeparator(viewMenu)
		appendMenu(viewMenu, MF_STRING, idViewShowDesktopIcons, syscall.StringToUTF16Ptr("显示桌面图标"))

		// 勾选当前状态
		if dm.isAutoArrange {
			checkMenuItem(viewMenu, idViewAutoArrange, MF_CHECKED)
		}
		if dm.isAlignToGrid {
			checkMenuItem(viewMenu, idViewAlignToGrid, MF_CHECKED)
		}
		if dm.isShowDesktopIcons {
			checkMenuItem(viewMenu, idViewShowDesktopIcons, MF_CHECKED)
		}

		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(viewMenu), syscall.StringToUTF16Ptr("查看(&V)"))
	}

	// ===== 排序方式 =====
	sortMenu := createPopupMenu()
	if sortMenu != 0 {
		appendMenu(sortMenu, MF_STRING, idSortByName, syscall.StringToUTF16Ptr("名称"))
		appendMenu(sortMenu, MF_STRING, idSortBySize, syscall.StringToUTF16Ptr("大小"))
		appendMenu(sortMenu, MF_STRING, idSortByType, syscall.StringToUTF16Ptr("项目类型"))
		appendMenu(sortMenu, MF_STRING, idSortByDate, syscall.StringToUTF16Ptr("修改日期"))

		// 当前排序方式对应的勾选
		switch dm.sortBy {
		case sortByName:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByName)
		case sortBySize:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortBySize)
		case sortByType:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByType)
		case sortByDate:
			checkMenuRadioItem(sortMenu, idSortByName, idSortByDate, idSortByDate)
		}

		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(sortMenu), syscall.StringToUTF16Ptr("排序方式(&O)"))
	}

	// ===== 分隔线 =====
	appendMenuSeparator(hMenu)

	// ===== 刷新 =====
	appendMenu(hMenu, MF_STRING, idRefresh, syscall.StringToUTF16Ptr("刷新(&E)"))

	// ===== 分隔线 =====
	appendMenuSeparator(hMenu)

	// ===== 粘贴 =====
	appendMenu(hMenu, MF_STRING, idPaste, syscall.StringToUTF16Ptr("粘贴(&P)"))

	// ===== 粘贴快捷方式 =====
	appendMenu(hMenu, MF_STRING, idPasteShortcut, syscall.StringToUTF16Ptr("粘贴快捷方式(&S)"))

	// ===== 分隔线 =====
	appendMenuSeparator(hMenu)

	// ===== 新建 =====
	newMenu := createPopupMenu()
	if newMenu != 0 {
		appendMenu(newMenu, MF_STRING, idNewFolder, syscall.StringToUTF16Ptr("文件夹(&F)"))
		appendMenu(newMenu, MF_STRING, idNewShortcut, syscall.StringToUTF16Ptr("快捷方式(&S)"))
		appendMenuSeparator(newMenu)
		appendMenu(newMenu, MF_STRING, idNewTextDoc, syscall.StringToUTF16Ptr("文本文档(&T)"))
		appendMenu(newMenu, MF_STRING, idNewBitmap, syscall.StringToUTF16Ptr("位图图像(&B)"))

		appendMenu(hMenu, MF_POPUP|MF_STRING, uintptr(newMenu), syscall.StringToUTF16Ptr("新建(&W)"))
	}

	// ===== 注册表扩展菜单项（第三方软件注册） =====
	regItems := readDesktopRegistryMenu()
	if len(regItems) > 0 {
		appendMenuSeparator(hMenu)
		nextID := maxCmdIDDynamic
		for _, item := range regItems {
			item.cmdID = nextID
			appendMenu(hMenu, MF_STRING, uintptr(nextID), syscall.StringToUTF16Ptr(item.name))
			nextID++
		}
		// 保存到 DesktopMode 中供命令处理
		dm.cachedDesktopRegItems = regItems
		dm.cachedDesktopRegCmdStart = maxCmdIDDynamic
	}

	// ===== 分隔线 =====
	appendMenuSeparator(hMenu)

	// ===== 显示设置 =====
	appendMenu(hMenu, MF_STRING, idDisplaySettings, syscall.StringToUTF16Ptr("显示设置(&D)"))

	// ===== 个性化 =====
	appendMenu(hMenu, MF_STRING, idPersonalize, syscall.StringToUTF16Ptr("个性化(&R)"))

	// ===== 显示菜单 =====
	itemCount := getMenuItemCount(hMenu)
	if itemCount == 0 {
		return
	}

	// 调整菜单位置确保不超出屏幕边界
	mi := getMonitorInfoAt(x, y)
	if mi != nil {
		if x > int(mi.rcWork.right)-350 {
			x = int(mi.rcWork.right) - 350
		}
		if y > int(mi.rcWork.bottom)-200 {
			y = int(mi.rcWork.bottom) - 200
		}
	}

	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return
	}

	// 处理菜单命令
	dm.handleContextMenuCommand(int(cmd))
}

// handleContextMenuCommand 处理右键菜单命令
func (dm *DesktopMode) handleContextMenuCommand(cmd int) {
	// 检查是否为注册表动态命令（桌面菜单）
	if cmd >= dm.cachedDesktopRegCmdStart && cmd < dm.cachedDesktopRegCmdStart+len(dm.cachedDesktopRegItems) {
		idx := cmd - dm.cachedDesktopRegCmdStart
		if idx >= 0 && idx < len(dm.cachedDesktopRegItems) {
			executeRegistryCommand(dm.cachedDesktopRegItems[idx].command, "")
		}
		return
	}

	switch cmd {
	// ===== 查看 =====
	case idViewLargeIcons:
		setDesktopIconSize(iconSizeLarge)
		dm.Refresh()
	case idViewMediumIcons:
		setDesktopIconSize(iconSizeMedium)
		dm.Refresh()
	case idViewSmallIcons:
		setDesktopIconSize(iconSizeSmall)
		dm.Refresh()
	case idViewAutoArrange:
		dm.isAutoArrange = !dm.isAutoArrange
		if dm.isAutoArrange {
			dm.autoArrangeIcons()
		}
		dm.Refresh()
	case idViewAlignToGrid:
		dm.isAlignToGrid = !dm.isAlignToGrid
		dm.Refresh()
	case idViewShowDesktopIcons:
		dm.isShowDesktopIcons = !dm.isShowDesktopIcons
		dm.bodyWidget.Invalidate()

	// ===== 排序方式 =====
	case idSortByName:
		dm.sortBy = sortByName
		dm.sortAndRefresh()
	case idSortBySize:
		dm.sortBy = sortBySize
		dm.sortAndRefresh()
	case idSortByType:
		dm.sortBy = sortByType
		dm.sortAndRefresh()
	case idSortByDate:
		dm.sortBy = sortByDate
		dm.sortAndRefresh()

	// ===== 刷新 =====
	case idRefresh:
		dm.refreshDesktop()

	// ===== 粘贴 =====
	case idPaste:
		pasteFromClipboard(dm.workX, dm.workY)

	// ===== 粘贴快捷方式 =====
	case idPasteShortcut:
		pasteShortcutFromClipboard(dm.workX, dm.workY)

	// ===== 新建 =====
	case idNewFolder:
		createNewFolder(dm.workX, dm.workY)
	case idNewShortcut:
		createNewShortcut(dm.workX, dm.workY)
	case idNewTextDoc:
		createNewTextDocument(dm.workX, dm.workY)
	case idNewBitmap:
		createNewBitmapImage(dm.workX, dm.workY)

	// ===== 系统设置 =====
	case idDisplaySettings:
		openDisplaySettings()
	case idPersonalize:
		openPersonalize()
	}
}

// ============================================================
// 图标右键菜单 —— 使用 IContextMenu COM 接口显示系统原生菜单
// ============================================================

// COM GUID 定义
var (
	IID_IShellFolder    = comGUID{0x000214E6, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	IID_IContextMenu    = comGUID{0x000214E4, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// comGUID COM GUID 结构
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// CMINVOKECOMMANDINFO 结构
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

var (
	procCoUninitialize     = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree      = ole32.NewProc("CoTaskMemFree")

	procSHParseDisplayName = shell32Ctx.NewProc("SHParseDisplayName")
	procSHBindToParent     = shell32Ctx.NewProc("SHBindToParent")
	procILFree             = shell32Ctx.NewProc("ILFree")
)

// iContextMenuVtbl IContextMenu 虚函数表
type iContextMenuVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	QueryContextMenu uintptr
	InvokeCommand  uintptr
	GetCommandString uintptr
}

// iContextMenu IContextMenu 接口指针
type iContextMenu struct {
	vtbl *iContextMenuVtbl
}

// iShellFolderVtbl IShellFolder 虚函数表（只需部分方法）
type iShellFolderVtbl struct {
	QueryInterface     uintptr
	AddRef             uintptr
	Release            uintptr
	ParseDisplayName   uintptr
	EnumObjects        uintptr
	BindToObject       uintptr
	BindToStorage      uintptr
	CompareIDs         uintptr
	CreateViewObject   uintptr
	GetAttributesOf    uintptr
	GetUIObjectOf      uintptr
	GetDisplayNameOf   uintptr
	SetNameOf          uintptr
}

// iShellFolder IShellFolder 接口指针
type iShellFolder struct {
	vtbl *iShellFolderVtbl
}

// ShowIconContextMenu 使用 Windows Shell IContextMenu 显示文件的系统原生右键菜单
func (dm *DesktopMode) ShowIconContextMenu(hwnd win.HWND, mgr *group.Manager, executor *ProgramExecutor, item group.GroupItem, x, y int) {
	// 初始化 COM（每个线程需独立初始化，重复调用安全）
	comInitThread()
	defer procCoUninitialize.Call()

	filePath := item.Path

	// 1. 解析文件路径为 PIDL
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	var pidl uintptr
	hr, _, _ := procSHParseDisplayName.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&pidl)),
		0,
		0,
	)
	if hr != 0 || pidl == 0 {
		logger.Warn("SHParseDisplayName failed: 0x%08X path=%s", hr, filePath)
		return
	}
	defer procILFree.Call(pidl)

	// 2. 获取父文件夹的 IShellFolder 和子 PIDL
	var pShellFolder uintptr
	var pidlChild uintptr
	hr, _, _ = procSHBindToParent.Call(
		pidl,
		uintptr(unsafe.Pointer(&IID_IShellFolder)),
		uintptr(unsafe.Pointer(&pShellFolder)),
		uintptr(unsafe.Pointer(&pidlChild)),
	)
	if hr != 0 || pShellFolder == 0 {
		logger.Warn("SHBindToParent failed: 0x%08X", hr)
		return
	}

	// 获取 IShellFolder 虚函数表
	sf := (*iShellFolder)(unsafe.Pointer(pShellFolder))
	defer syscall.SyscallN(sf.vtbl.Release, pShellFolder)

	// 3. 通过 IShellFolder::GetUIObjectOf 获取 IContextMenu
	var pContextMenu uintptr
	hr, _, _ = syscall.SyscallN(
		sf.vtbl.GetUIObjectOf,
		pShellFolder,
		uintptr(hwnd),
		1, // cidl = 1 个文件
		uintptr(unsafe.Pointer(&pidlChild)),
		uintptr(unsafe.Pointer(&IID_IContextMenu)),
		0,
		uintptr(unsafe.Pointer(&pContextMenu)),
	)
	if hr != 0 || pContextMenu == 0 {
		logger.Warn("GetUIObjectOf(IContextMenu) failed: 0x%08X", hr)
		return
	}

	cm := (*iContextMenu)(unsafe.Pointer(pContextMenu))
	defer syscall.SyscallN(cm.vtbl.Release, pContextMenu)

	// 4. 创建弹出菜单并让 Shell 填充
	hMenu := createPopupMenu()
	if hMenu == 0 {
		return
	}
	defer destroyMenu(hMenu)

	// QueryContextMenu(hMenu, indexMenu, idCmdFirst, idCmdLast, uFlags)
	// CMF_NORMAL = 0x00000000, CMF_EXPLORE = 0x00000004
	const CMF_NORMAL = 0x00000000
	hr, _, _ = syscall.SyscallN(
		cm.vtbl.QueryContextMenu,
		pContextMenu,
		uintptr(hMenu),
		0,     // indexMenu
		1,     // idCmdFirst
		0x7FFF, // idCmdLast
		CMF_NORMAL,
	)
	if hr < 0 {
		logger.Warn("QueryContextMenu failed: 0x%08X", hr)
		return
	}

	// 5. 显示菜单
	cmd := trackPopupMenu(hMenu, TPM_RETURNCMD|TPM_LEFTALIGN|TPM_LEFTBUTTON|TPM_RIGHTBUTTON, x, y, hwnd)
	if cmd == 0 {
		return
	}

	// 6. 执行选中的命令
	var ici cmInvokeCommandInfo
	ici.cbSize = uint32(unsafe.Sizeof(ici))
	ici.hwnd = uintptr(hwnd)
	ici.lpVerb = uintptr(cmd - 1) // MAKEINTRESOURCE(cmd - idCmdFirst)
	ici.nShow = 1                 // SW_SHOWNORMAL
	ici.lpDirectory = uintptr(unsafe.Pointer(pathPtr))

	syscall.SyscallN(
		cm.vtbl.InvokeCommand,
		pContextMenu,
		uintptr(unsafe.Pointer(&ici)),
	)
}

// handleIconContextMenuCommand 处理图标右键菜单命令（已由 IContextMenu 接管，保留供兼容）
func (dm *DesktopMode) handleIconContextMenuCommand(mgr *group.Manager, executor *ProgramExecutor, item group.GroupItem, cmd int) {
	// 现在由 IContextMenu::InvokeCommand 处理，此函数保留以兼容其他调用
}

// ============================================================
// 图标操作辅助函数
// ============================================================

// cutFileToClipboard 剪切文件到剪贴板
func cutFileToClipboard(path string) {
	win.OpenClipboard(0)
	win.EmptyClipboard()

	pathUTF16, _ := syscall.UTF16FromString(path + "\x00")
	size := len(pathUTF16) * 2
	hMem := win.GlobalAlloc(win.GMEM_MOVEABLE|win.GMEM_ZEROINIT, uintptr(size))
	if hMem != 0 {
		pMem := win.GlobalLock(hMem)
		if uintptr(pMem) != 0 {
			for i, c := range pathUTF16 {
				*(*uint16)(unsafe.Pointer(uintptr(pMem) + uintptr(i*2))) = c
			}
			win.GlobalUnlock(hMem)
		}
		win.SetClipboardData(win.CF_UNICODETEXT, win.HANDLE(hMem))
	}
	win.CloseClipboard()
}

// copyFileToClipboard 复制文件到剪贴板
func copyFileToClipboard(path string) {
	win.OpenClipboard(0)
	win.EmptyClipboard()

	pathUTF16, _ := syscall.UTF16FromString(path + "\x00")
	size := len(pathUTF16) * 2
	hMem := win.GlobalAlloc(win.GMEM_MOVEABLE|win.GMEM_ZEROINIT, uintptr(size))
	if hMem != 0 {
		pMem := win.GlobalLock(hMem)
		if uintptr(pMem) != 0 {
			for i, c := range pathUTF16 {
				*(*uint16)(unsafe.Pointer(uintptr(pMem) + uintptr(i*2))) = c
			}
			win.GlobalUnlock(hMem)
		}
		win.SetClipboardData(win.CF_UNICODETEXT, win.HANDLE(hMem))
	}
	win.CloseClipboard()
}

// deleteFileToRecycleBin 将文件移动到回收站
func deleteFileToRecycleBin(path string) {
	// 使用 cmd 的 Recycle 命令
	exec.Command("cmd", "/c", "start", "", "shell:RecycleBinFolder").Start()

	// 标准方式：使用 SHEmptyRecycleBin 或 SHFileOperation
	// 使用 PowerShell 方式移动文件到回收站
	psCmd := fmt.Sprintf(`
$shell = New-Object -ComObject Shell.Application
$shell.NameSpace(0).ParseName('%s').InvokeVerb('delete')
`, filepath.Base(path))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()
}

// renameDesktopItem 重命名桌面图标项
func renameDesktopItem(item group.GroupItem) {
	newName, ok := ShowInputDialog(nil, "重命名", "请输入新名称：", item.Name)
	if ok && newName != "" && newName != item.Name {
		oldPath := item.Path
		ext := filepath.Ext(oldPath)
		newPath := filepath.Join(filepath.Dir(oldPath), newName+ext)
		if err := os.Rename(oldPath, newPath); err != nil {
			logger.Warn("rename failed: %v", err)
		}
	}
}

// showFileProperties 显示文件属性对话框
func showFileProperties(path string) {
	p, _ := syscall.UTF16PtrFromString(path)
	// 使用 ShellExecute 打开文件属性对话框
	procShellExecuteW := shell32Ctx.NewProc("ShellExecuteW")
	procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("properties"))),
		uintptr(unsafe.Pointer(p)),
		0, 0,
		1, // SW_NORMAL
	)
}

// ============================================================
// 右键菜单命令 ID 常量
// ============================================================
const (
	idViewLargeIcons       = 0x1001
	idViewMediumIcons      = 0x1002
	idViewSmallIcons       = 0x1003
	idViewAutoArrange      = 0x1004
	idViewAlignToGrid      = 0x1005
	idViewShowDesktopIcons = 0x1006

	idSortByName = 0x1011
	idSortBySize = 0x1012
	idSortByType = 0x1013
	idSortByDate = 0x1014

	idRefresh = 0x1021

	idPaste         = 0x1031
	idPasteShortcut = 0x1032

	idNewFolder  = 0x1041
	idNewShortcut = 0x1042
	idNewTextDoc  = 0x1043
	idNewBitmap   = 0x1044

	idDisplaySettings = 0x1051
	idPersonalize     = 0x1052

	// 图标右键菜单命令 ID
	idIconOpen            = 0x2001
	idIconOpenFileLoc     = 0x2002
	idIconPinToStart      = 0x2003
	idIconSendToDesktop   = 0x2011
	idIconSendToMail      = 0x2012
	idIconCut             = 0x2021
	idIconCopy            = 0x2022
	idIconDelete          = 0x2023
	idIconRename          = 0x2024
	idIconProperties      = 0x2031
)

// ============================================================
// 桌面图标大小状态
// ============================================================

// 图标尺寸级别
const (
	iconSizeLarge  = 0
	iconSizeMedium = 1
	iconSizeSmall  = 2
)

// 排序方式
const (
	sortByName = 0
	sortBySize = 1
	sortByType = 2
	sortByDate = 3
)

// getDesktopIconSize 获取当前桌面图标大小
func getDesktopIconSize() int {
	if desktopIconItemWidth >= 120 {
		return iconSizeLarge
	} else if desktopIconItemWidth >= 90 {
		return iconSizeMedium
	}
	return iconSizeSmall
}

// setDesktopIconSize 设置桌面图标大小
func setDesktopIconSize(size int) {
	switch size {
	case iconSizeLarge:
		desktopIconItemWidth = 132
		desktopIconItemHeight = 132
	case iconSizeMedium:
		desktopIconItemWidth = 100
		desktopIconItemHeight = 100
	case iconSizeSmall:
		desktopIconItemWidth = 72
		desktopIconItemHeight = 72
	}
	// 重置磁贴尺寸测量标记，下次绘制时重新计算
	forceTileRemeasure()
}

// forceTileRemeasure 强制重新测量磁贴尺寸
var forceRemeasure bool
var forceRemeasureMu sync.Mutex

func forceTileRemeasure() {
	forceRemeasureMu.Lock()
	defer forceRemeasureMu.Unlock()
	forceRemeasure = true
}

// isTileRemeasureNeeded 检查是否需要重新测量并重置标记
func isTileRemeasureNeeded() bool {
	forceRemeasureMu.Lock()
	defer forceRemeasureMu.Unlock()
	if forceRemeasure {
		forceRemeasure = false
		return true
	}
	return false
}

// ============================================================
// 桌面状态管理
// ============================================================

// sortAndRefresh 按当前排序方式重新排序并刷新显示
func (dm *DesktopMode) sortAndRefresh() {
	dm.bodyWidget.Invalidate()
}

// autoArrangeIcons 自动排列图标
func (dm *DesktopMode) autoArrangeIcons() {
	items := dm.manager.GetUngroupedItems()
	for i, item := range items {
		col := i % 8
		row := i / 8
		relPos := dm.gridToRel(col, row)
		dm.manager.SetFreeItemPosition(item.Path, relPos)
	}
}

// refreshDesktop 刷新桌面
func (dm *DesktopMode) refreshDesktop() {
	dm.loadWallpaper()
	dm.manager.ReloadDesktopItems()
	dm.bodyWidget.Invalidate()
}

// ============================================================
// 剪贴板操作
// ============================================================

// pasteFromClipboard 从剪贴板粘贴文件到桌面
func pasteFromClipboard(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	// 注册 CF_HDROP 格式
	cfHDrop := registerClipboardFormat("CF_HDROP")
	if cfHDrop == 0 {
		cfHDrop = CF_HDROP
	}

	if !openClipboard(0) {
		return
	}
	defer closeClipboard()

	hDrop := getClipboardData(cfHDrop)
	if hDrop == 0 {
		return
	}

	fileCount := dragQueryFileCount(hDrop)
	if fileCount == 0 {
		return
	}

	for i := uint32(0); i < fileCount; i++ {
		buf := make([]uint16, 260)
		dragQueryFile(hDrop, i, &buf[0], uint32(len(buf)))
		srcPath := syscall.UTF16ToString(buf)
		if srcPath == "" {
			continue
		}
		destPath := filepath.Join(desktopPath, filepath.Base(srcPath))
		copyFileOrDir(srcPath, destPath)
	}

	dragFinish(hDrop)
}

// pasteShortcutFromClipboard 从剪贴板粘贴快捷方式到桌面
func pasteShortcutFromClipboard(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	cfHDrop := registerClipboardFormat("CF_HDROP")
	if cfHDrop == 0 {
		cfHDrop = CF_HDROP
	}

	if !openClipboard(0) {
		return
	}
	defer closeClipboard()

	hDrop := getClipboardData(cfHDrop)
	if hDrop == 0 {
		return
	}

	fileCount := dragQueryFileCount(hDrop)
	if fileCount == 0 {
		return
	}

	for i := uint32(0); i < fileCount; i++ {
		buf := make([]uint16, 260)
		dragQueryFile(hDrop, i, &buf[0], uint32(len(buf)))
		srcPath := syscall.UTF16ToString(buf)
		if srcPath == "" {
			continue
		}
		shortcutName := filepath.Base(srcPath)
		ext := filepath.Ext(shortcutName)
		shortcutName = shortcutName[:len(shortcutName)-len(ext)] + ".lnk"
		destPath := filepath.Join(desktopPath, shortcutName)
		createShortcut(srcPath, destPath)
	}

	dragFinish(hDrop)
}

// 剪贴板 API 封装
func openClipboard(hwnd uintptr) bool {
	ret, _, _ := procOpenClipboard.Call(hwnd)
	return ret != 0
}

func closeClipboard() {
	procCloseClipboard.Call()
}

func getClipboardData(format uint32) uintptr {
	ret, _, _ := procGetClipboardData.Call(uintptr(format))
	return ret
}

func registerClipboardFormat(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	ret, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(ret)
}

func dragQueryFileCount(hDrop uintptr) uint32 {
	ret, _, _ := procDragQueryFile.Call(hDrop, 0xFFFFFFFF, 0, 0)
	return uint32(ret)
}

func dragQueryFile(hDrop uintptr, i uint32, buf *uint16, size uint32) {
	procDragQueryFile.Call(hDrop, uintptr(i), uintptr(unsafe.Pointer(buf)), uintptr(size))
}

func dragFinish(hDrop uintptr) {
	procDragFinish.Call(hDrop)
}

// ============================================================
// 新建操作
// ============================================================

// createNewFolder 在桌面创建新文件夹
func createNewFolder(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	folderPath := filepath.Join(desktopPath, "新建文件夹")
	for i := 1; ; i++ {
		if _, err := os.Stat(folderPath); os.IsNotExist(err) {
			break
		}
		folderPath = filepath.Join(desktopPath, fmt.Sprintf("新建文件夹 (%d)", i))
	}

	if err := os.MkdirAll(folderPath, 0755); err != nil {
		logger.Warn("createNewFolder failed: %v", err)
	}
}

// createNewShortcut 创建新快捷方式
func createNewShortcut(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	shortcutPath := filepath.Join(desktopPath, "新建快捷方式.lnk")
	for i := 1; ; i++ {
		if _, err := os.Stat(shortcutPath); os.IsNotExist(err) {
			break
		}
		shortcutPath = filepath.Join(desktopPath, fmt.Sprintf("新建快捷方式 (%d).lnk", i))
	}

	createShortcut("C:\\Windows\\System32\\cmd.exe", shortcutPath)
}

// createNewTextDocument 创建新文本文档
func createNewTextDocument(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	docPath := filepath.Join(desktopPath, "新建文本文档.txt")
	for i := 1; ; i++ {
		if _, err := os.Stat(docPath); os.IsNotExist(err) {
			break
		}
		docPath = filepath.Join(desktopPath, fmt.Sprintf("新建文本文档 (%d).txt", i))
	}

	if err := os.WriteFile(docPath, []byte{}, 0644); err != nil {
		logger.Warn("createNewTextDocument failed: %v", err)
	}
}

// createNewBitmapImage 创建新位图图像
func createNewBitmapImage(_, _ int) {
	desktopPath := getDesktopPath()
	if desktopPath == "" {
		return
	}

	bmpPath := filepath.Join(desktopPath, "新建位图图像.bmp")
	for i := 1; ; i++ {
		if _, err := os.Stat(bmpPath); os.IsNotExist(err) {
			break
		}
		bmpPath = filepath.Join(desktopPath, fmt.Sprintf("新建位图图像 (%d).bmp", i))
	}

	// 创建最小的有效 BMP 文件（1x1 像素，24位）
	bmpData := []byte{
		0x42, 0x4D, // "BM"
		0x3E, 0x00, 0x00, 0x00, // 文件大小
		0x00, 0x00, // 保留
		0x00, 0x00, // 保留
		0x36, 0x00, 0x00, 0x00, // 数据偏移
		0x28, 0x00, 0x00, 0x00, // 信息头大小
		0x01, 0x00, 0x00, 0x00, // 宽度 = 1
		0x01, 0x00, 0x00, 0x00, // 高度 = 1
		0x01, 0x00, // 平面数 = 1
		0x18, 0x00, // 位深 = 24
		0x00, 0x00, 0x00, 0x00, // 压缩 = 无
		0x0C, 0x00, 0x00, 0x00, // 图像大小
		0x00, 0x00, 0x00, 0x00, // 水平分辨率
		0x00, 0x00, 0x00, 0x00, // 垂直分辨率
		0x00, 0x00, 0x00, 0x00, // 颜色数
		0x00, 0x00, 0x00, 0x00, // 重要颜色数
		// 像素数据（1x1 白色像素 + 补齐到4字节）
		0xFF, 0xFF, 0xFF, 0x00,
	}

	if err := os.WriteFile(bmpPath, bmpData, 0644); err != nil {
		logger.Warn("createNewBitmapImage failed: %v", err)
	}
}

// ============================================================
// 系统设置
// ============================================================

// openDisplaySettings 打开显示设置
func openDisplaySettings() {
	exec.Command("cmd", "/c", "start", "ms-settings:display").Start()
}

// openPersonalize 打开个性化设置
func openPersonalize() {
	exec.Command("cmd", "/c", "start", "ms-settings:personalization").Start()
}

// ============================================================
// 辅助函数
// ============================================================

// getDesktopPath 获取桌面路径
func getDesktopPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	desktopPath := filepath.Join(home, "Desktop")
	if info, err := os.Stat(desktopPath); err == nil && info.IsDir() {
		return desktopPath
	}
	publicDesktop := `C:\Users\Public\Desktop`
	if info, err := os.Stat(publicDesktop); err == nil && info.IsDir() {
		return publicDesktop
	}
	return home
}

// copyFileOrDir 复制文件或目录
func copyFileOrDir(src, dst string) {
	cmd := exec.Command("cmd", "/c", "xcopy", "/E", "/I", "/Y", src, dst)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()
}

// createShortcut 使用 PowerShell 创建快捷方式
func createShortcut(targetPath, shortcutPath string) {
	psCmd := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut('%s')
$Shortcut.TargetPath = '%s'
$Shortcut.Save()
`, shortcutPath, targetPath)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		logger.Warn("createShortcut failed: %v", err)
	}
}
