package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// Win32 函数声明
var (
	user32OutlineDLL        = syscall.NewLazyDLL("user32.dll")
	gdi32OutlineDLL         = syscall.NewLazyDLL("gdi32.dll")
	procCreateWindowExW2    = user32OutlineDLL.NewProc("CreateWindowExW")
	procDestroyWindow2      = user32OutlineDLL.NewProc("DestroyWindow")
	procSetWindowRgn        = user32OutlineDLL.NewProc("SetWindowRgn")
	procCreateRectRgn       = gdi32OutlineDLL.NewProc("CreateRectRgn")
	procCombineRgn          = gdi32OutlineDLL.NewProc("CombineRgn")
	procDeleteObject2       = gdi32OutlineDLL.NewProc("DeleteObject")
)

const (
	outlineBorderWidth = 2
	_RGN_DIFF          = 4
)

// outlineWndProc 缩放边框窗口的窗口过程
func outlineWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// ensureOutlineWindow 确保边框弹出窗口已创建
func (s *ResizeOutlineState) ensureOutlineWindow() {
	if s.OutlineHwnd != 0 {
		return
	}
	hInst := win.GetModuleHandle(nil)
	className := syscall.StringToUTF16Ptr("GroupResizeOutline")

	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(outlineWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(0)) // WHITE_BRUSH，边框显示为白色
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)

	// WS_EX_TRANSPARENT：鼠标穿透，不拦截点击
	// WS_EX_TOPMOST：保持在所有窗口之上
	// WS_EX_TOOLWINDOW：不在任务栏显示
	exStyle := uint32(win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)

	hwnd, _, _ := procCreateWindowExW2.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(style),
		0, 0, 0, 0,
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	s.OutlineHwnd = win.HWND(hwnd)
}

// setOutlineFrameRegion 设置边框窗口的显示区域为 2px 宽的边框（中间挖空透明）
func (s *ResizeOutlineState) setOutlineFrameRegion(w, h int) {
	if s.OutlineHwnd == 0 || w <= outlineBorderWidth*2 || h <= outlineBorderWidth*2 {
		return
	}
	b := outlineBorderWidth
	// 外矩形：整个窗口
	outerRgn, _, _ := procCreateRectRgn.Call(uintptr(0), uintptr(0), uintptr(w), uintptr(h))
	// 内矩形：挖掉中间
	innerRgn, _, _ := procCreateRectRgn.Call(uintptr(b), uintptr(b), uintptr(w-b), uintptr(h-b))
	// 组合：外 - 内 = 边框
	procCombineRgn.Call(outerRgn, outerRgn, innerRgn, uintptr(_RGN_DIFF))
	procDeleteObject2.Call(innerRgn)
	// SetWindowRgn 取得区域所有权，不需要 delete outerRgn
	procSetWindowRgn.Call(uintptr(s.OutlineHwnd), outerRgn, uintptr(1))
}

// showOutline 显示并定位边框窗口
func (s *ResizeOutlineState) showOutline(x, y, w, h int) {
	if s.OutlineHwnd == 0 {
		return
	}
	screenX := x + s.resizeWorkX()
	screenY := y + s.resizeWorkY()
	// 窗口比实际边框大 outlineBorderWidth*2，因为区域把中间挖掉了
	win.SetWindowPos(s.OutlineHwnd, win.HWND_TOPMOST,
		int32(screenX-outlineBorderWidth), int32(screenY-outlineBorderWidth),
		int32(w+outlineBorderWidth*2), int32(h+outlineBorderWidth*2),
		win.SWP_NOACTIVATE)
	s.setOutlineFrameRegion(w+outlineBorderWidth*2, h+outlineBorderWidth*2)
	win.ShowWindow(s.OutlineHwnd, win.SW_SHOWNA)
}

// hideOutline 隐藏边框窗口
func (s *ResizeOutlineState) hideOutline() {
	if s.OutlineHwnd != 0 {
		win.ShowWindow(s.OutlineHwnd, win.SW_HIDE)
	}
}

// destroyOutlineWindow 销毁边框窗口
func (s *ResizeOutlineState) destroyOutlineWindow() {
	if s.OutlineHwnd != 0 {
		procDestroyWindow2.Call(uintptr(s.OutlineHwnd))
		s.OutlineHwnd = 0
	}
}

// OnCardResizeOutline 卡片缩放虚框更新 — 使用弹出窗口替代 XOR 屏幕绘制
func (s *ResizeOutlineState) OnCardResizeOutline(card *ui.GroupCard, newX, newY, newW, newH int) {
	if s.ResizeOutlineCard == nil {
		// 第一次调用时确保窗口已创建
		s.ensureOutlineWindow()
	}
	s.ResizeOutlineCard = card
	s.ResizeOutlineX = newX
	s.ResizeOutlineY = newY
	s.ResizeOutlineW = newW
	s.ResizeOutlineH = newH
	s.showOutline(newX, newY, newW, newH)
}

// OnCardResizeOutlineEnd 卡片缩放虚框结束 — 隐藏弹出窗口
func (s *ResizeOutlineState) OnCardResizeOutlineEnd(card *ui.GroupCard) {
	s.hideOutline()
	s.ResizeOutlineCard = nil
}

// DrawResizeOutlineWin32 保留空函数（不再使用 XOR 方式）
func (s *ResizeOutlineState) DrawResizeOutlineWin32(x, y, w, h int) {
	// 已废弃 — 改用弹出窗口方式
}
