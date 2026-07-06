package desktop

import (
	"image"
	"image/color"

	"github.com/lxn/walk"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

var paintCount int

func (dm *DesktopMode) paintDesktop(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := dm.BodyWidget.ClientBoundsPixels()
	paintCount++
	if paintCount <= 3 {
		logger.Debug("paintDesktop #%d: bounds=(%d,%d,%dx%d), wallpaperBmp=%v",
			paintCount, bounds.X, bounds.Y, bounds.Width, bounds.Height, dm.WallpaperBmp != nil)
	}
	dm.paintBackground(canvas, bounds)
	dm.WallpaperState.PaintWallpaper(canvas, bounds)
	dm.paintToolbar(canvas, bounds)
	dm.HoverState.PaintFreeItems(canvas, bounds, dm.Manager.GetUngroupedItems(), dm.WorkH, dm.getFreeItemPixelPos, dm.SelectedFreeIdx, dm.EditingFreeIdx)
	if dm.DropToDesktop {
		dm.paintDesktopDropHighlight(canvas, bounds)
	}
	if dm.FreeItemDragActive {
		ghost := dm.IconDragState.GhostBmp
		dm.paintFreeItemDragGhost(canvas, ghost, bounds)
	}
	if dm.IconDragActive && dm.IconDragSourceCard != nil && !dm.FreeItemDragActive {
		dm.IconDragState.PaintCardItemDragGhost(canvas)
	}
	if dm.DragOutlineCard != nil {
		dm.CardDragOutline.PaintCardDragOutline(canvas, dm.BodyWidget)
	}
	return nil
}

func (dm *DesktopMode) paintBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	bgColor := color.RGBA{R: 0x1A, G: 0x1A, B: 0x2E, A: 0xFF}
	bgImg := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			bgImg.SetRGBA(x, y, bgColor)
		}
	}
	bmp, err := walk.NewBitmapFromImage(bgImg)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
}

func (dm *DesktopMode) paintToolbar(canvas *walk.Canvas, bounds walk.Rectangle) {
	font, _ := walk.NewFont("Microsoft YaHei", 14, 0)
	if font == nil {
		return
	}
	defer font.Dispose()
	toolbarBounds := walk.Rectangle{X: bounds.Width - 140, Y: 10, Width: 120, Height: 30}
	btnColor := color.RGBA{R: 0x30, G: 0x34, B: 0x3C, A: 0xBD}
	btnImg := image.NewRGBA(image.Rect(0, 0, toolbarBounds.Width, toolbarBounds.Height))
	for y := 0; y < toolbarBounds.Height; y++ {
		for x := 0; x < toolbarBounds.Width; x++ {
			btnImg.SetRGBA(x, y, btnColor)
		}
	}
	btnBmp, err := walk.NewBitmapFromImage(btnImg)
	if err == nil {
		defer btnBmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(btnBmp, toolbarBounds, byte(btnColor.A))
	}
	canvas.DrawTextPixels("+ 添加卡片", font, walk.RGB(0xFF, 0xFF, 0xFF),
		toolbarBounds, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
}

func (dm *DesktopMode) paintDesktopDropHighlight(canvas *walk.Canvas, bounds walk.Rectangle) {
	startX := bounds.Width - ui.TileWidth() - 24
	startY := 56
	w := ui.TileWidth() + 8
	items := dm.Manager.GetUngroupedItems()
	h := len(items)*ui.TileHeight() + 8
	if h < 60 {
		h = ui.TileHeight() + 8
	}
	rect := walk.Rectangle{X: startX, Y: startY, Width: w, Height: h}
	pen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x4A, 0xA0, 0xFF))
	if err != nil {
		return
	}
	defer pen.Dispose()
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y + rect.Height}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X + rect.Width, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
}

// paintFreeItemDragGhost 保留在 DesktopMode 因为混用 FreeItemDragState 和 IconDragState 的字段
func (dm *DesktopMode) paintFreeItemDragGhost(canvas *walk.Canvas, ghostBmp *walk.Bitmap, _ walk.Rectangle) {
	if ghostBmp == nil {
		return
	}
	ghostX := dm.FreeItemDragMouseX - ui.TileWidth()/2
	ghostY := dm.FreeItemDragMouseY - ui.TileHeight()/2
	iconX := ghostX + (ui.TileWidth()-ui.DesktopIconSize)/2
	iconY := ghostY + ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(ghostBmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 128)
	font := ui.GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := dm.FreeItemDragItem.Name
		lines := ui.SplitTextToLines(displayName, 4)
		labelTop := ghostY + ui.DesktopIconLabelTop
		for i, line := range lines {
			if i >= 2 {
				break
			}
			if i == 1 && len(lines) > 2 {
				line = ui.TruncateText(line, 3)
			}
			lineY := labelTop + i*ui.DesktopIconLineHeight
			textBounds := walk.Rectangle{X: ghostX, Y: lineY, Width: ui.TileWidth(), Height: ui.DesktopIconLineHeight}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
	}
}

// Refresh 刷新桌面模式
func (dm *DesktopMode) Refresh() {
	for _, card := range dm.Cards {
		card.Refresh()
	}
	dm.BodyWidget.Invalidate()
}
