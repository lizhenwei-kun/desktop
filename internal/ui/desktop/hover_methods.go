package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// checkItemHover 检测鼠标悬停的桌面图标（未分组 + 卡片内图标）。
// 未分组与分组共用全局 Hovered：未分组在此精确检测，卡片内图标由卡片自己的
// MouseMove 通过 SetHovered 写入全局状态。鼠标在卡片区域内时这里不干预悬停状态。
func (dm *DesktopMode) checkItemHover(x, y int) bool {
	// 检查未分组项目
	newHovered := ui.Selection{}
	ungrouped := dm.Manager.GetUngroupedItems()
	for i, item := range ungrouped {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			newHovered = ui.Selection{Path: item.Path}
			break
		}
	}

	if newHovered.Path == "" {
		// 未命中未分组图标：检查是否在任意卡片区域内
		if dm.isPointInAnyCardRegion(x, y) {
			// 鼠标在卡片区域内，卡片内悬停由卡片自己写入全局 Hovered，
			// 这里不干预，直接返回（避免覆盖卡片刚设置的悬停状态）
			return false
		}
	}

	// 通过 SetHovered 设置/清除
	if newHovered != dm.Hovered {
		dm.SetHovered(newHovered)
		return true
	}
	return false
}

// isPointInAnyCardRegion 判断客户区坐标是否在任意卡片范围内（仅区域判断，无选中副作用）
func (dm *DesktopMode) isPointInAnyCardRegion(cx, cy int) bool {
	var pt win.POINT
	pt.X = int32(cx)
	pt.Y = int32(cy)
	win.ClientToScreen(dm.BodyWidget.Handle(), &pt)
	sx := int(pt.X)
	sy := int(pt.Y)
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if sx >= sb.X && sx <= sb.X+sb.Width &&
			sy >= sb.Y && sy <= sb.Y+sb.Height {
			return true
		}
	}
	return false
}

// paintAllIcons 绘制所有桌面图标（未分组项目）
// 分组项目的图标由 GroupCard 的 bodyWidget 自行绘制
func (dm *DesktopMode) paintAllIcons(canvas *walk.Canvas, bounds walk.Rectangle) {
	items := dm.Manager.GetUngroupedItems()
	if len(items) == 0 {
		logger.Debug("paintAllIcons: no ungrouped items")
		return
	}
	logger.Debug("paintAllIcons: %d ungrouped items, bounds=%dx%d", len(items), bounds.Width, bounds.Height)

	effectiveH := bounds.Height
	if effectiveH < 100 {
		effectiveH = dm.WorkH
	}

	for idx, item := range items {
		px, py := dm.getFreeItemPixelPos(item.Path, idx)
		logger.Debug("paintAllIcons: item[%d] %q pos=(%d,%d) tile=%dx%d effH=%d", idx, item.Name, px, py, ui.TileWidth(), ui.TileHeight(), effectiveH)
		if py+ui.TileHeight() > effectiveH {
			logger.Debug("paintAllIcons: item[%d] %q SKIPPED (out of bounds)", idx, item.Name)
			continue
		}

		// 判断当前项目的选中/悬停/编辑状态（基于路径）
		isSelected := item.Path == dm.Selected.Path
		isHovered := item.Path == dm.Hovered.Path
		isEditing := item.Path == dm.EditingPath

		// 预先计算文字行数，选中/悬停时框需要包含全部显示文字
		displayName := item.Name
		lines := ui.SplitTextToLines(displayName, 4)
		selH := ui.TileHeight()
		if isSelected {
			selH = ui.DesktopIconLabelTop() + len(lines)*ui.DesktopIconLineHeight() + 4
		} else if isHovered {
			// 悬停只显示最多 2 行（GetIconDisplayLines），框高按实际显示行数，
			// 避免短名称（1 行）时磁贴底部留出整行空白
			hoverLines := ui.GetIconDisplayLines(displayName, 4)
			selH = ui.DesktopIconLabelTop() + len(hoverLines)*ui.DesktopIconLineHeight() + 4
		}

		// 绘制选中/悬停高亮
		if isSelected {
			ui.DrawSelectionRect(canvas, walk.Rectangle{X: px, Y: py, Width: ui.TileWidth(), Height: selH})
		} else if isHovered {
			ui.DrawHoverRect(canvas, walk.Rectangle{X: px, Y: py, Width: ui.TileWidth(), Height: selH})
		}

		// 绘制图标
		bmp := ui.GlobalIconBmpCache.GetOrLoad(item.Path)
		logger.Debug("paintAllIcons: item[%d] bmp=%v", idx, bmp != nil)
		if bmp != nil {
			iconX := px + (ui.TileWidth()-ui.DesktopIconSize())/2
			iconY := py + ui.DesktopIconTop()
			canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize(), Height: ui.DesktopIconSize()}, 255)
		}

		// 编辑模式下不绘制文字标签（由编辑框显示）
		if isEditing {
			continue
		}

		// 绘制文字标签
		font := ui.GetIconFont()
		if font != nil {
			defer font.Dispose()
			labelTop := py + ui.DesktopIconLabelTop()
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
