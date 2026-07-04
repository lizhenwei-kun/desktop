package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/ui"
)

// CheckFreeItemHover 检测悬停的未分组图标
func (s *HoverState) CheckFreeItemHover(x, y int, items []group.GroupItem, getPixelPos func(string, int) (int, int)) bool {
	newIdx := -1
	for i, item := range items {
		ix, iy := getPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			newIdx = i
			break
		}
	}
	if newIdx != s.HoveredFreeIdx {
		s.HoveredFreeIdx = newIdx
		return true
	}
	return false
}

// PaintFreeItems 绘制未分组图标
func (s *HoverState) PaintFreeItems(canvas *walk.Canvas, bounds walk.Rectangle, items []group.GroupItem, workH int, getPixelPos func(string, int) (int, int)) {
	if len(items) == 0 {
		return
	}
	effectiveH := bounds.Height
	if effectiveH < 100 {
		effectiveH = workH
	}
	for idx, item := range items {
		px, py := getPixelPos(item.Path, idx)
		if py+ui.TileHeight() > effectiveH {
			continue
		}
		if idx == s.HoveredFreeIdx {
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

// IsPointInUngroupedArea 判断屏幕坐标是否在未分组图标区域内
func (s *HoverState) IsPointInUngroupedArea(screenX, screenY int, bodyWidget *walk.CustomWidget, items []group.GroupItem, getPixelPos func(string, int) (int, int)) bool {
	var pt win.POINT
	pt.X = int32(screenX)
	pt.Y = int32(screenY)
	win.ScreenToClient(bodyWidget.Handle(), &pt)
	cx := int(pt.X)
	cy := int(pt.Y)
	for i, item := range items {
		ix, iy := getPixelPos(item.Path, i)
		if cx >= ix && cx <= ix+ui.TileWidth() &&
			cy >= iy && cy <= iy+ui.TileHeight() {
			return true
		}
	}
	return false
}
