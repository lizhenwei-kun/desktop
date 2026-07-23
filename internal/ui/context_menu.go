package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// 桌面背景菜单中已内置的 Shell verb 列表，
// 读取注册表扩展菜单时需跳过这些 verbs，避免重复。
var desktopReservedVerbs = map[string]bool{
	"paste":       true,
	"pastelink":   true,
	"refresh":     true,
	"display":     true,
	"personalize": true,
	"new":         true,
	"view":        true,
	"arrangeby":   true,
	"arrange":     true,
	"properties":  true,
}

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

	MaxCmdIDDynamic     = 0x3000 // 动态命令ID起始值（桌面）
	maxCmdIDIconDynamic = 0x4000 // 动态命令ID起始值（图标）
	maxCmdIDSendTo      = 0x5000 // 发送到动态命令ID起始值
)

// RegistryShellItem 注册表shell菜单项
type RegistryShellItem struct {
	Verb     string // 动词名（如 "open", "edit"）
	Name     string // 显示名称
	Command  string // 执行命令
	IsDir    bool   // 是否文件夹类型路径
	CmdID    int    // 动态分配的命令ID（桌面/图标菜单共用）
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
func readRegistryShellItems(regRoot uintptr, subKey string) []RegistryShellItem {
	var items []RegistryShellItem

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

		items = append(items, RegistryShellItem{
			Verb:    verb,
			Name:    itemName,
			Command: cmdLine,
			IsDir:   false,
		})
	}
	return items
}

// appendRegistryMenuItems 将注册表菜单项附加到弹出菜单
// 返回添加的最后一个命令ID（供后续使用）
func appendRegistryMenuItems(menu hMenu, items []RegistryShellItem, cmdIDStart int) int {
	if len(items) == 0 {
		return cmdIDStart
	}

	appendMenuSeparator(menu)

	nextID := cmdIDStart
	for _, item := range items {
		if item.Name == "" || item.Name == "-" {
			appendMenuSeparator(menu)
			continue
		}
		item.CmdID = nextID
		appendMenu(menu, MF_STRING, uintptr(nextID), syscall.StringToUTF16Ptr(item.Name))
		nextID++
	}
	return nextID
}

// executeRegistryCommand 执行注册表菜单命令
func ExecuteRegistryCommand(cmdLine string, filePath string) {
	if cmdLine == "" {
		return
	}

	// 替换占位符
	cmdLine = strings.ReplaceAll(cmdLine, "%1", filePath)
	cmdLine = strings.ReplaceAll(cmdLine, "%L", filePath)
	cmdLine = strings.ReplaceAll(cmdLine, "%V", filePath)
	// 移除残留的未替换占位符（%W, %D 等）
	cmdLine = removeUnresolvedPlaceholders(cmdLine)
	if strings.TrimSpace(cmdLine) == "" {
		return
	}

	// 使用 cmd /c 执行整个命令行，正确处理引号、空格等复杂路径
	cmd := exec.Command("cmd", "/c", cmdLine)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	if err := cmd.Start(); err != nil {
		logger.Warn("executeRegistryCommand failed: %v (cmdLine=%s)", err, cmdLine)
	}
}

// removeUnresolvedPlaceholders 移除命令行中残留的 % 占位符
func removeUnresolvedPlaceholders(cmdLine string) string {
	var result strings.Builder
	i := 0
	for i < len(cmdLine) {
		if cmdLine[i] == '%' && i+1 < len(cmdLine) {
			next := cmdLine[i+1]
			// 跳过 %X 形式的单字符占位符（如 %V, %W, %1 等）
			if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
				i += 2
				continue
			}
		}
		result.WriteByte(cmdLine[i])
		i++
	}
	return result.String()
}

