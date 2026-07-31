package desktop

import (
	"time"

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

// OnCardResizeOutlineEx 带参考线的缩放虚框更新。
// 参考线以两个卡片的右下角坐标 (right, bottom) 为标准对齐。
func (s *ResizeOutlineState) OnCardResizeOutlineEx(card *ui.GroupCard, newX, newY, newW, newH int, cards []*ui.GroupCard) {
	s.OnCardResizeOutline(card, newX, newY, newW, newH)

	// 每隔 guideCheckInterval 检测一次右下角对齐（避免每帧卡顿）
	now := time.Now().UnixNano()
	if now-s.guideLastCheck < int64(guideCheckInterval) {
		return
	}
	s.guideLastCheck = now

	hLines, vLines := s.detectResizeAlignment(card, newX, newY, newW, newH, cards)
	s.guide.show(hLines, vLines)
}

// detectResizeAlignment 检测被缩放卡的右下角 (right, bottom) 与其他卡右下角的对齐。
func (s *ResizeOutlineState) detectResizeAlignment(card *ui.GroupCard, newX, newY, newW, newH int, cards []*ui.GroupCard) (hLines, vLines []int) {
	dRight := newX + newW
	dBottom := newY + newH

	for _, other := range cards {
		if other == card {
			continue
		}
		// 被缩放卡 right → 其他卡 right
		if abs(dRight-other.PixelRight()) <= guideSnapThresh {
			vLines = append(vLines, other.PixelRight())
		}
		// 被缩放卡 bottom → 其他卡 bottom
		if abs(dBottom-other.PixelBottom()) <= guideSnapThresh {
			hLines = append(hLines, other.PixelBottom())
		}
	}
	hLines = uniqueInts(hLines)
	vLines = uniqueInts(vLines)
	return
}

// OnCardResizeOutlineEnd 卡片缩放虚框结束 — 隐藏边框窗口和参考线
func (s *ResizeOutlineState) OnCardResizeOutlineEnd(card *ui.GroupCard) {
	s.resizeOutline.hide()
	s.guide.destroy()
	s.ResizeOutlineCard = nil
}

// DrawResizeOutlineWin32 保留空函数（不再使用 XOR 方式）
func (s *ResizeOutlineState) DrawResizeOutlineWin32(x, y, w, h int) {
	// 已废弃 — 改用弹出窗口方式
}
