package desktop

import (
	"syscall"
	"unsafe"

	"desktop_go/internal/logger"
	"github.com/lxn/win"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	comctl32             = syscall.NewLazyDLL("comctl32.dll")

	procFindWindowW      = user32.NewProc("FindWindowW")
	procSetParent        = user32.NewProc("SetParent")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procMoveWindow       = user32.NewProc("MoveWindow")
	procSetWindowLongW   = user32.NewProc("SetWindowLongW")
	procGetWindowLongW   = user32.NewProc("GetWindowLongW")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procSendMessageW     = user32.NewProc("SendMessageTimeoutW")
	procFindWindowExW    = user32.NewProc("FindWindowExW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procSetMenu          = user32.NewProc("SetMenu")
	procDrawMenuBar      = user32.NewProc("DrawMenuBar")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procSendMessageW2    = user32.NewProc("SendMessageW")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procGetClassNameW        = user32.NewProc("GetClassNameW")
	procEnumChildWindows     = user32.NewProc("EnumChildWindows")
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32.NewProc("DefSubclassProc")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procInvalidateRect       = user32.NewProc("InvalidateRect")

)

const (
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	GWL_STYLE   = -16
	GWL_EXSTYLE = -20

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_POPUP            = 0x80000000
	WS_VISIBLE          = 0x10000000
	WS_CAPTION          = 0x00C00000
	WS_THICKFRAME       = 0x00040000
	WS_SYSMENU          = 0x00080000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_MINIMIZEBOX      = 0x00020000
	WS_BORDER           = 0x00800000

	WS_EX_TOOLWINDOW    = 0x00000080
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_WINDOWEDGE    = 0x00000100
	WS_EX_STATICEDGE    = 0x00020000
	WS_EX_DLGMODALFRAME = 0x00000001
	WS_EX_APPWINDOW     = 0x00040000

	SWP_FRAMECHANGED  = 0x0020
	SWP_NOOWNERZORDER = 0x0200
	SWP_NOZORDER      = 0x0004
	SWP_NOMOVE        = 0x0002
	SWP_NOSIZE        = 0x0001
	SWP_NOACTIVATE    = 0x0010

	HWND_TOP       = 0
	HWND_BOTTOM    = 1
	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2

	SW_SHOW    = 5
	SW_HIDE    = 0
	SW_SHOWNA  = 8

	SWP_NOREDRAW = 0x0008

	SPI_GETWORKAREA = 0x0030

	SC_MINIMIZE = 0xF020

	WM_SIZE            = 0x0005
	WM_SYSCOMMAND      = 0x0112
	WM_POWERBROADCAST  = 0x0218
	WM_DISPLAYCHANGE   = 0x007E

	// WM_POWERBROADCAST wParam 值
	PBT_APMRESUMEAUTOMATIC = 0x0012
	PBT_APMRESUMESUSPEND   = 0x0007

	subclassID = 1
)

// WindowsAPI Windows API 封装
type WindowsAPI struct{}

// NewWindowsAPI 创建 WindowsAPI 实例
func NewWindowsAPI() *WindowsAPI {
	return &WindowsAPI{}
}

// GetScreenSize 获取屏幕分辨率
func (api *WindowsAPI) GetScreenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	return int(w), int(h)
}

// GetVirtualScreenSize 获取虚拟屏幕尺寸（多显示器）
func (api *WindowsAPI) GetVirtualScreenSize() (int, int) {
	const SM_CXVIRTUALSCREEN = 78
	const SM_CYVIRTUALSCREEN = 79
	w, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXVIRTUALSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYVIRTUALSCREEN))
	return int(w), int(h)
}

// GetWorkAreaRect 获取工作区矩形（排除任务栏）
func (api *WindowsAPI) GetWorkAreaRect() (left, top, right, bottom int) {
	type RECT struct {
		Left, Top, Right, Bottom int32
	}
	var rect RECT
	procSystemParametersInfoW.Call(
		uintptr(SPI_GETWORKAREA),
		0,
		uintptr(unsafe.Pointer(&rect)),
		0,
	)
	return int(rect.Left), int(rect.Top), int(rect.Right), int(rect.Bottom)
}

