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

func (dm *DesktopMode) posToGrid(pos config.Position) (int, int) {
	px := int(pos.X * float64(dm.WorkW))
	py := int(pos.Y * float64(dm.WorkH))
	return pixelToGrid(px, py)
}

func (dm *DesktopMode) gridToRel(col, row int) config.Position {
	px, py := gridToPixel(col, row)
	return config.Position{
		X: float64(px) / float64(dm.WorkW),
		Y: float64(py) / float64(dm.WorkH),
	}
}

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

func (dm *DesktopMode) paintFreeItems(canvas *walk.Canvas, bounds walk.Rectangle) {
	items := dm.Manager.GetUngroupedItems()
	if len(items) == 0 {
		return
	}
	effectiveH := bounds.Height
	if effectiveH < 100 {
		effectiveH = dm.WorkH
	}
	for idx, item := range items {
		px, py := dm.getFreeItemPixelPos(item.Path, idx)
		if py+ui.TileHeight() > effectiveH {
			continue
		}
		if idx == dm.HoveredFreeIdx {
			ui.DrawHoverRect(canvas, walk.Rectangle{X: px, Y: py, Width: ui.TileWidth(), Height: ui.TileHeight()})
		}
		bmp := ui.GlobalIconBmpCache.GetOrLoad(item.Path)
		if bmp != nil {
			iconX := px + (ui.TileWidth()-ui.DesktopIconSize)/2
			iconY := py + ui.DesktopIconTop
			canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 255)
		}
		font := ui.GetIconFont()
		if font != nil {
			defer font.Dispose()
			displayName := item.Name
			lines := ui.SplitTextToLines(displayName, 4)
			labelTop := py + ui.DesktopIconLabelTop
			for i, line := range lines {
				if i >= 2 {
					break
				}
				if i == 1 && len(lines) > 2 {
					line = ui.TruncateText(line, 3)
				}
				lineY := labelTop + i*ui.DesktopIconLineHeight
				textBounds := walk.Rectangle{X: px, Y: lineY, Width: ui.TileWidth(), Height: ui.DesktopIconLineHeight}
				canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
			}
		}
	}
}

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

func (dm *DesktopMode) checkFreeItemDragStart() {
	defer recoverGoroutine("checkFreeItemDragStart")
	time.Sleep(ui.IconDragDelay)
	dm.BodyWidget.Synchronize(func() {
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
		dm.loadDragGhostBmp(dm.FreeItemDragItem.Path)
		dm.LastDragMoveTime = time.Now()
		dm.BodyWidget.Invalidate()
		win.SetCapture(dm.BodyWidget.Handle())
		dm.IconDragActive = true
		dm.IconDragItem = dm.FreeItemDragItem
		dm.IconDragSourceGroup = ""
		dm.IconDragScreenX = int(screenPt.X)
		dm.IconDragScreenY = int(screenPt.Y)
	})
}

func (dm *DesktopMode) handleFreeItemDrop(screenX, screenY int) {
	dm.IconDragActive = false
	dm.disposeDragGhostBmp()
	defer dm.BodyWidget.Invalidate()
	targetCard := dm.findCardAtPoint(screenX, screenY)
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
	dm.clearDropState()
}

func (dm *DesktopMode) checkFreeItemHover(x, y int) bool {
	items := dm.Manager.GetUngroupedItems()
	newIdx := -1
	for i := range items {
		ix, iy := dm.getFreeItemPixelPos(items[i].Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			newIdx = i
			break
		}
	}
	if newIdx != dm.HoveredFreeIdx {
		dm.HoveredFreeIdx = newIdx
		return true
	}
	return false
}
