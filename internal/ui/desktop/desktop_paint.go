package desktop

import (
	"image"
	"image/color"
	"syscall"

	"github.com/lxn/walk"
	"github.com/lxn/win"

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
	dm.paintWallpaper(canvas, bounds)
	dm.paintToolbar(canvas, bounds)
	dm.paintFreeItems(canvas, bounds)
	if dm.DropToDesktop {
		dm.paintDesktopDropHighlight(canvas, bounds)
	}
	if dm.FreeItemDragActive {
		dm.paintFreeItemDragGhost(canvas, bounds)
	}
	if dm.IconDragActive && dm.IconDragSourceCard != nil && !dm.FreeItemDragActive {
		dm.paintCardItemDragGhost(canvas, bounds)
	}
	if dm.DragOutlineCard != nil {
		dm.paintCardDragOutline(canvas, bounds)
	}
	return nil
}

func (dm *DesktopMode) drawResizeOutlineWin32(x, y, w, h int) {
	hdc := win.GetDC(0)
	if hdc == 0 {
		return
	}
	defer win.ReleaseDC(0, hdc)
	screenX := x + dm.WorkX
	screenY := y + dm.WorkY
	gdi32 := syscall.NewLazyDLL("gdi32.dll")
	procSetROP2 := gdi32.NewProc("SetROP2")
	procCreatePen := gdi32.NewProc("CreatePen")
	procGetStockObject := gdi32.NewProc("GetStockObject")
	procSetROP2.Call(uintptr(hdc), uintptr(3))
	pen, _, _ := procCreatePen.Call(uintptr(0), uintptr(2), uintptr(win.RGB(0xFF, 0xFF, 0xFF)))
	if pen == 0 {
		return
	}
	defer gdi32.NewProc("DeleteObject").Call(pen)
	oldPen := win.SelectObject(hdc, win.HGDIOBJ(pen))
	defer win.SelectObject(hdc, oldPen)
	hollowBrush, _, _ := procGetStockObject.Call(uintptr(5))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(hollowBrush))
	defer win.SelectObject(hdc, oldBrush)
	win.MoveToEx(hdc, screenX, screenY, nil)
	win.LineTo(hdc, int32(screenX+w), int32(screenY))
	win.LineTo(hdc, int32(screenX+w), int32(screenY+h))
	win.LineTo(hdc, int32(screenX), int32(screenY+h))
	win.LineTo(hdc, int32(screenX), int32(screenY))
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

func (dm *DesktopMode) loadWallpaper() {
	wallpaperPath := ui.GetCurrentWallpaper()
	logger.Debug("loadWallpaper: path=%q", wallpaperPath)
	if wallpaperPath == "" {
		return
	}
	if dm.WallpaperBmp != nil {
		dm.WallpaperBmp.Dispose()
		dm.WallpaperBmp = nil
	}
	img := ui.LoadWallpaperImage(dm.WorkW, dm.WorkH)
	if img == nil {
		logger.Debug("loadWallpaper: LoadWallpaperImage 返回 nil，回退到 GDI+ 加载")
		dpi := dm.MainWindow.DPI()
		if dpi <= 0 {
			dpi = 96
		}
		bmp, err := walk.NewBitmapFromFileForDPI(wallpaperPath, dpi)
		if err != nil {
			logger.Debug("loadWallpaper: GDI+ 也失败: %v", err)
			return
		}
		dm.WallpaperBmp = bmp
		return
	}
	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		logger.Debug("loadWallpaper: NewBitmapFromImageForDPI failed: %v", err)
		return
	}
	dm.WallpaperBmp = bmp
}

func (dm *DesktopMode) paintWallpaper(canvas *walk.Canvas, bounds walk.Rectangle) {
	if dm.WallpaperBmp != nil {
		canvas.DrawBitmapWithOpacityPixels(dm.WallpaperBmp, bounds, 255)
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

func (dm *DesktopMode) paintFreeItemDragGhost(canvas *walk.Canvas, _ walk.Rectangle) {
	if dm.GhostBmp == nil {
		return
	}
	ghostX := dm.FreeItemDragMouseX - ui.TileWidth()/2
	ghostY := dm.FreeItemDragMouseY - ui.TileHeight()/2
	iconX := ghostX + (ui.TileWidth()-ui.DesktopIconSize)/2
	iconY := ghostY + ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.GhostBmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 128)
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

func (dm *DesktopMode) paintCardItemDragGhost(canvas *walk.Canvas, _ walk.Rectangle) {
	if dm.GhostBmp == nil {
		return
	}
	var pt win.POINT
	pt.X = int32(dm.IconDragScreenX)
	pt.Y = int32(dm.IconDragScreenY)
	win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
	ghostX := int(pt.X) - ui.TileWidth()/2
	ghostY := int(pt.Y) - ui.TileHeight()/2
	iconX := ghostX + (ui.TileWidth()-ui.DesktopIconSize)/2
	iconY := ghostY + ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.GhostBmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 128)
	font := ui.GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := dm.IconDragItem.Name
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

func (dm *DesktopMode) paintCardDragOutline(canvas *walk.Canvas, bounds walk.Rectangle) {
	var tl, br win.POINT
	tl.X = int32(dm.DragOutlineX)
	tl.Y = int32(dm.DragOutlineY)
	br.X = int32(dm.DragOutlineX + dm.DragOutlineW)
	br.Y = int32(dm.DragOutlineY + dm.DragOutlineH)
	win.ScreenToClient(dm.BodyWidget.Handle(), &tl)
	win.ScreenToClient(dm.BodyWidget.Handle(), &br)
	rect := walk.Rectangle{X: int(tl.X), Y: int(tl.Y), Width: int(br.X - tl.X), Height: int(br.Y - tl.Y)}
	pen, err := walk.NewCosmeticPen(walk.PenDash, walk.RGB(0xFF, 0xFF, 0xFF))
	if err != nil {
		return
	}
	defer pen.Dispose()
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y + rect.Height}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X, Y: rect.Y}, walk.Point{X: rect.X, Y: rect.Y + rect.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: rect.X + rect.Width, Y: rect.Y}, walk.Point{X: rect.X + rect.Width, Y: rect.Y + rect.Height})
}

func (dm *DesktopMode) loadDragGhostBmp(filePath string) {
	dm.disposeDragGhostBmp()
	dm.GhostBmp = ui.GlobalIconBmpCache.GetOrLoad(filePath)
}

func (dm *DesktopMode) disposeDragGhostBmp() {
	dm.GhostBmp = nil
}

// Refresh 刷新桌面模式
func (dm *DesktopMode) Refresh() {
	for _, card := range dm.Cards {
		card.Refresh()
	}
	dm.BodyWidget.Invalidate()
}