// FindProgman 查找 Progman 窗口句柄
func (api *WindowsAPI) FindProgman() win.HWND {
	progman, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)
	return win.HWND(progman)
}

// FindShellWorkerW 查找包含 SHELLDLL_DefView 的 WorkerW（桌面图标层）
// 发送 0x052C 后确保 WorkerW 存在
func (api *WindowsAPI) FindShellWorkerW() win.HWND {
	progman := api.FindProgman()
	logger.Debug("FindShellWorkerW: Progman=%v", progman)
	if progman == 0 {
		logger.Error("FindShellWorkerW: Progman not found")
		return 0
	}

	// 发送 0x052C 消息确保 WorkerW 存在（超时 500ms，避免长时间卡死）
	var result uintptr
	ret, _, err := procSendMessageW.Call(
		uintptr(progman),
		0x052C,
		0xD, 1,
		0, uintptr(500),
		uintptr(unsafe.Pointer(&result)),
	)
	logger.Debug("FindShellWorkerW: SendMessage 0x052C ret=%v, result=%v, err=%v", ret, result, err)

	// 找到包含 SHELLDLL_DefView 的顶层窗口
	var shellWorkerW win.HWND
	enumFunc := syscall.NewCallback(func(hwnd win.HWND, lParam uintptr) uintptr {
		shellView, _, _ := procFindWindowExW.Call(
			uintptr(hwnd),
			0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("SHELLDLL_DefView"))),
			0,
		)
		if shellView != 0 {
			shellWorkerW = hwnd
			logger.Debug("FindShellWorkerW: found SHELLDLL_DefView=%v in parent=%v", shellView, hwnd)
			return 0
		}
		return 1
	})

	procEnumWindows.Call(enumFunc, 0)
	logger.Debug("FindShellWorkerW: shellWorkerW=%v", shellWorkerW)
	return shellWorkerW
}

// SetAsDesktopChild 将窗口嵌入桌面层级
// 策略：SetParent 到包含 SHELLDLL_DefView 的 WorkerW 中，窗口置顶覆盖桌面图标
// 返回是否成功嵌入
func (api *WindowsAPI) SetAsDesktopChild(hwnd win.HWND) bool {
	// 记录嵌入前窗口状态
	visible := api.IsWindowVisible(hwnd)
	var rect [4]int32
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect[0])))
	logger.Debug("SetAsDesktopChild: before - hwnd=%v, visible=%v, rect=(%d,%d,%d,%d)",
		hwnd, visible, rect[0], rect[1], rect[2], rect[3])

	// 找到包含 SHELLDLL_DefView 的 WorkerW
	shellWorkerW := api.FindShellWorkerW()
	if shellWorkerW == 0 {
		logger.Error("SetAsDesktopChild: shell WorkerW not found")
		return false
	}

	// 检查目标 WorkerW 的可见性
	targetVisible := api.IsWindowVisible(shellWorkerW)
	logger.Debug("SetAsDesktopChild: shell WorkerW=%v, visible=%v", shellWorkerW, targetVisible)

	// 将窗口设为该 WorkerW 的子窗口（和 SHELLDLL_DefView 同级）
	logger.Debug("SetAsDesktopChild: SetParent hwnd=%v to WorkerW=%v", hwnd, shellWorkerW)
	prevParent, _, err := procSetParent.Call(uintptr(hwnd), uintptr(shellWorkerW))
	logger.Debug("SetAsDesktopChild: SetParent returned prevParent=%v, err=%v", prevParent, err)

	// 确保窗口可见并置于 WorkerW 子窗口的最顶层（覆盖 SHELLDLL_DefView）
	procShowWindow.Call(uintptr(hwnd), uintptr(SW_SHOW))
	ret, _, _ := procSetWindowPos.Call(
		uintptr(hwnd), HWND_TOP,
		0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE),
	)
	logger.Debug("SetAsDesktopChild: SetWindowPos HWND_TOP ret=%v", ret)

	// 记录嵌入后窗口状态
	visible2 := api.IsWindowVisible(hwnd)
	var rect2 [4]int32
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect2[0])))
	style, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE))
	exStyle, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_EXSTYLE))
	logger.Debug("SetAsDesktopChild: after - visible=%v, rect=(%d,%d,%d,%d), style=0x%X, exStyle=0x%X",
		visible2, rect2[0], rect2[1], rect2[2], rect2[3], style, exStyle)

	logger.Debug("SetAsDesktopChild: done")
	return true
}

