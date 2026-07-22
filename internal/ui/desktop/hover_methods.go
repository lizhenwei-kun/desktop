package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// checkItemHover 检测鼠标悬停的桌面图标（未分组 + 卡片内图标）
// 对于卡片内图标：简单检测鼠标是否在卡片范围内
// 对于未分组图标：精确检测鼠标是否在图标磁贴内
func (dm *DesktopMode) checkItemHover(x, y int) bool {
	prevHovered := dm.HoveredPath
	dm.HoveredPath = ""

	// 检查未分组项目
	ungrouped := dm.Manager.GetUngroupedItems()
	for i, item := range ungrouped {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			dm.HoveredPath = item.Path
			break
		}
	}

	// 如果未分组未命中，检查是否在任意卡片范围内（简化处理，不精确到单个图标）
	if dm.HoveredPath == "" {
		for _, card := range dm.Cards {
			sb := card.ScreenBounds()
			var pt win.POINT
			pt.X = int32(x)
			pt.Y = int32(y)
			win.ClientToScreen(dm.BodyWidget.Handle(), &pt)
			sx := int(pt.X)
			sy := int(pt.Y)
			if sx >= sb.X && sx <= sb.X+sb.Width &&
				sy >= sb.Y && sy <= sb.Y+sb.Height {
				// 鼠标在卡片区域内，但卡片内图标由卡片自己管理悬停
				// 这里不清除 HoveredPath，保持为空即可（卡片区域不设置桌面级悬停）
				break
			}
		}
	}

	return dm.HoveredPath != prevHovered
}

// paintAllIcons 绘制所有桌面图标（未分组项目）
// 分组项目的图标由 GroupCard 的 bodyWidget 自行绘制
func (dm *DesktopMode) paintAllIcons(canvas *walk.Canvas, bounds walk.Rectangle) {
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

		// 判断当前项目的选中/悬停/编辑状态（基于路径）
		isSelected := item.Path == dm.SelectedPath
		isHovered := item.Path == dm.HoveredPath
		isEditing := item.Path == dm.EditingPath

		// 预先计算文字行数，选中时框需要包含所有文字
		displayName := item.Name
		lines := ui.SplitTextToLines(displayName, 4)
		selH := ui.TileHeight()
		if isSelected {
			selH = ui.DesktopIconLabelTop + len(lines)*ui.DesktopIconLineHeight + 4
		}

		// 绘制选中/悬停高亮
		if isSelected {
			ui.DrawSelectionRect(canvas, walk.Rectangle{X: px, Y: py, Width: ui.TileWidth(), Height: selH})
		} else if isHovered {
			ui.DrawHoverRect(canvas, walk.Rectangle{X: px, Y: py, Width: ui.TileWidth(), Height: ui.TileHeight()})
		}

		// 绘制图标
		bmp := ui.GlobalIconBmpCache.GetOrLoad(item.Path)
		if bmp != nil {
			iconX := px + (ui.TileWidth()-ui.DesktopIconSize)/2
			iconY := py + ui.DesktopIconTop
			canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 255)
		}

		// 编辑模式下不绘制文字标签（由编辑框显示）
		if isEditing {
			continue
		}

		// 绘制文字标签
		font := ui.GetIconFont()
		if font != nil {
			defer font.Dispose()
			labelTop := py + ui.DesktopIconLabelTop
		if isSelected {
			// 选中状态：显示所有行，不加省略号
			drawIconLabel(canvas, font, lines, px, labelTop, ui.TileWidth())
		} else {
			// 非选中：最多显示2行，超出省略
			displayLines := ui.GetIconDisplayLines(displayName, 4)
			drawIconLabel(canvas, font, displayLines, px, labelTop, ui.TileWidth())
		}
		}
	}
}

// IsPointInItem 判断屏幕坐标是否在任意桌面图标的范围内
func (dm *DesktopMode) IsPointInItem(screenX, screenY int) bool {
	var pt win.POINT
	pt.X = int32(screenX)
	pt.Y = int32(screenY)
	win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
	cx := int(pt.X)
	cy := int(pt.Y)

	// 检查未分组项目
	ungrouped := dm.Manager.GetUngroupedItems()
	for i, item := range ungrouped {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if cx >= ix && cx <= ix+ui.TileWidth() &&
			cy >= iy && cy <= iy+ui.TileHeight() {
			return true
		}
	}

	// 检查分组卡片范围
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			return true
		}
	}

	return false
}
