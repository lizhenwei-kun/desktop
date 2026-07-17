package ui

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
)

// ============================================================
// Win32 / COM API 声明
// ============================================================

var (
	// ole32 在 windows_icon.go 中已定义，此处使用已存在的变量
	// 注意：windows_icon.go 中定义的是 var ole32 = syscall.NewLazyDLL("ole32.dll")
	// 因此在同一包内直接使用 ole32

	// OLE API 预留，待后续实现完整 OLE 拖出时启用
	// procOleInitialize    = ole32.NewProc("OleInitialize")
	// procOleUninitialize  = ole32.NewProc("OleUninitialize")
	// procDoDragDrop       = ole32.NewProc("DoDragDrop")
	// procReleaseStgMedium = ole32.NewProc("ReleaseStgMedium")
)

// COM 常量
const (
	DROPEFFECT_NONE = 0
	DROPEFFECT_COPY = 1
	DROPEFFECT_MOVE = 2
	DROPEFFECT_LINK = 4
)

// DROPFILES 结构体定义（与 Win32 SDK 一致）
type DROPFILES struct {
	pFiles uint32
	pt     win.POINT
	fNC    win.BOOL
	fWide  win.BOOL
}

// ============================================================
// 拖放回调函数类型
// ============================================================

// OnFilesDropped 文件被拖入时的回调
type OnFilesDropped func(files []string, screenX, screenY int)

// OnExternalDragStart 开始对外拖出时的回调
type OnExternalDragStart func(filePath string) bool

// ============================================================
// 对外拖出状态
// ============================================================

var (
	externalDragActive    bool
	externalDragPath      string
	externalDragStartCB   OnExternalDragStart
	externalDragCleanupFn func()
)

// IsExternalDragActive 判断是否正在进行对外拖出
func IsExternalDragActive() bool {
	return externalDragActive
}

// StartExternalDrag 开始对外拖出（使用剪贴板 CF_HDROP 模拟）
// filePath: 被拖拽的文件路径
// cleanup: 拖放结束后的清理函数
// 返回 true 表示成功启动
func StartExternalDrag(filePath string, cleanup func()) bool {
	if filePath == "" {
		return false
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false
	}

	// 将文件路径复制到剪贴板（CF_HDROP 格式）
	success := CopyFilesToClipboard([]string{filePath})
	if !success {
		logger.Error("StartExternalDrag: CopyFilesToClipboard failed")
		return false
	}

	externalDragActive = true
	externalDragPath = filePath
	externalDragCleanupFn = cleanup

	logger.Debug("StartExternalDrag: file=%s, files copied to clipboard", filePath)

	// 使用后台 goroutine 尝试 OLE DoDragDrop（如果后续实现）
	go func() {
		// 目前通过剪贴板实现
		// 实际 OLE 拖出需要完整的 IDataObject COM 实现，后续可扩展
		cleanupExternalDrag()
	}()

	return true
}

// CancelExternalDrag 取消对外拖出
func CancelExternalDrag() {
	externalDragActive = false
	externalDragPath = ""
	externalDragStartCB = nil
	externalDragCleanupFn = nil
}

// cleanupExternalDrag 清理对外拖出状态
func cleanupExternalDrag() {
	externalDragActive = false
	externalDragPath = ""
	if externalDragCleanupFn != nil {
		externalDragCleanupFn()
		externalDragCleanupFn = nil
	}
	externalDragStartCB = nil
}

// ============================================================
// 剪贴板工具
// ============================================================

// CopyFilesToClipboard 将文件路径复制到剪贴板（CF_HDROP 格式）
func CopyFilesToClipboard(files []string) bool {
	if len(files) == 0 {
		return false
	}

	if !win.OpenClipboard(win.HWND(0)) {
		return false
	}
	defer win.CloseClipboard()

	if !win.EmptyClipboard() {
		return false
	}

	// 构建文件列表（UTF16，双 null 终止）
	var utf16Data []uint16
	for _, f := range files {
		utf16Path, _ := syscall.UTF16FromString(f)
		utf16Data = append(utf16Data, utf16Path...)
	}
	utf16Data = append(utf16Data, 0) // 第二个 null 终止

	// 计算总大小
	dropFilesSize := int(unsafe.Sizeof(DROPFILES{}))
	dataSize := len(utf16Data) * 2
	totalSize := dropFilesSize + dataSize

	// 分配全局内存
	hGlobal := win.GlobalAlloc(win.GMEM_MOVEABLE|win.GMEM_ZEROINIT, uintptr(totalSize))
	if hGlobal == 0 {
		return false
	}

	pGlobal := win.GlobalLock(hGlobal)
	if pGlobal == nil {
		win.GlobalFree(hGlobal)
		return false
	}

	// 写入 DROPFILES 结构体
	df := (*DROPFILES)(unsafe.Pointer(pGlobal))
	df.pFiles = uint32(dropFilesSize)
	df.fWide = win.TRUE

	// 写入文件路径
	pathPtr := (*uint16)(unsafe.Pointer(uintptr(pGlobal) + uintptr(dropFilesSize)))
	copy((*[1 << 20]uint16)(unsafe.Pointer(pathPtr))[:len(utf16Data)], utf16Data)

	win.GlobalUnlock(hGlobal)

	// 设置剪贴板数据
	if win.SetClipboardData(CF_HDROP, win.HANDLE(hGlobal)) == 0 {
		win.GlobalFree(hGlobal)
		return false
	}

	return true
}

// ============================================================
// 高层 API：集成到 DesktopMode 的回调
// ============================================================

// HandleExternalDrop 处理外部文件拖入
// 根据屏幕坐标确定目标区域（卡片/桌面），并添加到 Manager
func HandleExternalDrop(files []string, screenX, screenY int, mgr *group.Manager, cards []*GroupCard, workW, workH int) {
	logger.Debug("HandleExternalDrop: %d files at (%d,%d)", len(files), screenX, screenY)

	var targetCard *GroupCard
	for _, card := range cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			targetCard = card
			break
		}
	}

	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		ext := filepath.Ext(fileName)
		displayName := fileName[:len(fileName)-len(ext)]

		if targetCard != nil {
			mgr.AddItemToGroup(targetCard.GroupName(), filePath, displayName)
			logger.Debug("HandleExternalDrop: added %s to group %s", filePath, targetCard.GroupName())
		} else {
			mgr.AddItemToDesktop(filePath, displayName)
			logger.Debug("HandleExternalDrop: added %s to desktop (ungrouped)", filePath)
		}
	}

	for _, card := range cards {
		card.Refresh()
	}
}

// GetDropTargetInfo 获取拖放释放位置的目标信息
func GetDropTargetInfo(screenX, screenY int, cards []*GroupCard) (*GroupCard, int, int) {
	for _, card := range cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			clientX := screenX - sb.X
			clientY := screenY - sb.Y
			return card, clientX, clientY
		}
	}
	return nil, 0, 0
}

// RefreshCardsAfterDrop 拖放后刷新所有卡片
func RefreshCardsAfterDrop(cards []*GroupCard) {
	for _, card := range cards {
		card.Refresh()
	}
}
