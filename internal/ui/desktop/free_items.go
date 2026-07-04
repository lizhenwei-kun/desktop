package desktop

import (
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/ui"
)

const (
	freeGridLeft = 20
	freeGridTop  = 60
)

func freeCellW() int { return ui.TileWidth() + ui.DesktopIconGap }
func freeCellH() int { return ui.TileHeight() + ui.DesktopIconGap }

func gridToPixel(col, row int) (int, int) {
	return freeGridLeft + col*freeCellW(), freeGridTop + row*freeCellH()
}

func pixelToGrid(px, py int) (int, int) {
	col := (px - freeGridLeft + freeCellW()/2) / freeCellW()
	row := (py - freeGridTop + freeCellH()/2) / freeCellH()
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	return col, row
}

// posToGrid 保留在 DesktopMode，grid helpers 被多处调用
func (dm *DesktopMode) posToGrid(pos config.Position) (int, int) {
	px := int(pos.X * float64(dm.WorkW))
	py := int(pos.Y * float64(dm.WorkH))
	return pixelToGrid(px, py)
}

// gridToRel 保留在 DesktopMode
func (dm *DesktopMode) gridToRel(col, row int) config.Position {
	px, py := gridToPixel(col, row)
	return config.Position{
		X: float64(px) / float64(dm.WorkW),
		Y: float64(py) / float64(dm.WorkH),
	}
}

// getOccupiedCells 保留在 DesktopMode
func (dm *DesktopMode) getOccupiedCells(exceptPath string) map[[2]int]bool {
	items := dm.Manager.GetUngroupedItems()
	cells := make(map[[2]int]bool)
	for _, item := range items {
		if item.Path == exceptPath {
			continue
		}
		pos := dm.Manager.GetFreeItemPosition(item.Path)
		col, row := dm.posToGrid(pos)
		if col < 0 || row < 0 {
			continue
		}
		cell := [2]int{col, row}
		if !cells[cell] {
			cells[cell] = true
		}
	}
	return cells
}

// getFreeItemPixelPos 保留在 DesktopMode
func (dm *DesktopMode) getFreeItemPixelPos(path string, fallbackIdx int) (int, int) {
	pos := dm.Manager.GetFreeItemPosition(path)
	if pos.X < 0 || pos.Y < 0 {
		bounds := dm.BodyWidget.ClientBoundsPixels()
		if bounds.Width < 100 || bounds.Height < 100 {
			maxRow := dm.WorkH / freeCellH()
			if maxRow < 1 {
				maxRow = 1
			}
			col := fallbackIdx / maxRow
			row := fallbackIdx % maxRow
			return gridToPixel(col, row)
		}
		col, row := dm.findFreeGridCell("", 0, fallbackIdx)
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(path, relPos)
		return gridToPixel(col, row)
	}
	col, row := dm.posToGrid(pos)
	return gridToPixel(col, row)
}

// findFreeGridCell 保留在 DesktopMode
func (dm *DesktopMode) findFreeGridCell(exceptPath string, wantCol, wantRow int) (int, int) {
	occupied := dm.getOccupiedCells(exceptPath)
	bounds := dm.BodyWidget.ClientBoundsPixels()
	maxCol := bounds.Width / freeCellW()
	if maxCol < 1 {
		maxCol = 1
	}
	maxRow := bounds.Height / freeCellH()
	if maxRow < 1 {
		maxRow = 1
	}
	for attempt := 0; attempt < 500; attempt++ {
		cell := [2]int{wantCol, wantRow}
		if !occupied[cell] {
			return wantCol, wantRow
		}
		wantRow++
		if wantRow >= maxRow {
			wantRow = 0
			wantCol++
		}
		if wantCol >= maxCol {
			wantCol = 0
		}
	}
	return wantCol, wantRow
}

// handleDesktopMouseDown 桌面左键按下
func (dm *DesktopMode) handleDesktopMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}
	bounds := dm.BodyWidget.ClientBoundsPixels()
	btnRect := walk.Rectangle{X: bounds.Width - 140, Y: 10, Width: 120, Height: 30}
	if x >= btnRect.X && x <= btnRect.X+btnRect.Width &&
		y >= btnRect.Y && y <= btnRect.Y+btnRect.Height {
		dm.addNewCard()
		return
	}
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			dm.FreeItemDragPressed = true
			dm.FreeItemDragIdx = i
			dm.FreeItemDragItem = item
			dm.FreeItemDragStartX = x
			dm.FreeItemDragStartY = y
			dm.FreeItemDragStartTime = time.Now()
			go dm.checkFreeItemDragStart()
			return
		}
	}
}

// checkFreeItemDragStart 长按延迟后启动未分组图标拖拽
func (dm *DesktopMode) checkFreeItemDragStart() {
	defer recoverGoroutine("checkFreeItemDragStart")
	time.Sleep(ui.IconDragDelay)
	dm.Post(func() {
		if !dm.FreeItemDragPressed || dm.FreeItemDragActive {
			return
		}
		dm.FreeItemDragActive = true
		var screenPt, clientPt win.POINT
		win.GetCursorPos(&screenPt)
		clientPt = screenPt
		win.ScreenToClient(dm.BodyWidget.Handle(), &clientPt)
		dm.FreeItemDragMouseX = int(clientPt.X)
		dm.FreeItemDragMouseY = int(clientPt.Y)
		dm.IconDragState.LoadGhostBmp(dm.FreeItemDragItem.Path)
		dm.LastDragMoveTime = time.Now()
		dm.BodyWidget.Invalidate()
		win.SetCapture(dm.BodyWidget.Handle())
		dm.IconDragState.ActivateFromFreeDrag(dm.FreeItemDragItem, int(screenPt.X), int(screenPt.Y))
	})
}

// handleFreeItemDrop 未分组图标拖拽释放
func (dm *DesktopMode) handleFreeItemDrop(screenX, screenY int) {
	dm.IconDragActive = false
	dm.IconDragState.DisposeGhostBmp()
	defer dm.BodyWidget.Invalidate()
	targetCard := dm.IconDragState.FindCardAtPoint(screenX, screenY)
	if targetCard != nil {
		dm.Manager.MoveItemToGroup(dm.FreeItemDragItem.Path, targetCard.GroupName())
		targetCard.Refresh()
	} else {
		var pt win.POINT
		pt.X = int32(screenX)
		pt.Y = int32(screenY)
		win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
		px := int(pt.X) - ui.TileWidth()/2
		py := int(pt.Y) - ui.TileHeight()/2
		wantCol, wantRow := pixelToGrid(px, py)
		col, row := dm.findFreeGridCell(dm.FreeItemDragItem.Path, wantCol, wantRow)
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(dm.FreeItemDragItem.Path, relPos)
	}
	dm.IconDragState.ClearDropState()
}

// checkFreeItemHover 调用 HoverState
func (dm *DesktopMode) checkFreeItemHover(x, y int) bool {
	return dm.HoverState.CheckFreeItemHover(x, y, dm.Manager.GetUngroupedItems(), dm.getFreeItemPixelPos)
}
