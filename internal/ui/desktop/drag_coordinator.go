package desktop

import (
	"time"

	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/ui"
)

func (dm *DesktopMode) onCardIconDragStart(card *ui.GroupCard, idx int, item group.GroupItem) {
	dm.IconDragActive = true
	dm.IconDragSourceCard = card
	dm.IconDragItem = item
	dm.IconDragSourceGroup = card.GroupName()
	dm.loadDragGhostBmp(item.Path)
	var curPt win.POINT
	win.GetCursorPos(&curPt)
	dm.IconDragScreenX = int(curPt.X)
	dm.IconDragScreenY = int(curPt.Y)
	dm.LastDragMoveTime = time.Now()
	dm.BodyWidget.Invalidate()
}

func (dm *DesktopMode) onCardIconDragMove(card *ui.GroupCard, screenX, screenY int) {
	dm.IconDragScreenX = screenX
	dm.IconDragScreenY = screenY
	dm.updateDropTarget(screenX, screenY)
}

func (dm *DesktopMode) onCardIconDragEnd(card *ui.GroupCard, screenX, screenY int) {
	dm.IconDragActive = false
	dm.disposeDragGhostBmp()
	dm.BodyWidget.Invalidate()
	targetCard := dm.findCardAtPoint(screenX, screenY)
	sourceGroup := card.GroupName()
	if dm.isPointInUngroupedArea(screenX, screenY) {
		dm.Manager.MoveItemToDesktop(card.IconDragItem().Path)
		card.Refresh()
	} else if targetCard != nil && targetCard != card {
		dm.Manager.MoveItemToGroup(card.IconDragItem().Path, targetCard.GroupName())
		card.Refresh()
		targetCard.Refresh()
	} else if targetCard == card {
		insertIdx := card.GetDropIndexAt(card.IconDragMouseX(), card.IconDragMouseY())
		if insertIdx >= 0 && insertIdx <= len(card.Items()) {
			dm.Manager.MoveItemWithinGroup(sourceGroup, card.IconDragItem().Path, insertIdx)
		}
		card.Refresh()
	} else {
		dm.Manager.MoveItemToDesktop(card.IconDragItem().Path)
		card.Refresh()
	}
	dm.clearDropState()
}

func (dm *DesktopMode) onCardDragOutline(card *ui.GroupCard, newX, newY int) {
	dm.DragOutlineCard = card
	dm.DragOutlineX = newX
	dm.DragOutlineY = newY
	dm.DragOutlineW = card.PixelW()
	dm.DragOutlineH = card.PixelH()
	dm.dragThrottleInvalidate()
}

func (dm *DesktopMode) onCardDragOutlineEnd(card *ui.GroupCard) {
	dm.DragOutlineCard = nil
	dm.BodyWidget.Invalidate()
}

func (dm *DesktopMode) onCardResizeOutline(card *ui.GroupCard, newX, newY, newW, newH int) {
	if dm.ResizeOutlineCard != nil {
		dm.drawResizeOutlineWin32(dm.ResizeOutlineX, dm.ResizeOutlineY, dm.ResizeOutlineW, dm.ResizeOutlineH)
	}
	dm.ResizeOutlineCard = card
	dm.ResizeOutlineX = newX
	dm.ResizeOutlineY = newY
	dm.ResizeOutlineW = newW
	dm.ResizeOutlineH = newH
	dm.drawResizeOutlineWin32(newX, newY, newW, newH)
}

func (dm *DesktopMode) onCardResizeOutlineEnd(card *ui.GroupCard) {
	if dm.ResizeOutlineCard != nil {
		dm.drawResizeOutlineWin32(dm.ResizeOutlineX, dm.ResizeOutlineY, dm.ResizeOutlineW, dm.ResizeOutlineH)
	}
	dm.ResizeOutlineCard = nil
}

func (dm *DesktopMode) dragThrottleInvalidate() {
	if time.Since(dm.LastDragMoveTime) < 30*time.Millisecond {
		return
	}
	dm.LastDragMoveTime = time.Now()
	dm.BodyWidget.Invalidate()
}

func (dm *DesktopMode) updateDropTarget(screenX, screenY int) {
	if dm.DropTargetCard != nil {
		dm.DropTargetCard.SetIsDropTarget(false)
	}
	dm.DropTargetCard = nil
	dm.DropToDesktop = false
	for _, c := range dm.Cards {
		if dm.isPointInCard(c, screenX, screenY) {
			dm.DropTargetCard = c
			c.SetIsDropTarget(true)
			break
		}
	}
	if dm.DropTargetCard == nil && dm.isPointInUngroupedArea(screenX, screenY) {
		dm.DropToDesktop = true
	}
	dm.dragThrottleInvalidate()
}

func (dm *DesktopMode) clearDropState() {
	if dm.DropTargetCard != nil {
		dm.DropTargetCard.SetIsDropTarget(false)
	}
	dm.DropTargetCard = nil
	dm.DropToDesktop = false
	dm.IconDragSourceCard = nil
	dm.IconDragSourceGroup = ""
	dm.DragOutlineCard = nil
}

func (dm *DesktopMode) findCardAtPoint(screenX, screenY int) *ui.GroupCard {
	for _, c := range dm.Cards {
		if dm.isPointInCard(c, screenX, screenY) {
			return c
		}
	}
	return nil
}

func (dm *DesktopMode) isPointInCard(card *ui.GroupCard, screenX, screenY int) bool {
	sb := card.ScreenBounds()
	return screenX >= sb.X && screenX <= sb.X+sb.Width &&
		screenY >= sb.Y && screenY <= sb.Y+sb.Height
}

func (dm *DesktopMode) isPointInUngroupedArea(screenX, screenY int) bool {
	var pt win.POINT
	pt.X = int32(screenX)
	pt.Y = int32(screenY)
	win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
	cx := int(pt.X)
	cy := int(pt.Y)
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if cx >= ix && cx <= ix+ui.TileWidth() &&
			cy >= iy && cy <= iy+ui.TileHeight() {
			return true
		}
	}
	return false
}
