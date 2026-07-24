package desktop

import (
	"github.com/lxn/walk"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// LoadWallpaper 按工作区物理像素尺寸加载壁纸，确保 1:1 绘制不缩放。
// 注意：LoadWallpaperImage 返回的图像像素尺寸已是 workW×workH（物理像素，与 DPI 无关），
// 因此必须用 NewBitmapFromImage（1:1），绝不能再用 NewBitmapFromImageForDPI 按 DPI 二次解释，
// 否则非 96 DPI 下位图会被再次缩放，导致多次刷新时画面跳动、颜色因重采样变化。
func (s *WallpaperState) LoadWallpaper(dpiFn func() int, workW, workH int) {
	path := ui.GetCurrentWallpaper()
	logger.Debug("loadWallpaper: path=%q, work=%dx%d", path, workW, workH)
	if path == "" {
		return
	}
	img := ui.LoadWallpaperImage(workW, workH)
	var bmp *walk.Bitmap
	if img == nil {
		logger.Debug("loadWallpaper: LoadWallpaperImage 返回 nil，回退到 GDI+ 加载")
		dpi := dpiFn()
		if dpi <= 0 {
			dpi = 96
		}
		b, err := walk.NewBitmapFromFileForDPI(path, dpi)
		if err != nil {
			logger.Debug("loadWallpaper: GDI+ 也失败: %v", err)
			return
		}
		bmp = b
	} else {
		b, err := walk.NewBitmapFromImage(img)
		if err != nil {
			logger.Debug("loadWallpaper: NewBitmapFromImage failed: %v", err)
			return
		}
		bmp = b
	}
	s.swapBitmap(bmp)
}

// swapBitmap 线程安全地替换缓存的壁纸位图：先 Dispose 旧位图，再赋值新位图。
// 加锁避免异步 Work.Post 加载与 UI 绘制线程读 WallpaperBmp 指针竞争。
func (s *WallpaperState) swapBitmap(bmp *walk.Bitmap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WallpaperBmp != nil {
		s.WallpaperBmp.Dispose()
	}
	s.WallpaperBmp = bmp
}

// getBitmap 线程安全地读取当前壁纸位图
func (s *WallpaperState) getBitmap() *walk.Bitmap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.WallpaperBmp
}

// HasWallpaper 线程安全地判断当前是否已加载壁纸（供跨包日志/调试使用）
func (s *WallpaperState) HasWallpaper() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.WallpaperBmp != nil
}

// PaintWallpaper 绘制壁纸
// WallpaperBmp 已为工作区大小，1:1 绘制，不缩放
func (s *WallpaperState) PaintWallpaper(canvas *walk.Canvas, bounds walk.Rectangle) {
	if bmp := s.getBitmap(); bmp != nil {
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
}

// LoadWallpaperSimple 无 DPI 的简易加载（供右键刷新等场景使用）
func (s *WallpaperState) LoadWallpaperSimple(workW, workH int) {
	path := ui.GetCurrentWallpaper()
	if path == "" {
		return
	}
	img := ui.LoadWallpaperImage(workW, workH)
	if img == nil {
		return
	}
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	s.swapBitmap(bmp)
}