// logChildWindows 枚举并日志输出某窗口的子窗口
func (api *WindowsAPI) logChildWindows(parent win.HWND, parentName string) {
	enumChildFunc := syscall.NewCallback(func(hwnd win.HWND, lParam uintptr) uintptr {
		visible := api.IsWindowVisible(hwnd)
		var className [256]uint16
		procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&className[0])), 256)
		name := syscall.UTF16ToString(className[:])
		logger.Debug("  child of %s: hwnd=%v, class=%q, visible=%v", parentName, hwnd, name, visible)
		return 1
	})
	procEnumChildWindows.Call(uintptr(parent), enumChildFunc, 0)
}

// DetachFromDesktop 将窗口从桌面层脱离（恢复为顶级窗口）
func (api *WindowsAPI) DetachFromDesktop(hwnd win.HWND) {
	procSetParent.Call(uintptr(hwnd), 0)
}

// HideDesktopIcons 隐藏系统桌面图标（隐藏包含 SHELLDLL_DefView 的父窗口）
func (api *WindowsAPI) HideDesktopIcons() {
	var targetHwnd win.HWND
	enumFunc := syscall.NewCallback(func(hwnd win.HWND, lParam uintptr) uintptr {
		shellView, _, _ := procFindWindowExW.Call(
			uintptr(hwnd),
			0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("SHELLDLL_DefView"))),
			0,
		)
		if shellView != 0 {
			targetHwnd = hwnd
			return 0 // 停止枚举
		}
		return 1
	})
	procEnumWindows.Call(enumFunc, 0)

	if targetHwnd != 0 {
		procShowWindow.Call(uintptr(targetHwnd), uintptr(SW_HIDE))
	}
}

// ShowDesktopIcons 显示系统桌面图标
func (api *WindowsAPI) ShowDesktopIcons() {
	var targetHwnd win.HWND
	enumFunc := syscall.NewCallback(func(hwnd win.HWND, lParam uintptr) uintptr {
		shellView, _, _ := procFindWindowExW.Call(
			uintptr(hwnd),
			0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("SHELLDLL_DefView"))),
			0,
		)
		if shellView != 0 {
			targetHwnd = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(enumFunc, 0)

	if targetHwnd != 0 {
		procShowWindow.Call(uintptr(targetHwnd), uintptr(SW_SHOW))
	}
}

// SetWindowBorderless 移除窗口边框（标题栏等），确保无白边
func (api *WindowsAPI) SetWindowBorderless(hwnd win.HWND) {
	// 移除普通样式中的边框（包括 WS_BORDER 确保无白边）
	style, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE))
	newStyle := style &^ uintptr(WS_OVERLAPPEDWINDOW|WS_THICKFRAME|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX|WS_MAXIMIZEBOX|WS_BORDER)
	newStyle |= uintptr(WS_POPUP | WS_VISIBLE)
	procSetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE), newStyle)

	// 移除扩展样式中的边缘效果（消除白边）
	exStyle, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_EXSTYLE))
	newExStyle := exStyle &^ uintptr(WS_EX_CLIENTEDGE|WS_EX_WINDOWEDGE|WS_EX_STATICEDGE|WS_EX_DLGMODALFRAME|WS_EX_APPWINDOW)
	newExStyle |= uintptr(WS_EX_TOOLWINDOW)
	procSetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_EXSTYLE), newExStyle)

	// 应用样式变更
	procSetWindowPos.Call(
		uintptr(hwnd), HWND_TOP,
		0, 0, 0, 0,
		uintptr(SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOOWNERZORDER),
	)
}

