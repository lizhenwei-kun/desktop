package desktop

import (
	"syscall"

	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// OnCardResizeOutline 卡片缩放虚框更新
func (s *ResizeOutlineState) OnCardResizeOutline(card *ui.GroupCard, newX, newY, newW, newH int) {
	if s.ResizeOutlineCard != nil {
		s.DrawResizeOutlineWin32(s.ResizeOutlineX, s.ResizeOutlineY, s.ResizeOutlineW, s.ResizeOutlineH)
	}
	s.ResizeOutlineCard = card
	s.ResizeOutlineX = newX
	s.ResizeOutlineY = newY
	s.ResizeOutlineW = newW
	s.ResizeOutlineH = newH
	s.DrawResizeOutlineWin32(newX, newY, newW, newH)
}

// OnCardResizeOutlineEnd 卡片缩放虚框结束
func (s *ResizeOutlineState) OnCardResizeOutlineEnd(card *ui.GroupCard) {
	if s.ResizeOutlineCard != nil {
		s.DrawResizeOutlineWin32(s.ResizeOutlineX, s.ResizeOutlineY, s.ResizeOutlineW, s.ResizeOutlineH)
	}
	s.ResizeOutlineCard = nil
}

// DrawResizeOutlineWin32 在屏幕上绘制缩放虚框（XOR 模式）
func (s *ResizeOutlineState) DrawResizeOutlineWin32(x, y, w, h int) {
	hdc := win.GetDC(0)
	if hdc == 0 {
		return
	}
	defer win.ReleaseDC(0, hdc)
	screenX := x + s.resizeWorkX()
	screenY := y + s.resizeWorkY()
	gdi32 := syscall.NewLazyDLL("gdi32.dll")
	procSetROP2 := gdi32.NewProc("SetROP2")
	procCreatePen := gdi32.NewProc("CreatePen")
	procGetStockObject := gdi32.NewProc("GetStockObject")
	procSetROP2.Call(uintptr(hdc), uintptr(3))
	pen, _, _ := procCreatePen.Call(uintptr(0), uintptr(2), uintptr(win.RGB(0xFF, 0xFF, 0xFF)))
	if pen == 0 {
		return
	}
	defer gdi32.NewProc("DeleteObject").Call(pen)
	oldPen := win.SelectObject(hdc, win.HGDIOBJ(pen))
	defer win.SelectObject(hdc, oldPen)
	hollowBrush, _, _ := procGetStockObject.Call(uintptr(5))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(hollowBrush))
	defer win.SelectObject(hdc, oldBrush)
	win.MoveToEx(hdc, screenX, screenY, nil)
	win.LineTo(hdc, int32(screenX+w), int32(screenY))
	win.LineTo(hdc, int32(screenX+w), int32(screenY+h))
	win.LineTo(hdc, int32(screenX), int32(screenY+h))
	win.LineTo(hdc, int32(screenX), int32(screenY))
}
