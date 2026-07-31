package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/logger"
)

const (
	guideLineWidth = 2 // 参考线宽度（px）
)

// 参考线窗口用 WS_EX_LAYERED + UpdateLayeredWindow 绘制，
// 这样 GDI 内容能可靠显示（与卡片拖拽幽灵窗口同款技术）。
var (
	guideDLLUser32 = syscall.NewLazyDLL("user32.dll")
	guideDLLGdi32  = syscall.NewLazyDLL("gdi32.dll")

	guideCreateWindowEx  = guideDLLUser32.NewProc("CreateWindowExW")
	guideDestroyWindow   = guideDLLUser32.NewProc("DestroyWindow")
	guideUpdateLayered   = guideDLLUser32.NewProc("UpdateLayeredWindow")
	guideCreateCompatibleDC = guideDLLGdi32.NewProc("CreateCompatibleDC")
	guideDeleteDC         = guideDLLGdi32.NewProc("DeleteDC")
	guideCreateDIBSection = guideDLLGdi32.NewProc("CreateDIBSection")
	guideSelectObj        = guideDLLGdi32.NewProc("SelectObject")
	guideDeleteObject     = guideDLLGdi32.NewProc("DeleteObject")
	guideBitBlt           = guideDLLGdi32.NewProc("BitBlt")
	guideCreatePen        = guideDLLGdi32.NewProc("CreatePen")
	guideMoveToEx         = guideDLLGdi32.NewProc("MoveToEx")
	guideLineTo           = guideDLLGdi32.NewProc("LineTo")
)

func guideWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func (s *CardDragOutline) ensureGuideWindow() {
	if s.guideHwnd != 0 {
		return
	}
	hInst := win.GetModuleHandle(nil)
	className := syscall.StringToUTF16Ptr("DesktopGoGuideLine")
	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(guideWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(5))
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)

	// WS_EX_LAYERED: 用 UpdateLayeredWindow 可靠绘制
	// WS_EX_TRANSPARENT: 点击穿透
	exStyle := uint32(win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)
	hwnd, _, _ := guideCreateWindowEx.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(style),
		0, 0, 0, 0,
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	s.guideHwnd = win.HWND(hwnd)
}

func (s *CardDragOutline) updateGuideWindow(hLines, vLines []int) {
	if s.guideHwnd == 0 {
		s.ensureGuideWindow()
	}
	if s.guideHwnd == 0 {
		return
	}
	if len(hLines) == 0 && len(vLines) == 0 {
		win.ShowWindow(s.guideHwnd, win.SW_HIDE)
		return
	}

	logger.Debug("updateGuideWindow: hLines=%v vLines=%v workArea=%dx%d color=RGB(%d,%d,%d)",
		hLines, vLines, s.workW, s.workH, s.guideColorR, s.guideColorG, s.guideColorB)

	w := s.workW
	h := s.workH
	if w < 1 {
		w = 4000
	}
	if h < 1 {
		h = 2000
	}

	// 创建 32-bit BGRA DIB 位图，全透明背景
	hdcScreen := win.GetDC(0)
	if hdcScreen == 0 {
		return
	}
	defer win.ReleaseDC(0, hdcScreen)

	hdcMem, _, _ := guideCreateCompatibleDC.Call(uintptr(hdcScreen))
	if hdcMem == 0 {
		return
	}
	defer guideDeleteDC.Call(hdcMem)

	var bi win.BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	hBmp, _, _ := guideCreateDIBSection.Call(hdcMem, uintptr(unsafe.Pointer(&bi.BmiHeader)),
		uintptr(win.DIB_RGB_COLORS), uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hBmp == 0 {
		return
	}
	defer guideDeleteObject.Call(hBmp)

	hOld, _, _ := guideSelectObj.Call(hdcMem, hBmp)
	if hOld == 0 {
		return
	}
	defer guideSelectObj.Call(hdcMem, hOld)

	// 初始化为全透明（BGRA，alpha=0）
	pixels := (*[1 << 24]byte)(bits)
	for i := 0; i < w*h*4; i += 4 {
		pixels[i+3] = 0
	}

	// 手动绘制参考线像素（BGRA 格式，alpha=255 不透明）
	// 注意：DIB 是 BGRA 自顶向下，像素排列为 [b,g,r,a]
	halfW := guideLineWidth / 2
	for _, lineY := range hLines {
		for y := lineY - halfW; y <= lineY+halfW; y++ {
			if y < 0 || y >= h {
				continue
			}
			for x := 0; x < w; x++ {
				idx := (y*w + x) * 4
				pixels[idx+0] = s.guideColorB
				pixels[idx+1] = s.guideColorG
				pixels[idx+2] = s.guideColorR
				pixels[idx+3] = 255
			}
		}
	}
	for _, lineX := range vLines {
		for x := lineX - halfW; x <= lineX+halfW; x++ {
			if x < 0 || x >= w {
				continue
			}
			for y := 0; y < h; y++ {
				idx := (y*w + x) * 4
				pixels[idx+0] = s.guideColorB
				pixels[idx+1] = s.guideColorG
				pixels[idx+2] = s.guideColorR
				pixels[idx+3] = 255
			}
		}
	}

	// UpdateLayeredWindow 显示
	var pt win.POINT
	var size win.SIZE
	size.CX = int32(w)
	size.CY = int32(h)
	var blend win.BLENDFUNCTION
	blend.BlendOp = 0
	blend.BlendFlags = 0
	blend.SourceConstantAlpha = 255
	blend.AlphaFormat = win.AC_SRC_ALPHA

	guideUpdateLayered.Call(
		uintptr(s.guideHwnd),
		uintptr(hdcScreen),
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(hdcMem),
		uintptr(unsafe.Pointer(&pt)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		uintptr(2)) // ULW_ALPHA

	win.SetWindowPos(s.guideHwnd, win.HWND_TOPMOST,
		int32(s.workX), int32(s.workY),
		int32(w), int32(h),
		win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)

	win.ShowWindow(s.guideHwnd, win.SW_SHOWNA)
}

func (s *CardDragOutline) destroyGuideWindow() {
	if s.guideHwnd != 0 {
		guideDestroyWindow.Call(uintptr(s.guideHwnd))
		s.guideHwnd = 0
	}
}
