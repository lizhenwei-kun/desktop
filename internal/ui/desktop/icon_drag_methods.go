package desktop

import (
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/ui"
)

// DragThrottleInvalidate 拖拽重绘节流
func (s *IconDragState) DragThrottleInvalidate() {
	if time.Since(s.LastDragMoveTime) < 30*time.Millisecond {
		return
	}
	s.LastDragMoveTime = time.Now()
	s.iconInvalidate()
}

// OnCardIconDragStart 卡片内图标拖拽开始
func (s *IconDragState) OnCardIconDragStart(card *ui.GroupCard, idx int, item group.GroupItem) {
	s.IconDragActive = true
	s.IconDragSourceCard = card
	s.IconDragItem = item
	s.IconDragSourceGroup = card.GroupName()
	s.LoadGhostBmp(item.Path)
	var curPt win.POINT
	win.GetCursorPos(&curPt)
	s.IconDragScreenX = int(curPt.X)
	s.IconDragScreenY = int(curPt.Y)
	s.LastDragMoveTime = time.Now()
	s.iconInvalidate()
}

// OnCardIconDragMove 卡片内图标拖拽移动
func (s *IconDragState) OnCardIconDragMove(card *ui.GroupCard, screenX, screenY int) {
	s.IconDragScreenX = screenX
	s.IconDragScreenY = screenY
	s.UpdateDropTarget(screenX, screenY)
}

// OnCardIconDragEnd 卡片内图标拖拽结束
func (s *IconDragState) OnCardIconDragEnd(card *ui.GroupCard, screenX, screenY int) {
	s.IconDragActive = false
	s.DisposeGhostBmp()
	s.iconInvalidate()
	targetCard := s.FindCardAtPoint(screenX, screenY)
	sourceGroup := card.GroupName()
	if s.IconIsPointInUngroupedArea(screenX, screenY) {
		s.iconMoveItemToDesktop(card.IconDragItem().Path)
		s.iconRefreshCard(card)
	} else if targetCard != nil && targetCard != card {
		s.iconMoveItemToGroup(card.IconDragItem().Path, targetCard.GroupName())
		s.iconRefreshCard(card)
		s.iconRefreshCard(targetCard)
	} else if targetCard == card {
		insertIdx := card.GetDropIndexAt(card.IconDragMouseX(), card.IconDragMouseY())
		if insertIdx >= 0 && insertIdx <= len(card.Items()) {
			s.iconMoveItemWithinGroup(sourceGroup, card.IconDragItem().Path, insertIdx)
		}
		s.iconRefreshCard(card)
	} else {
		s.iconMoveItemToDesktop(card.IconDragItem().Path)
		s.iconRefreshCard(card)
	}
	s.ClearDropState()
}

// UpdateDropTarget 更新拖拽目标
func (s *IconDragState) UpdateDropTarget(screenX, screenY int) {
	if s.DropTargetCard != nil {
		s.DropTargetCard.SetIsDropTarget(false)
	}
	s.DropTargetCard = nil
	s.DropToDesktop = false
	for _, c := range s.iconCards() {
		if s.IsPointInCard(c, screenX, screenY) {
			s.DropTargetCard = c
			c.SetIsDropTarget(true)
			break
		}
	}
	if s.DropTargetCard == nil && s.IconIsPointInUngroupedArea(screenX, screenY) {
		s.DropToDesktop = true
	}
	s.DragThrottleInvalidate()
}

// ClearDropState 清除拖拽状态
func (s *IconDragState) ClearDropState() {
	if s.DropTargetCard != nil {
		s.DropTargetCard.SetIsDropTarget(false)
	}
	s.DropTargetCard = nil
	s.DropToDesktop = false
	s.IconDragSourceCard = nil
	s.IconDragSourceGroup = ""
}

// FindCardAtPoint 查找指定屏幕坐标所在的卡片
func (s *IconDragState) FindCardAtPoint(screenX, screenY int) *ui.GroupCard {
	for _, c := range s.iconCards() {
		if s.IsPointInCard(c, screenX, screenY) {
			return c
		}
	}
	return nil
}

// IsPointInCard 判断屏幕坐标是否在卡片内
func (s *IconDragState) IsPointInCard(card *ui.GroupCard, screenX, screenY int) bool {
	sb := card.ScreenBounds()
	return screenX >= sb.X && screenX <= sb.X+sb.Width &&
		screenY >= sb.Y && screenY <= sb.Y+sb.Height
}

// IconIsPointInUngroupedArea 判断屏幕坐标是否在未分组图标区域
func (s *IconDragState) IconIsPointInUngroupedArea(screenX, screenY int) bool {
	return s.iconIsPointInUngrouped(screenX, screenY)
}

// ActivateFromFreeDrag 从未分组拖拽激活图标拖拽状态
func (s *IconDragState) ActivateFromFreeDrag(item group.GroupItem, screenX, screenY int) {
	s.IconDragActive = true
	s.IconDragItem = item
	s.IconDragSourceGroup = ""
	s.IconDragScreenX = screenX
	s.IconDragScreenY = screenY
}

// LoadGhostBmp 加载拖拽 ghost 图像
func (s *IconDragState) LoadGhostBmp(filePath string) {
	s.DisposeGhostBmp()
	s.GhostBmp = ui.GlobalIconBmpCache.GetOrLoad(filePath)
}

// DisposeGhostBmp 释放拖拽 ghost 图像
func (s *IconDragState) DisposeGhostBmp() {
	s.GhostBmp = nil
}

// PaintCardItemDragGhost 绘制卡片内图标拖拽 ghost
func (s *IconDragState) PaintCardItemDragGhost(canvas *walk.Canvas) {
	if s.GhostBmp == nil || !s.IconDragActive || s.IconDragSourceCard == nil {
		return
	}
	var pt win.POINT
	pt.X = int32(s.IconDragScreenX)
	pt.Y = int32(s.IconDragScreenY)
	win.ScreenToClient(s.iconBodyWidget().Handle(), &pt)
	ghostX := int(pt.X) - ui.TileWidth()/2
	ghostY := int(pt.Y) - ui.TileHeight()/2
	iconX := ghostX + (ui.TileWidth()-ui.DesktopIconSize)/2
	iconY := ghostY + ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(s.GhostBmp,
		walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 128)
	font := ui.GetIconFont()
	if font != nil {
		defer font.Dispose()
		lines := ui.SplitTextToLines(s.IconDragItem.Name, 4)
		labelTop := ghostY + ui.DesktopIconLabelTop
		for i, line := range lines {
			if i >= 2 {
				break
			}
			if i == 1 && len(lines) > 2 {
				line = ui.TruncateText(line, 3)
			}
			lineY := labelTop + i*ui.DesktopIconLineHeight
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF),
				walk.Rectangle{X: ghostX, Y: lineY, Width: ui.TileWidth(), Height: ui.DesktopIconLineHeight},
				walk.TextCenter|walk.TextSingleLine)
		}
	}
}
