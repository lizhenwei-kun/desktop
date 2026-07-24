package desktop

import (
	"image"
	"image/draw"

	"github.com/lxn/walk"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// LoadWallpaper 按全屏尺寸加载壁纸，并裁剪出工作区部分，确保 1:1 绘制不缩放
func (s *WallpaperState) LoadWallpaper(dpiFn func() int, screenW, screenH, workX, workY, workW, workH int) {
	path := ui.GetCurrentWallpaper()
	logger.Debug("loadWallpaper: path=%q, screen=%dx%d, work=(%d,%d,%dx%d)", path, screenW, screenH, workX, workY, workW, workH)
	if path == "" {
		return
	}
	if s.WallpaperBmp != nil {
		s.WallpaperBmp.Dispose()
		s.WallpaperBmp = nil
	}
	// 按全屏尺寸加载壁纸（Fill 模式）
	img := ui.LoadWallpaperImage(screenW, screenH)
	if img == nil {
		logger.Debug("loadWallpaper: LoadWallpaperImage 返回 nil，回退到 GDI+ 加载")
		dpi := dpiFn()
		if dpi <= 0 {
			dpi = 96
		}
		fullBmp, err := walk.NewBitmapFromFileForDPI(path, dpi)
		if err != nil {
			logger.Debug("loadWallpaper: GDI+ 也失败: %v", err)
			return
		}
		// GDI+ 加载后裁剪工作区部分
		fullImg, err := fullBmp.ToImage()
		fullBmp.Dispose()
		if err != nil {
			return
		}
		s.WallpaperBmp = cropBitmapFromImage(fullImg, workX, workY, workW, workH)
		return
	}
	// 从全屏 image 中裁剪工作区部分
	s.WallpaperBmp = cropBitmapFromImage(img, workX, workY, workW, workH)
}

// PaintWallpaper 绘制壁纸
// WallpaperBmp 已裁剪为工作区大小，1:1 绘制，不缩放
func (s *WallpaperState) PaintWallpaper(canvas *walk.Canvas, bounds walk.Rectangle) {
	if s.WallpaperBmp != nil {
		canvas.DrawBitmapWithOpacityPixels(s.WallpaperBmp, bounds, 255)
	}
}

// LoadWallpaperSimple 无 DPI 的简易加载（供右键刷新等场景使用）
func (s *WallpaperState) LoadWallpaperSimple(screenW, screenH int) {
	path := ui.GetCurrentWallpaper()
	if path == "" {
		return
	}
	if s.WallpaperBmp != nil {
		s.WallpaperBmp.Dispose()
		s.WallpaperBmp = nil
	}
	img := ui.LoadWallpaperImage(screenW, screenH)
	if img == nil {
		return
	}
	bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		return
	}
	s.WallpaperBmp = bmp
}

// cropBitmapFromImage 从 image.Image 中裁剪指定区域，返回新 bitmap
func cropBitmapFromImage(src image.Image, x, y, w, h int) *walk.Bitmap {
	cropRect := image.Rect(x, y, x+w, y+h)
	cropped := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(cropped, cropped.Bounds(), src, cropRect.Min, draw.Src)
	bmp, err := walk.NewBitmapFromImageForDPI(cropped, 96)
	if err != nil {
		return nil
	}
	return bmp
}