// readDesktopRegistryMenu 读取桌面背景的注册表菜单项
func ReadDesktopRegistryMenu() []RegistryShellItem {
	// 先尝试当前用户设置，再尝试全局设置
	items := readRegistryShellItems(regHKEY_CURRENT_USER, `Software\Classes\Directory\Background\shell`)
	if len(items) == 0 {
		items = readRegistryShellItems(regHKEY_CLASSES_ROOT, `Directory\Background\shell`)
	}

	// 过滤掉已内置的菜单项（避免重复）
	filtered := items[:0]
	for _, item := range items {
		if !desktopReservedVerbs[strings.ToLower(item.Verb)] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// readFileRegistryMenu 读取文件类型的注册表菜单项
func ReadFileRegistryMenu(filePath string) []RegistryShellItem {
	var items []RegistryShellItem

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
func ReadSendToMenu() []RegistryShellItem {
	// 从 ShellNew\SendTo 或实际的 SendTo 文件夹读取
	sendToPath := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\SendTo`)
	var items []RegistryShellItem
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
		items = append(items, RegistryShellItem{
			Verb:    name,
			Name:    displayName,
			Command: fullPath,
			CmdID:   cmdID,
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

// ============================================================
// 图标操作辅助函数
// ============================================================

// CutFileToClipboard 剪切文件到剪贴板
func CutFileToClipboard(path string) {
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

// CopyFileToClipboard 复制文件到剪贴板
func CopyFileToClipboard(path string) {
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

// DeleteFileToRecycleBin 将文件移动到回收站
func DeleteFileToRecycleBin(path string) {
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

// 图标尺寸级别（导出供其他包使用）
const (
	IconSizeLarge  = 0
	IconSizeMedium = 1
	IconSizeSmall  = 2
)

const (
	iconSizeLarge  = IconSizeLarge
	iconSizeMedium = IconSizeMedium
	iconSizeSmall  = IconSizeSmall
)

// 排序方式
const (
	sortByName = 0
	sortBySize = 1
	sortByType = 2
	sortByDate = 3
)

// getDesktopIconSize 获取当前桌面图标大小
func GetDesktopIconSize() int {
	switch desktopIconSizeBase {
	case 48:
		return iconSizeLarge // 大档/中档都用 48px，通过档位记录区分
	case 28:
		return iconSizeSmall
	default:
		return iconSizeSmall
	}
}

// GetDesktopIconSizeLevel 返回当前图标档位（基于配置值，不依赖像素尺寸）
// 因为大档和中档现在都用 48px，需要额外的档位状态
var currentIconSizeLevel = iconSizeLarge

// SetDesktopIconSizeLevel 设置当前图标档位
func SetDesktopIconSizeLevel(level int) {
	currentIconSizeLevel = level
}

// CurrentIconSizeLevel 获取当前图标档位
func CurrentIconSizeLevel() int {
	return currentIconSizeLevel
}

// setDesktopIconSize 设置桌面图标大小（同时调整标签字号）
//
// 档位规格：
//   - 大档：图标 48px，文字 11pt，磁贴间距 10px
//   - 中档：图标 48px（与大档同尺寸），文字 10pt，磁贴间距 8px
//   - 小档：图标 32px，文字 9pt， 磁贴间距 6px
//
// 中档使用 48px 标准尺寸，避免 40px 非标尺寸导致部分 .ico 加载失败
// 或缩放时出现白色框等异常。切换档位时必须清空图标缓存。
func SetDesktopIconSize(size int) {
	logger.Info("SetDesktopIconSize: ENTER size=%d (0=large/48, 1=medium/48, 2=small/32), dpi=%d, prevBase=%d",
		size, CurrentDPI(), desktopIconSizeBase)
	SetDesktopIconSizeLevel(size) // 记录档位
	switch size {
	case iconSizeLarge:
		desktopIconSizeBase = 48
		setIconFontSize(11) // 大档 11pt
		setIconGap(10)
	case iconSizeMedium:
		desktopIconSizeBase = 48 // 使用标准 48px，避免 40px 非标尺寸导致 .ico 加载/缩放问题
		setIconFontSize(10)       // 中档 10pt
		setIconGap(8)
	case iconSizeSmall:
		desktopIconSizeBase = 32 // 与 Windows 系统小图标尺寸一致
		setIconFontSize(9) // 小档 9pt
		setIconGap(6)
	}
	logger.Info("SetDesktopIconSize: newBase=%d newTarget=%d", desktopIconSizeBase, DesktopIconSize())
	// 重置磁贴尺寸测量标记，下次绘制时重新计算
	ForceTileRemeasure()
	// 清空图标缓存，强制按新档位重新提取（image 层缓存和 bitmap 层缓存都清）
	ClearIconCaches()
}

// ClearIconCaches 清空所有图标缓存（iconCache + GlobalIconBmpCache）
// 切换图标档位或 DPI 变化后调用，确保新档位按目标尺寸重新加载
func ClearIconCaches() {
	cnt := 0
	iconCache.Range(func(k, v interface{}) bool {
		iconCache.Delete(k)
		cnt++
		return true
	})
	GlobalIconBmpCache.Clear()
	logger.Info("ClearIconCaches: cleared iconCache entries=%d, GlobalIconBmpCache cleared", cnt)
}

// setIconFontSize 线程安全地设置图标标签字号
func setIconFontSize(size int) {
	iconFontMu.Lock()
	defer iconFontMu.Unlock()
	iconFontSize = size
}

// forceTileRemeasure 强制重新测量磁贴尺寸
var forceRemeasure bool
var forceRemeasureMu sync.Mutex

func ForceTileRemeasure() {
	forceRemeasureMu.Lock()
	defer forceRemeasureMu.Unlock()
	forceRemeasure = true
}

// isTileRemeasureNeeded 检查是否需要重新测量并重置标记
func IsTileRemeasureNeeded() bool {
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

// ============================================================
// 剪贴板操作
// ============================================================

// pasteFromClipboard 从剪贴板粘贴文件到桌面
func PasteFromClipboard(_, _ int) {
	desktopPath := GetDesktopPath()
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
func PasteShortcutFromClipboard(_, _ int) {
	desktopPath := GetDesktopPath()
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
func CreateNewFolder(_, _ int) {
	desktopPath := GetDesktopPath()
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
func CreateNewShortcut(_, _ int) {
	desktopPath := GetDesktopPath()
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
func CreateNewTextDocument(_, _ int) {
	desktopPath := GetDesktopPath()
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
func CreateNewBitmapImage(_, _ int) {
	desktopPath := GetDesktopPath()
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

// shell32.dll 已经声明（顶部 var shell32Ctx）
var procShellExecuteW = shell32Ctx.NewProc("ShellExecuteW")

// openSystemSettingsURI 通过 Windows 设置 URI 打开系统设置页
// 使用 ShellExecuteW 替代 cmd /c start，更可靠，错误也更易捕获。
// 在 Windows 10/11 上可通过 ms-settings: URI 打开系统设置。
// uri 示例：
//   - ms-settings:display          显示
//   - ms-settings:personalization  个性化
//   - ms-settings:personalization-background 个性化 - 背景
func openSystemSettingsURI(uri string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(uri)
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0,
		1, // SW_NORMAL
	)
	// ShellExecuteW 返回值 > 32 表示成功
	if uintptr(ret) <= 32 {
		logger.Warn("openSystemSettingsURI(%q) failed: code=%d", uri, ret)
	} else {
		logger.Info("openSystemSettingsURI(%q) success", uri)
	}
}

// OpenDisplaySettings 打开显示设置
func OpenDisplaySettings() {
	openSystemSettingsURI("ms-settings:display")
}

// OpenPersonalize 打开个性化设置
func OpenPersonalize() {
	openSystemSettingsURI("ms-settings:personalization")
}

// OpenPersonalizeBackground 打开个性化 - 背景设置（快捷入口）
func OpenPersonalizeBackground() {
	openSystemSettingsURI("ms-settings:personalization-background")
}

// ============================================================
// 辅助函数
// ============================================================

// GetDesktopPath 获取桌面路径（导出供其他包使用）
func GetDesktopPath() string {
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

// ============================================================
// 图标右键菜单缓存（供 desktop 包定时刷新用）
// ============================================================

// QueryIconMenuItems 通过 COM IContextMenu 查询文件的右键菜单项
// 使用 explorer.exe 作为通用测试文件，获取完整的 Shell 菜单，
// 包含注册表项 + COM 扩展处理器（如 7-Zip、VS Code 等第三方软件的菜单）。
// 导出供 desktop 包定时缓存使用。
func QueryIconMenuItems() ([]RegistryShellItem, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ComInitThread()
	defer procCoUninitialize.Call()

	filePath := "C:\\Windows\\explorer.exe"
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)

	var pidl uintptr
	hr, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if hr != 0 || pidl == 0 {
		logger.Warn("QueryIconMenuItems: SHParseDisplayName failed for %q", filePath)
		return nil, false
	}
	defer procILFree.Call(pidl)

	var pShellFolder uintptr
	var pidlChild uintptr
	hr, _, _ = procSHBindToParent.Call(pidl, uintptr(unsafe.Pointer(&IID_IShellFolder)), uintptr(unsafe.Pointer(&pShellFolder)), uintptr(unsafe.Pointer(&pidlChild)))
	if hr != 0 || pShellFolder == 0 {
		logger.Warn("QueryIconMenuItems: SHBindToParent failed")
		return nil, false
	}
	sf := (*iShellFolder)(unsafe.Pointer(pShellFolder))
	defer syscall.SyscallN(sf.vtbl.Release, pShellFolder)

	var pContextMenu uintptr
	hr, _, _ = syscall.SyscallN(sf.vtbl.GetUIObjectOf, pShellFolder, 0, 1, uintptr(unsafe.Pointer(&pidlChild)), uintptr(unsafe.Pointer(&IID_IContextMenu)), 0, uintptr(unsafe.Pointer(&pContextMenu)))
	if hr != 0 || pContextMenu == 0 {
		logger.Warn("QueryIconMenuItems: GetUIObjectOf failed")
		return nil, false
	}
	cm := (*iContextMenu)(unsafe.Pointer(pContextMenu))
	defer syscall.SyscallN(cm.vtbl.Release, pContextMenu)

	hMenu := createPopupMenu()
	if hMenu == 0 {
		return nil, false
	}
	defer destroyMenu(hMenu)

	const CMF_NORMAL = 0x00000000
	const CMF_EXPLORE = 0x00000004
	hr, _, _ = syscall.SyscallN(cm.vtbl.QueryContextMenu, pContextMenu, uintptr(hMenu), 0, 1, 0x7FFF, CMF_NORMAL|CMF_EXPLORE)
	if int32(hr) < 0 {
		return nil, false
	}

	itemCount := getMenuItemCount(hMenu)
	if itemCount <= 0 {
		return nil, false
	}

	var items []RegistryShellItem
	cmdID := 0x5000

	for i := 0; i < itemCount; i++ {
		var buf [256]uint16
		var info struct {
			cbSize        uint32
			fMask         uint32
			fType         uint32
			fState        uint32
			wID           uint32
			hSubMenu      uintptr
			hbmpChecked   uintptr
			hbmpUnchecked uintptr
			dwItemData    uintptr
			dwTypeData    uintptr
			cch           uint32
			hbmpItem      uintptr
		}
		info.cbSize = uint32(unsafe.Sizeof(info))
		info.fMask = 0x00000040 // MIIM_STRING
		info.dwTypeData = uintptr(unsafe.Pointer(&buf[0]))
		info.cch = uint32(len(buf))

		ret, _, _ := procGetMenuItemInfoW.Call(uintptr(hMenu), uintptr(i), 1, uintptr(unsafe.Pointer(&info)))
		if ret == 0 || info.wID == 0 {
			continue
		}

		itemName := syscall.UTF16ToString(buf[:])

		if itemName == "" || itemName == "-" {
			continue
		}

		verbStr := getVerbFromContextMenu(cm, uintptr(info.wID-1))

		items = append(items, RegistryShellItem{
			Verb:    verbStr,
			Name:    itemName,
			Command: verbStr,
			CmdID:   cmdID,
		})
		cmdID++
	}

	return items, true
}

// getVerbFromContextMenu 从 IContextMenu 获取指定命令的 verb 名称
func getVerbFromContextMenu(cm *iContextMenu, cmdOffset uintptr) string {
	verbA := make([]byte, 512)
	hrA, _, _ := syscall.SyscallN(cm.vtbl.GetCommandString, uintptr(unsafe.Pointer(cm)), cmdOffset, 0x0004, 0, uintptr(unsafe.Pointer(&verbA[0])), uintptr(len(verbA)))
	if int32(hrA) >= 0 {
		if len(verbA) >= 2 {
			_ = verbA[1]
		}
		verbW := unsafe.Slice((*uint16)(unsafe.Pointer(&verbA[0])), len(verbA)/2)
		verbName := syscall.UTF16ToString(verbW)
		if verbName != "" {
			return verbName
		}
		n := bytes.IndexByte(verbA, 0)
		if n < 0 {
			n = len(verbA)
		}
		if n > 0 {
			return string(verbA[:n])
		}
	}

	verbW2 := make([]uint16, 256)
	hrW, _, _ := syscall.SyscallN(cm.vtbl.GetCommandString, uintptr(unsafe.Pointer(cm)), cmdOffset, 0x0044, 0, uintptr(unsafe.Pointer(&verbW2[0])), uintptr(len(verbW2)))
	if int32(hrW) >= 0 {
		return syscall.UTF16ToString(verbW2)
	}

	return ""
}

// Win32 API（供 QueryIconMenuItems 使用）
var (
	user32MenuInfo       = syscall.NewLazyDLL("user32.dll")
	procGetMenuItemInfoW = user32MenuInfo.NewProc("GetMenuItemInfoW")
)
