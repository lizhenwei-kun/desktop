package desktop

import (
	"desktop_go/internal/ui"
)

// OnCardResizeOutline 卡片缩放虚框更新 — 使用弹出窗口替代 XOR 屏幕绘制
func (s *ResizeOutlineState) OnCardResizeOutline(card *ui.GroupCard, newX, newY, newW, newH int) {
	s.resizeOutline.ensureCreated()
	s.ResizeOutlineCard = card
	s.ResizeOutlineX = newX
	s.ResizeOutlineY = newY
	s.ResizeOutlineW = newW
	s.ResizeOutlineH = newH

	screenX := newX + s.resizeWorkX()
	screenY := newY + s.resizeWorkY()
	s.resizeOutline.showAt(screenX, screenY, newW, newH)
}

// OnCardResizeOutlineEnd 卡片缩放虚框结束 — 隐藏弹出窗口
func (s *ResizeOutlineState) OnCardResizeOutlineEnd(card *ui.GroupCard) {
	s.resizeOutline.hide()
	s.ResizeOutlineCard = nil
}

// DrawResizeOutlineWin32 保留空函数（不再使用 XOR 方式）
func (s *ResizeOutlineState) DrawResizeOutlineWin32(x, y, w, h int) {
	// 已废弃 — 改用弹出窗口方式
}