// SetBorderlessAndPosition 一次性设置无边框 + 定位（减少闪烁）
func (api *WindowsAPI) SetBorderlessAndPosition(hwnd win.HWND, x, y, w, h int) {
	// 移除普通样式中的边框（包括 WS_BORDER 确保无白边）
	style, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE))
	newStyle := style &^ uintptr(WS_OVERLAPPEDWINDOW|WS_THICKFRAME|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX|WS_MAXIMIZEBOX|WS_BORDER)
	newStyle |= uintptr(WS_POPUP | WS_VISIBLE)
	procSetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE), newStyle)

	// 移除扩展样式中的边缘效果
	exStyle, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_EXSTYLE))
	newExStyle := exStyle &^ uintptr(WS_EX_CLIENTEDGE|WS_EX_WINDOWEDGE|WS_EX_STATICEDGE|WS_EX_DLGMODALFRAME|WS_EX_APPWINDOW)
	newExStyle |= uintptr(WS_EX_TOOLWINDOW)
	procSetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_EXSTYLE), newExStyle)

	// 样式变更 + 定位 一次性完成，使用 HWND_BOTTOM 沉底
	procSetWindowPos.Call(
		uintptr(hwnd), uintptr(HWND_BOTTOM),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(SWP_FRAMECHANGED|SWP_NOOWNERZORDER|SWP_NOACTIVATE),
	)
}

// SetWindowBottom 将窗口设置到 Z 序最底层（在所有程序下方，桌面上方）
func (api *WindowsAPI) SetWindowBottom(hwnd win.HWND) {
	procSetWindowPos.Call(
		uintptr(hwnd), uintptr(HWND_BOTTOM),
		0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE),
	)
}

// IsForegroundWindow 判断指定窗口是否为当前前台窗口
func (api *WindowsAPI) IsForegroundWindow(hwnd win.HWND) bool {
	fg, _, _ := procGetForegroundWindow.Call()
	return win.HWND(fg) == hwnd
}

// IsIconic 判断窗口是否处于最小化状态
func (api *WindowsAPI) IsIconic(hwnd win.HWND) bool {
	ret, _, _ := procIsIconic.Call(uintptr(hwnd))
	return ret != 0
}

// IsWindowVisible 判断窗口是否可见
func (api *WindowsAPI) IsWindowVisible(hwnd win.HWND) bool {
	ret, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return ret != 0
}

// RemoveWindowMenu 移除窗口菜单栏（消除顶部空白区域）
func (api *WindowsAPI) RemoveWindowMenu(hwnd win.HWND) {
	// 设置菜单为空（NULL），移除菜单栏占用的空间
	procSetMenu.Call(uintptr(hwnd), 0)
	// 通知窗口重绘菜单区域
	procDrawMenuBar.Call(uintptr(hwnd))
}

// DisableMinimize 禁用窗口最小化（移除 WS_MINIMIZEBOX 样式）
func (api *WindowsAPI) DisableMinimize(hwnd win.HWND) {
	style, _, _ := procGetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE))
	newStyle := style &^ uintptr(WS_MINIMIZEBOX)
	procSetWindowLongW.Call(uintptr(hwnd), negIntToUintptr(GWL_STYLE), newStyle)
}

// ShowWindowCmd 显示/隐藏窗口
func (api *WindowsAPI) ShowWindowCmd(hwnd win.HWND, cmd int) {
	procShowWindow.Call(uintptr(hwnd), uintptr(cmd))
}

// MoveWindow 移动/调整窗口
func (api *WindowsAPI) MoveWindow(hwnd win.HWND, x, y, w, h int) {
	procMoveWindow.Call(
		uintptr(hwnd),
		uintptr(x), uintptr(y),
		uintptr(w), uintptr(h),
		1, // repaint
	)
}

