package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

const (
	guideLineWidth = 1
)

var (
	guideGDIDLL   = syscall.NewLazyDLL("gdi32.dll")
	procCreatePen = guideGDIDLL.NewProc("CreatePen")
	procSelectObj = guideGDIDLL.NewProc("SelectObject")
	procMoveToEx  = guideGDIDLL.NewProc("MoveToEx")
	procLineTo    = guideGDIDLL.NewProc("LineTo")
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

	minX, minY, maxX, maxY := calcGuideBounds(hLines, vLines)
	w := maxX - minX
	h := maxY - minY
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	win.SetWindowPos(s.guideHwnd, win.HWND_TOPMOST,
		int32(s.workX+minX), int32(s.workY+minY),
		int32(w), int32(h),
		win.SWP_NOACTIVATE)

	halfW := guideLineWidth / 2
	combinedRgn := uintptr(0)
	first := true

	for _, lineY := range hLines {
		ly := lineY - minY
		rgn, _, _ := procCreateRectRgn_.Call(uintptr(0), uintptr(ly-halfW), uintptr(w), uintptr(ly+halfW+1))
		if rgn == 0 {
			continue
		}
		if first {
			combinedRgn = rgn
			first = false
		} else {
			tmpRgn, _, _ := procCreateRectRgn_.Call(0, 0, 0, 0)
			procCombineRgn_.Call(tmpRgn, combinedRgn, rgn, uintptr(2))
			procDeleteObject_.Call(combinedRgn)
			procDeleteObject_.Call(rgn)
			combinedRgn = tmpRgn
		}
	}

	for _, lineX := range vLines {
		lx := lineX - minX
		rgn, _, _ := procCreateRectRgn_.Call(uintptr(lx-halfW), uintptr(0), uintptr(lx+halfW+1), uintptr(h))
		if rgn == 0 {
			continue
		}
		if first {
			combinedRgn = rgn
			first = false
		} else {
			tmpRgn, _, _ := procCreateRectRgn_.Call(0, 0, 0, 0)
			procCombineRgn_.Call(tmpRgn, combinedRgn, rgn, uintptr(2))
			procDeleteObject_.Call(combinedRgn)
			procDeleteObject_.Call(rgn)
			combinedRgn = tmpRgn
		}
	}

	if first {
		win.ShowWindow(s.guideHwnd, win.SW_HIDE)
		return
	}

	procSetWindowRgn_.Call(uintptr(s.guideHwnd), combinedRgn, uintptr(1))

	hdcWnd := win.GetDC(s.guideHwnd)
	if hdcWnd != 0 {
	pen, _, _ := procCreatePen.Call(uintptr(win.PS_SOLID), uintptr(guideLineWidth),
		uintptr(win.RGB(s.guideColorR, s.guideColorG, s.guideColorB)))
		if pen != 0 {
			oldPen, _, _ := procSelectObj.Call(uintptr(hdcWnd), pen)
			for _, lineY := range hLines {
				ly := lineY - minY
				procMoveToEx.Call(uintptr(hdcWnd), uintptr(0), uintptr(ly), 0)
				procLineTo.Call(uintptr(hdcWnd), uintptr(w), uintptr(ly))
			}
			for _, lineX := range vLines {
				lx := lineX - minX
				procMoveToEx.Call(uintptr(hdcWnd), uintptr(lx), uintptr(0), 0)
				procLineTo.Call(uintptr(hdcWnd), uintptr(lx), uintptr(h))
			}
			if oldPen != 0 {
				procSelectObj.Call(uintptr(hdcWnd), oldPen)
			}
			procDeleteObject_.Call(pen)
		}
		win.ReleaseDC(s.guideHwnd, hdcWnd)
	}

	win.ShowWindow(s.guideHwnd, win.SW_SHOWNA)
}

func calcGuideBounds(hLines, vLines []int) (minX, minY, maxX, maxY int) {
	minX = 99999
	minY = 99999
	maxX = -1
	maxY = -1

	for _, y := range hLines {
		y1 := y - guideLineWidth
		y2 := y + guideLineWidth
		if y1 < minY {
			minY = y1
		}
		if y2 > maxY {
			maxY = y2
		}
	}

	for _, x := range vLines {
		x1 := x - guideLineWidth
		x2 := x + guideLineWidth
		if x1 < minX {
			minX = x1
		}
		if x2 > maxX {
			maxX = x2
		}
	}

	if len(hLines) == 0 {
		minY = 0
		maxY = 2000
	}
	if len(vLines) == 0 {
		minX = 0
		maxX = 4000
	}

	if minX > 4 {
		minX -= 4
	} else {
		minX = 0
	}
	if minY > 4 {
		minY -= 4
	} else {
		minY = 0
	}
	maxX += 4
	maxY += 4
	return
}

func (s *CardDragOutline) destroyGuideWindow() {
	if s.guideHwnd != 0 {
		procDestroyWindow_.Call(uintptr(s.guideHwnd))
		s.guideHwnd = 0
	}
}
