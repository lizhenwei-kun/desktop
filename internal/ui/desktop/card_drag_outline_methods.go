package desktop

import (
	"desktop_go/internal/ui"
)

// OnCardDragOutline 卡片拖拽虚框更新 — 使用弹出窗口替代 paintCanvas 方式
func (s *CardDragOutline) OnCardDragOutline(card *ui.GroupCard, newX, newY int) {
	s.dragOutline.ensureCreated()
	s.DragOutlineCard = card
	s.DragOutlineX = newX
	s.DragOutlineY = newY
	s.DragOutlineW = card.PixelW()
	s.DragOutlineH = card.PixelH()

	// newX/newY 来自 handleDrag: dragCardX(client-relative) + screenDelta
	// 转为屏幕坐标: screenX = newX + workX
	screenX := newX + s.workX
	screenY := newY + s.workY
	s.dragOutline.showAt(screenX, screenY, s.DragOutlineW, s.DragOutlineH)
}

// OnCardDragOutlineEnd 卡片拖拽虚框结束 — 隐藏弹出窗口
func (s *CardDragOutline) OnCardDragOutlineEnd(card *ui.GroupCard) {
	s.dragOutline.hide()
	s.DragOutlineCard = nil
}
