package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

// Win32 函数声明（与 resize_outline_methods.go 共享）
var (
	gdi32DllOutline         = syscall.NewLazyDLL("gdi32.dll")
	user32DllOutline        = syscall.NewLazyDLL("user32.dll")
	procCreateWindowExW_    = user32DllOutline.NewProc("CreateWindowExW")
	procDestroyWindow_      = user32DllOutline.NewProc("DestroyWindow")
	procSetWindowRgn_       = user32DllOutline.NewProc("SetWindowRgn")
	procCreateRectRgn_      = gdi32DllOutline.NewProc("CreateRectRgn")
	procCombineRgn_         = gdi32DllOutline.NewProc("CombineRgn")
	procDeleteObject_       = gdi32DllOutline.NewProc("DeleteObject")
)

const (
	frameBorderWidth = 2
	_RGN_DIFF_       = 4
)

// frameWndProc 边框窗口的窗口过程
func frameWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// popupFrameOverlay 管理一个弹出式边框窗口（2px 白色边框，中间透明）
type popupFrameOverlay struct {
	hwnd win.HWND
}

// ensureCreated 确保边框窗口已创建
func (f *popupFrameOverlay) ensureCreated() {
	if f.hwnd != 0 {
		return
	}
	hInst := win.GetModuleHandle(nil)
	className := syscall.StringToUTF16Ptr("DesktopGoOutline")

	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(frameWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(0)) // WHITE_BRUSH
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)

	exStyle := uint32(win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)

	hwnd, _, _ := procCreateWindowExW_.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(style),
		0, 0, 0, 0,
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	f.hwnd = win.HWND(hwnd)
}

// setFrameRegion 设置窗口显示区域为 2px 宽的边框（中间挖空透明）
func (f *popupFrameOverlay) setFrameRegion(w, h int) {
	if f.hwnd == 0 || w <= frameBorderWidth*2 || h <= frameBorderWidth*2 {
		return
	}
	b := frameBorderWidth
	outerRgn, _, _ := procCreateRectRgn_.Call(uintptr(0), uintptr(0), uintptr(w), uintptr(h))
	innerRgn, _, _ := procCreateRectRgn_.Call(uintptr(b), uintptr(b), uintptr(w-b), uintptr(h-b))
	procCombineRgn_.Call(outerRgn, outerRgn, innerRgn, uintptr(_RGN_DIFF_))
	procDeleteObject_.Call(innerRgn)
	procSetWindowRgn_.Call(uintptr(f.hwnd), outerRgn, uintptr(1))
}

// showAt 在屏幕坐标 (screenX, screenY) 处显示尺寸为 w×h 的边框
func (f *popupFrameOverlay) showAt(screenX, screenY, w, h int) {
	if f.hwnd == 0 {
		return
	}
	win.SetWindowPos(f.hwnd, win.HWND_TOPMOST,
		int32(screenX-frameBorderWidth), int32(screenY-frameBorderWidth),
		int32(w+frameBorderWidth*2), int32(h+frameBorderWidth*2),
		win.SWP_NOACTIVATE)
	f.setFrameRegion(w + frameBorderWidth*2, h + frameBorderWidth*2)
	win.ShowWindow(f.hwnd, win.SW_SHOWNA)
}

// hide 隐藏边框
func (f *popupFrameOverlay) hide() {
	if f.hwnd != 0 {
		win.ShowWindow(f.hwnd, win.SW_HIDE)
	}
}

// destroy 销毁边框窗口
func (f *popupFrameOverlay) destroy() {
	if f.hwnd != 0 {
		procDestroyWindow_.Call(uintptr(f.hwnd))
		f.hwnd = 0
	}
}
