package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// OnCardDragOutline 卡片拖拽虚框更新
func (s *CardDragOutline) OnCardDragOutline(card *ui.GroupCard, newX, newY int) {
	s.DragOutlineCard = card
	s.DragOutlineX = newX
	s.DragOutlineY = newY
	s.DragOutlineW = card.PixelW()
	s.DragOutlineH = card.PixelH()
	s.outlineInvalidate()
}

// OnCardDragOutlineEnd 卡片拖拽虚框结束
func (s *CardDragOutline) OnCardDragOutlineEnd(card *ui.GroupCard) {
	s.DragOutlineCard = nil
	s.outlineInvalidate()
}

// PaintCardDragOutline 绘制卡片拖拽虚框
func (s *CardDragOutline) PaintCardDragOutline(canvas *walk.Canvas, bodyWidget *walk.CustomWidget) {
	if s.DragOutlineCard == nil {
		return
	}
	var tl, br win.POINT
	tl.X = int32(s.DragOutlineX)
	tl.Y = int32(s.DragOutlineY)
	br.X = int32(s.DragOutlineX + s.DragOutlineW)
	br.Y = int32(s.DragOutlineY + s.DragOutlineH)
	win.ScreenToClient(bodyWidget.Handle(), &tl)
	win.ScreenToClient(bodyWidget.Handle(), &br)
	rect := walk.Rectangle{X: int(tl.X), Y: int(tl.Y), Width: int(br.X - tl.X), Height: int(br.Y - tl.Y)}
	pen, err := walk.NewCosmeticPen(walk.PenDash, walk.RGB(0xFF, 0xFF, 0xFF))
	if err != nil {
		return
	}
	defer pen.Dispose()
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y + rect.Height}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X + rect.Width, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
}
