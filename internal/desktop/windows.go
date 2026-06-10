package desktop

import (
	"syscall"
	"unsafe"

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
	procSendMessageW     = user32.NewProc("SendMessageTimeout")
	procFindWindowExW    = user32.NewProc("FindWindowExW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procSetMenu          = user32.NewProc("SetMenu")
	procDrawMenuBar      = user32.NewProc("DrawMenuBar")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procSendMessageW2    = user32.NewProc("SendMessageW")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32.NewProc("DefSubclassProc")
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

	WM_SIZE        = 0x0005
	WM_SYSCOMMAND  = 0x0112

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

// FindWorkerW 查找桌面 WorkerW 窗口句柄
func (api *WindowsAPI) FindWorkerW() win.HWND {
	progman, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Progman"))),
		0,
	)
	if progman == 0 {
		return 0
	}

	// 发送 0x052C 消息让 Progman 生成 WorkerW
	procSendMessageW.Call(
		progman,
		0x052C, 0, 0,
		uintptr(1000), // timeout
		0,
	)

	var workerW win.HWND
	enumFunc := syscall.NewCallback(func(hwnd win.HWND, lParam uintptr) uintptr {
		shellView, _, _ := procFindWindowExW.Call(
			uintptr(hwnd),
			0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("SHELLDLL_DefView"))),
			0,
		)
		if shellView != 0 {
			// 找到 SHELLDLL_DefView 的下一个 WorkerW
			next, _, _ := procFindWindowExW.Call(0, uintptr(hwnd), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("WorkerW"))), 0)
			if next != 0 {
				workerW = win.HWND(next)
			}
		}
		return 1 // 继续枚举
	})

	procEnumWindows.Call(enumFunc, 0)
	return workerW
}

// SetAsDesktopChild 将窗口设为桌面 WorkerW 子窗口（使窗口嵌入桌面层，不受 Win+D 影响）
func (api *WindowsAPI) SetAsDesktopChild(hwnd win.HWND) {
	workerW := api.FindWorkerW()
	if workerW != 0 {
		procSetParent.Call(uintptr(hwnd), uintptr(workerW))
	}
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

// subclassProc 子类化回调：拦截 WM_SYSCOMMAND SC_MINIMIZE，忽略最小化请求
func subclassProc(hwnd uintptr, msg uint32, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
	if msg == WM_SYSCOMMAND && (wParam&0xFFF0) == SC_MINIMIZE {
		// 吞掉最小化消息，不传递给原 WndProc
		return 0
	}
	ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

var subclassCB = syscall.NewCallback(subclassProc)

// InstallMinimizeBlock 安装子类化拦截最小化消息（仅影响本窗口）
func (api *WindowsAPI) InstallMinimizeBlock(hwnd win.HWND) {
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
