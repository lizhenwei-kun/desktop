package desktop

import (
	"image"
	"image/color"

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
	dm.WallpaperState.PaintWallpaper(canvas, bounds)
	dm.paintToolbar(canvas, bounds)
	dm.paintAllIcons(canvas, bounds)
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

// paintDragGhost 绘制统一拖拽幽灵（合并原 paintFreeItemDragGhost 和 PaintCardItemDragGhost）
func (dm *DesktopMode) paintDragGhost(canvas *walk.Canvas, _ walk.Rectangle) {
	if dm.GhostBmp == nil {
		return
	}
	ghostX := dm.DragMouseX - ui.TileWidth()/2
	ghostY := dm.DragMouseY - ui.TileHeight()/2
	iconX := ghostX + (ui.TileWidth()-ui.DesktopIconSize)/2
	iconY := ghostY + ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.GhostBmp, walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 128)
	font := ui.GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := dm.DragItemName
		lines := ui.GetIconDisplayLines(displayName, 4)
		labelTop := ghostY + ui.DesktopIconLabelTop
		drawIconLabel(canvas, font, lines, ghostX, labelTop, ui.TileWidth())
	}
}

// drawIconLabel 绘制带阴影的图标文字标签（白字 + 8 方向 1px 黑色阴影，模拟 Windows 原生桌面图标文字效果）
// x: 文本块左上角 X；labelTop: 第一行 Y；tileWidth: 文本块宽度
func drawIconLabel(canvas *walk.Canvas, font *walk.Font, lines []string, x, labelTop, tileWidth int) {
	if font == nil {
		return
	}
	// 8 个方向的阴影偏移：水平/垂直 + 对角线
	shadowOffsets := [8]struct{ dx, dy int }{
		{-1, -1}, {0, -1}, {1, -1},
		{-1, 0}, {1, 0},
		{-1, 1}, {0, 1}, {1, 1},
	}
	for i, line := range lines {
		lineY := labelTop + i*ui.DesktopIconLineHeight
		textBounds := walk.Rectangle{X: x, Y: lineY, Width: tileWidth, Height: ui.DesktopIconLineHeight}
		// 8 方向阴影各画 2 遍（加深、消除锯齿造成的浅灰感）
		for pass := 0; pass < 2; pass++ {
			for _, off := range shadowOffsets {
				shadowBounds := walk.Rectangle{
					X:      textBounds.X + off.dx,
					Y:      textBounds.Y + off.dy,
					Width:  textBounds.Width,
					Height: textBounds.Height,
				}
				canvas.DrawTextPixels(line, font, walk.RGB(0, 0, 0), shadowBounds, walk.TextCenter|walk.TextSingleLine)
			}
		}
		// 最后画白色正文
		canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
	}
}

// Refresh 刷新桌面模式
func (dm *DesktopMode) Refresh() {
	// 窗口不可见时跳过刷新，避免触发 WM_PAINT 导致窗口意外显示
	// 窗口重新显示时 showDesktopMode() 会调用 ReapplyCardPositionsAndRefresh() 完成完整刷新
	if !dm.MainWindow.Visible() {
		return
	}
	// 刷新所有卡片内容
	for _, card := range dm.Cards {
		card.Refresh()
	}
	// 预加载未分组图标的图标缓存
	ungrouped := dm.Manager.GetUngroupedItems()
	freePaths := make([]string, 0, len(ungrouped))
	for _, item := range ungrouped {
		freePaths = append(freePaths, item.Path)
	}
	ui.GlobalIconBmpCache.LoadAll(freePaths)

	dm.BodyWidget.Invalidate()
}

// ReapplyCardPositionsAndRefresh 重新应用卡片位置并强制完全重绘
// 用于窗口从隐藏变为可见后的完整刷新
func (dm *DesktopMode) ReapplyCardPositionsAndRefresh() {
	// 重新应用卡片位置和 Z 序
	dm.reapplyCardPositions()
	// 把 BodyWidget 在 Z 序中置顶，确保其上绘制的未分组图标不被卡片覆盖
	win.SetWindowPos(dm.BodyWidget.Handle(), win.HWND_TOP, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	// 刷新所有卡片内容
	for _, card := range dm.Cards {
		card.Refresh()
	}
	// 预加载未分组图标的图标缓存
	ungrouped := dm.Manager.GetUngroupedItems()
	freePaths := make([]string, 0, len(ungrouped))
	for _, item := range ungrouped {
		freePaths = append(freePaths, item.Path)
	}
	ui.GlobalIconBmpCache.LoadAll(freePaths)

	// 使 BodyWidget 无效化，触发重绘
	dm.WinAPI.InvalidateRect(win.HWND(dm.BodyWidget.Handle()))
	// 发送 WM_SIZE 让 walk 重新布局
	dm.WinAPI.SendWMSize(win.HWND(dm.BodyWidget.Handle()))
	// 强制立即重绘（不等待消息队列）
	dm.WinAPI.UpdateWindow(win.HWND(dm.BodyWidget.Handle()))
	// 同时强制主窗口立即重绘
	dm.WinAPI.UpdateWindow(win.HWND(dm.MainWindow.Handle()))

	// 再次确保 BodyWidget Z 序置顶（重绘和布局后可能被覆盖）
	win.SetWindowPos(dm.BodyWidget.Handle(), win.HWND_TOP, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
}