// SendWMSize 发送 WM_SIZE 消息触发窗口重新计算客户区布局
func (api *WindowsAPI) SendWMSize(hwnd win.HWND) {
	// 获取当前客户区尺寸
	var rect struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	w := rect.Right - rect.Left
	h := rect.Bottom - rect.Top
	// 发送 WM_SIZE, wParam=0 (SIZE_RESTORED), lParam=MAKELPARAM(w, h)
	lParam := uintptr(uint32(w) | uint32(h)<<16)
	procSendMessageW2.Call(uintptr(hwnd), WM_SIZE, 0, lParam)
}

// UpdateWindow 强制窗口立即重绘（发送 WM_PAINT）
func (api *WindowsAPI) UpdateWindow(hwnd win.HWND) {
	procUpdateWindow.Call(uintptr(hwnd))
}

// InvalidateRect 使窗口整个客户区无效化，触发重绘
func (api *WindowsAPI) InvalidateRect(hwnd win.HWND) {
	procInvalidateRect.Call(uintptr(hwnd), 0, 1)
}

// SetWindowFullScreen 设置窗口全屏（合并操作减少闪烁）
func (api *WindowsAPI) SetWindowFullScreen(hwnd win.HWND, x, y, w, h int) {
	api.SetBorderlessAndPosition(hwnd, x, y, w, h)
}

// SetWindowPosNoRedraw 设置窗口位置/大小但不触发重绘（减少闪烁）
func (api *WindowsAPI) SetWindowPosNoRedraw(hwnd win.HWND, x, y, w, h int) {
	procSetWindowPos.Call(
		uintptr(hwnd), 0,
		uintptr(x), uintptr(y),
		uintptr(w), uintptr(h),
		uintptr(SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOREDRAW),
	)
}

// HideTaskbar 隐藏任务栏
func (api *WindowsAPI) HideTaskbar() {
	taskbar, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Shell_TrayWnd"))),
		0,
	)
	if taskbar != 0 {
		procShowWindow.Call(taskbar, SW_HIDE)
	}
}

// ShowTaskbar 显示任务栏
func (api *WindowsAPI) ShowTaskbar() {
	taskbar, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Shell_TrayWnd"))),
		0,
	)
	if taskbar != 0 {
		procShowWindow.Call(taskbar, SW_SHOW)
	}
}

// ForceShowAndRaise 强制显示并置顶
func (api *WindowsAPI) ForceShowAndRaise(hwnd win.HWND) {
	procShowWindow.Call(uintptr(hwnd), SW_SHOW)
	procSetWindowPos.Call(
		uintptr(hwnd), HWND_TOPMOST,
		0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE),
	)
	// 取消置顶但保持在最前
	procSetWindowPos.Call(
		uintptr(hwnd), HWND_NOTOPMOST,
		0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE),
	)
}

// negIntToUintptr 将负整数常量安全转换为 uintptr
func negIntToUintptr(v int) uintptr {
	return uintptr(v)
}

// 全局系统事件回调（电源恢复、显示变更等）
var (
	systemEventCallback func()
	subclassHwnd        win.HWND
)

// SetOnSystemEvent 设置系统事件回调（电源恢复、显示变更等触发）
func (api *WindowsAPI) SetOnSystemEvent(fn func()) {
	systemEventCallback = fn
}

