package desktop

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// Win32 函数（card drag ghost 专用）
var (
	gdi32CardDragDLL           = syscall.NewLazyDLL("gdi32.dll")
	user32CardDragDLL          = syscall.NewLazyDLL("user32.dll")
	procCreateWindowExDrag     = user32CardDragDLL.NewProc("CreateWindowExW")
	procDestroyWindowDrag      = user32CardDragDLL.NewProc("DestroyWindow")
	procUpdateLayeredWindowDrag = user32CardDragDLL.NewProc("UpdateLayeredWindow")
	procCreateCompatibleDCDrag = gdi32CardDragDLL.NewProc("CreateCompatibleDC")
	procDeleteDCDrag           = gdi32CardDragDLL.NewProc("DeleteDC")
	procCreateDIBSectionDrag   = gdi32CardDragDLL.NewProc("CreateDIBSection")
	procSelectObjectDrag       = gdi32CardDragDLL.NewProc("SelectObject")
	procBitBltDrag             = gdi32CardDragDLL.NewProc("BitBlt")
)

const (
	cardDragOpacity = 180 // 0~255, ~70% 不透明度
	cardDragBW      = 2   // 边框宽度
)

// dragGhostWndProc 拖拽幽灵窗口的窗口过程
func dragGhostWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// ensureDragGhost 创建拖拽幽灵窗口（WS_EX_LAYERED）+ 捕获卡片快照
func (s *CardDragOutline) ensureDragGhost(card *ui.GroupCard) {
	if s.DragGhostHwnd != 0 {
		return
	}

	containerHwnd := card.Container().Handle()
	var rect win.RECT
	win.GetWindowRect(containerHwnd, &rect)
	cw := int(rect.Right - rect.Left)
	ch := int(rect.Bottom - rect.Top)
	if cw <= 0 || ch <= 0 {
		return
	}

	// 注册窗口类
	hInst := win.GetModuleHandle(nil)
	className := syscall.StringToUTF16Ptr("DesktopGoCardDragGhost")
	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(dragGhostWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(5)) // HOLLOW_BRUSH
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)

	// 创建 WS_EX_LAYERED 弹出窗口
	exStyle := uint32(win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)

	hwnd, _, _ := procCreateWindowExDrag.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(style),
		uintptr(rect.Left), uintptr(rect.Top), uintptr(cw), uintptr(ch),
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	s.DragGhostHwnd = win.HWND(hwnd)

	// 捕获卡片内容到 DIB
	s.captureCardContent(int(rect.Left), int(rect.Top), cw, ch)

	win.ShowWindow(s.DragGhostHwnd, win.SW_SHOWNA)
}

// captureCardContent 从屏幕捕获卡片区域像素到 DIB，设置透明度 + 白色边框
func (s *CardDragOutline) captureCardContent(screenX, screenY, w, h int) {
	if s.DragGhostHwnd == 0 {
		return
	}

	hdcScreen := win.GetDC(0)
	if hdcScreen == 0 {
		return
	}
	defer win.ReleaseDC(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDCDrag.Call(uintptr(hdcScreen))
	if hdcMem == 0 {
		return
	}
	defer procDeleteDCDrag.Call(hdcMem)

	// 创建 DIB section（32-bit BGRA，顶向下）
	var bi win.BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	hBmp, _, _ := procCreateDIBSectionDrag.Call(hdcMem, uintptr(unsafe.Pointer(&bi.BmiHeader)),
		uintptr(win.DIB_RGB_COLORS), uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hBmp == 0 {
		return
	}

	s.DragGhostDib = win.HBITMAP(hBmp)
	s.DragGhostDibBits = bits
	s.DragGhostW = w
	s.DragGhostH = h

	hOld, _, _ := procSelectObjectDrag.Call(hdcMem, uintptr(hBmp))
	if hOld == 0 {
		return
	}
	defer procSelectObjectDrag.Call(hdcMem, hOld)

	procBitBltDrag.Call(hdcMem, 0, 0, uintptr(w), uintptr(h),
		uintptr(hdcScreen), uintptr(screenX), uintptr(screenY), uintptr(win.SRCCOPY))

	// 像素处理：内容降低透明度、边框纯白不透明
	pixels := (*[1 << 24]byte)(bits)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := (y*w + x) * 4
			b := pixels[idx+0]
			g := pixels[idx+1]
			r := pixels[idx+2]

			isBorder := x < cardDragBW || x >= w-cardDragBW || y < cardDragBW || y >= h-cardDragBW
			if isBorder {
				pixels[idx+0] = 255
				pixels[idx+1] = 255
				pixels[idx+2] = 255
				pixels[idx+3] = 255
			} else {
				newA := uint16(cardDragOpacity)
				pixels[idx+0] = byte(uint16(b) * newA / 255)
				pixels[idx+1] = byte(uint16(g) * newA / 255)
				pixels[idx+2] = byte(uint16(r) * newA / 255)
				pixels[idx+3] = byte(newA)
			}
		}
	}

	// UpdateLayeredWindow 显示
	var pt win.POINT
	var size win.SIZE
	size.CX = int32(w)
	size.CY = int32(h)
	var blend win.BLENDFUNCTION
	blend.BlendOp = 0 // AC_SRC_OVER
	blend.BlendFlags = 0
	blend.SourceConstantAlpha = 255
	blend.AlphaFormat = win.AC_SRC_ALPHA

	procUpdateLayeredWindowDrag.Call(
		uintptr(s.DragGhostHwnd),
		uintptr(hdcScreen),
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(hdcMem),
		uintptr(unsafe.Pointer(&pt)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		uintptr(2)) // ULW_ALPHA
}

// moveDragGhost 移动拖拽幽灵窗口到指定屏幕坐标
func (s *CardDragOutline) moveDragGhost(screenX, screenY int) {
	if s.DragGhostHwnd == 0 {
		return
	}
	win.SetWindowPos(s.DragGhostHwnd, 0,
		int32(screenX), int32(screenY),
		int32(s.DragGhostW), int32(s.DragGhostH),
		win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

// destroyDragGhost 销毁拖拽幽灵窗口
func (s *CardDragOutline) destroyDragGhost() {
	if s.DragGhostHwnd != 0 {
		procDestroyWindowDrag.Call(uintptr(s.DragGhostHwnd))
		s.DragGhostHwnd = 0
	}
	if s.DragGhostDib != 0 {
		win.DeleteObject(win.HGDIOBJ(s.DragGhostDib))
		s.DragGhostDib = 0
	}
	s.DragGhostDibBits = nil
}

// OnCardDragOutline 卡片拖拽虚框更新 — 显示卡片快照幽灵窗口
func (s *CardDragOutline) OnCardDragOutline(card *ui.GroupCard, newX, newY int) {
	if s.DragGhostHwnd == 0 {
		s.ensureDragGhost(card)
	}
	s.DragOutlineCard = card
	s.DragOutlineX = newX
	s.DragOutlineY = newY
	s.DragOutlineW = card.PixelW()
	s.DragOutlineH = card.PixelH()

	screenX := newX + s.workX
	screenY := newY + s.workY
	s.moveDragGhost(screenX, screenY)
}

// OnCardDragOutlineEnd 卡片拖拽虚框结束 — 销毁幽灵窗口
func (s *CardDragOutline) OnCardDragOutlineEnd(card *ui.GroupCard) {
	s.destroyDragGhost()
	s.DragOutlineCard = nil
}
