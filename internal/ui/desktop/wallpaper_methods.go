package desktop

import (
	"github.com/lxn/walk"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// LoadWallpaper 加载壁纸（需要外部传入 MainWindow 和屏幕尺寸）
func (s *WallpaperState) LoadWallpaper(dpiFn func() int, workW, workH int) {
	path := ui.GetCurrentWallpaper()
	logger.Debug("loadWallpaper: path=%q", path)
	if path == "" {
		return
	}
	if s.WallpaperBmp != nil {
		s.WallpaperBmp.Dispose()
		s.WallpaperBmp = nil
	}
	img := ui.LoadWallpaperImage(workW, workH)
	if img == nil {
		logger.Debug("loadWallpaper: LoadWallpaperImage 返回 nil，回退到 GDI+ 加载")
		dpi := dpiFn()
		if dpi <= 0 {
			dpi = 96
		}
		bmp, err := walk.NewBitmapFromFileForDPI(path, dpi)
		if err != nil {
			logger.Debug("loadWallpaper: GDI+ 也失败: %v", err)
			return
		}
		s.WallpaperBmp = bmp
		return
	}
	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		logger.Debug("loadWallpaper: NewBitmapFromImageForDPI failed: %v", err)
		return
	}
	s.WallpaperBmp = bmp
}

// PaintWallpaper 绘制壁纸
func (s *WallpaperState) PaintWallpaper(canvas *walk.Canvas, bounds walk.Rectangle) {
	if s.WallpaperBmp != nil {
		canvas.DrawBitmapWithOpacityPixels(s.WallpaperBmp, bounds, 255)
	}
}

// LoadWallpaperSimple 无 DPI 的简易加载（供右键刷新等场景使用）
func (s *WallpaperState) LoadWallpaperSimple(workW, workH int) {
	path := ui.GetCurrentWallpaper()
	if path == "" {
		return
	}
	if s.WallpaperBmp != nil {
		s.WallpaperBmp.Dispose()
		s.WallpaperBmp = nil
	}
	img := ui.LoadWallpaperImage(workW, workH)
	if img == nil {
		return
	}
	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		return
	}
	s.WallpaperBmp = bmp
}