// subclassProc 子类化回调：拦截 WM_SYSCOMMAND SC_MINIMIZE + 监听电源/显示事件
func subclassProc(hwnd uintptr, msg uint32, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
	switch msg {
	case WM_SYSCOMMAND:
		if (wParam & 0xFFF0) == SC_MINIMIZE {
			return 0
		}
	case WM_POWERBROADCAST:
		if wParam == PBT_APMRESUMEAUTOMATIC || wParam == PBT_APMRESUMESUSPEND {
			// 系统从睡眠/休眠恢复，触发刷新回调
			logger.Debug("subclassProc: power resume event (wParam=0x%X)", wParam)
			if systemEventCallback != nil {
				systemEventCallback()
			}
		}
	case WM_DISPLAYCHANGE:
		// 显示分辨率/状态变更，触发刷新回调
		logger.Debug("subclassProc: display change event (new size=%dx%d)", int(lParam&0xFFFF), int((lParam>>16)&0xFFFF))
		if systemEventCallback != nil {
			systemEventCallback()
		}
	}
	ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

var subclassCB = syscall.NewCallback(subclassProc)

// InstallMinimizeBlock 安装子类化拦截最小化消息（仅影响本窗口）
func (api *WindowsAPI) InstallMinimizeBlock(hwnd win.HWND) {
	subclassHwnd = hwnd
	procSetWindowSubclass.Call(
		uintptr(hwnd),
		subclassCB,
		uintptr(subclassID),
		0,
	)
}

// RemoveMinimizeBlock 移除子类化
func (api *WindowsAPI) RemoveMinimizeBlock(hwnd win.HWND) {
	procRemoveWindowSubclass.Call(
		uintptr(hwnd),
		subclassCB,
		uintptr(subclassID),
	)
}

// --- WinEvent Hook：监听窗口最小化/隐藏事件，立即恢复 ---

const (
	EVENT_SYSTEM_MINIMIZESTART = 0x0016
	EVENT_OBJECT_HIDE          = 0x8003
	WINEVENT_OUTOFCONTEXT      = 0x0000
)

var (
	procSetWinEventHook   = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent    = user32.NewProc("UnhookWinEvent")
)

var (
	winEventHookHandle  uintptr
	winEventHookHandle2 uintptr
	watchedHwnd         win.HWND
)

// winEventProc 事件钩子回调：检测到本窗口被最小化时立即恢复
func winEventProc(hWinEventHook, event, hwnd, idObject, idChild, dwEventThread, dwmsEventTime uintptr) uintptr {
	if win.HWND(hwnd) != watchedHwnd {
		return 0
	}
	if event == EVENT_SYSTEM_MINIMIZESTART {
		// 窗口被最小化，立即恢复并沉底
		procShowWindow.Call(hwnd, 9) // SW_RESTORE
		procSetWindowPos.Call(
			hwnd, uintptr(HWND_BOTTOM),
			0, 0, 0, 0,
			uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE),
		)
	} else if event == EVENT_OBJECT_HIDE {
		// 窗口被隐藏，立即恢复并沉底
		procShowWindow.Call(hwnd, uintptr(SW_SHOWNA))
		procSetWindowPos.Call(
			hwnd, uintptr(HWND_BOTTOM),
			0, 0, 0, 0,
			uintptr(SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE),
		)
	}
	return 0
}

var winEventCB = syscall.NewCallback(winEventProc)

// InstallWinEventHook 安装事件钩子监听本窗口的最小化/隐藏事件
func (api *WindowsAPI) InstallWinEventHook(hwnd win.HWND) {
	watchedHwnd = hwnd

	// 监听最小化事件
	h1, _, _ := procSetWinEventHook.Call(
		uintptr(EVENT_SYSTEM_MINIMIZESTART), // eventMin
		uintptr(EVENT_SYSTEM_MINIMIZESTART), // eventMax
		0,                            // hmodWinEventProc (0 = 当前进程)
		winEventCB,                   // pfnWinEventProc
		0,                            // idProcess (0 = 所有进程)
		0,                            // idThread (0 = 所有线程)
		uintptr(WINEVENT_OUTOFCONTEXT), // dwFlags
	)
	winEventHookHandle = h1

	// 监听隐藏事件
	h2, _, _ := procSetWinEventHook.Call(
		uintptr(EVENT_OBJECT_HIDE),
		uintptr(EVENT_OBJECT_HIDE),
		0,
		winEventCB,
		0,
		0,
		uintptr(WINEVENT_OUTOFCONTEXT),
	)
	winEventHookHandle2 = h2
}

// RemoveWinEventHook 移除事件钩子
func (api *WindowsAPI) RemoveWinEventHook() {
	watchedHwnd = 0
	if winEventHookHandle != 0 {
		procUnhookWinEvent.Call(winEventHookHandle)
		winEventHookHandle = 0
	}
	if winEventHookHandle2 != 0 {
		procUnhookWinEvent.Call(winEventHookHandle2)
		winEventHookHandle2 = 0
	}
}
