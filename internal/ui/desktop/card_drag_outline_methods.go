package desktop

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

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
	cardDragOpacity    = 180
	cardDragBW         = 2
	guideCheckInterval = 500 * time.Millisecond
	guideSnapThresh    = 10
)

func dragGhostWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

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
	hInst := win.GetModuleHandle(nil)
	className := syscall.StringToUTF16Ptr("DesktopGoCardDragGhost")
	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(dragGhostWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(5))
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)
	exStyle := uint32(win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)
	hwnd, _, _ := procCreateWindowExDrag.Call(
		uintptr(exStyle), uintptr(unsafe.Pointer(className)), 0, uintptr(style),
		uintptr(rect.Left), uintptr(rect.Top), uintptr(cw), uintptr(ch),
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	s.DragGhostHwnd = win.HWND(hwnd)
	s.captureCardContent(int(rect.Left), int(rect.Top), cw, ch)
	win.ShowWindow(s.DragGhostHwnd, win.SW_SHOWNA)
}

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
	var pt win.POINT
	var size win.SIZE
	size.CX = int32(w)
	size.CY = int32(h)
	var blend win.BLENDFUNCTION
	blend.BlendOp = 0
	blend.BlendFlags = 0
	blend.SourceConstantAlpha = 255
	blend.AlphaFormat = win.AC_SRC_ALPHA
	procUpdateLayeredWindowDrag.Call(uintptr(s.DragGhostHwnd), uintptr(hdcScreen),
		0, uintptr(unsafe.Pointer(&size)), uintptr(hdcMem), uintptr(unsafe.Pointer(&pt)),
		0, uintptr(unsafe.Pointer(&blend)), uintptr(2))
}

func (s *CardDragOutline) moveDragGhost(screenX, screenY int) {
	if s.DragGhostHwnd == 0 {
		return
	}
	win.SetWindowPos(s.DragGhostHwnd, 0, int32(screenX), int32(screenY),
		int32(s.DragGhostW), int32(s.DragGhostH), win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

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

func (s *CardDragOutline) OnCardDragOutlineEx(card *ui.GroupCard, newX, newY int, cards []*ui.GroupCard) {
	if s.DragGhostHwnd == 0 {
		s.ensureDragGhost(card)
	}
	s.DragOutlineCard = card
	s.DragOutlineW = card.PixelW()
	s.DragOutlineH = card.PixelH()
	s.DragOutlineX = newX
	s.DragOutlineY = newY

	now := time.Now().UnixNano()
	if now-s.guideLastCheck < int64(guideCheckInterval) {
		screenX := newX + s.workX
		screenY := newY + s.workY
		s.moveDragGhost(screenX, screenY)
		return
	}

	hLines, vLines := s.detectAlignment(card, newX, newY, cards)
	logger.Debug("OnCardDragOutlineEx: newX=%d newY=%d hLines=%v vLines=%v", newX, newY, hLines, vLines)
	s.guide.show(hLines, vLines)
	s.guideLastCheck = now
	s.guideLastX = newX
	s.guideLastY = newY

	screenX := newX + s.workX
	screenY := newY + s.workY
	s.moveDragGhost(screenX, screenY)
}

func (s *CardDragOutline) detectAlignment(card *ui.GroupCard, newX, newY int, cards []*ui.GroupCard) (hLines, vLines []int) {
	for _, other := range cards {
		if other == card {
			continue
		}
		// 被拖拽卡 left → 其他卡 left
		if abs(newX-other.PixelX()) <= guideSnapThresh {
			vLines = append(vLines, other.PixelX())
		}
		// 被拖拽卡 top → 其他卡 top
		if abs(newY-other.PixelY()) <= guideSnapThresh {
			hLines = append(hLines, other.PixelY())
		}
	}
	hLines = uniqueInts(hLines)
	vLines = uniqueInts(vLines)
	return
}

func (s *CardDragOutline) SnapPosition(card *ui.GroupCard, cards []*ui.GroupCard, newX, newY int) (snappedX, snappedY int) {
	// 默认保持实际拖到的位置
	snappedX, snappedY = newX, newY

	// X 轴吸附：仅当被拖拽卡 left 接近某张卡 left 时吸附 X
	// Y 轴吸附：仅当被拖拽卡 top 接近某张卡 top 时吸附 Y
	// 两个轴独立，未满足条件的一轴保持实际拖到的位置（newX/newY）
	bestX := guideSnapThresh + 1
	bestY := guideSnapThresh + 1

	for _, other := range cards {
		if other == card {
			continue
		}
		// X 轴：left → other left
		dist := newX - other.PixelX()
		if dist < 0 {
			dist = -dist
		}
		if dist <= guideSnapThresh && dist < bestX {
			bestX = dist
			snappedX = other.PixelX()
		}
		// Y 轴：top → other top
		dist = newY - other.PixelY()
		if dist < 0 {
			dist = -dist
		}
		if dist <= guideSnapThresh && dist < bestY {
			bestY = dist
			snappedY = other.PixelY()
		}
	}
	return
}

func (s *CardDragOutline) OnCardDragOutlineEnd(card *ui.GroupCard) {
	s.destroyDragGhost()
	s.guide.destroy()
	s.DragOutlineCard = nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func uniqueInts(slice []int) []int {
	if len(slice) == 0 {
		return slice
	}
	seen := make(map[int]bool, len(slice))
	result := make([]int, 0, len(slice))
	for _, v := range slice {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
