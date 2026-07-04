package ui

import (
	"image"

	"github.com/lxn/walk"
)

// GlobalIconBmpCache 全局图标 bitmap 缓存
// 所有访问均在 walk 主 UI 线程中，无需加锁
var GlobalIconBmpCache = newIconBmpCache()

// IconBmpCache 图标 bitmap 缓存
type IconBmpCache struct {
	cache map[string]*walk.Bitmap
}

func newIconBmpCache() *IconBmpCache {
	return &IconBmpCache{
		cache: make(map[string]*walk.Bitmap),
	}
}

// Get 获取缓存的 bitmap，未缓存时返回 nil
func (c *IconBmpCache) Get(path string) *walk.Bitmap {
	return c.cache[path]
}

// GetOrLoad 获取缓存的 bitmap，不存在则提取并缓存
func (c *IconBmpCache) GetOrLoad(path string) *walk.Bitmap {
	if bmp, ok := c.cache[path]; ok {
		return bmp
	}
	return c.extractAndCache(path)
}

// LoadAll 批量预加载图标到缓存
func (c *IconBmpCache) LoadAll(paths []string) {
	extractor := NewIconExtractor()
	for _, path := range paths {
		if _, ok := c.cache[path]; ok {
			continue
		}
		iconImg, err := extractor.GetIconImage(path)
		if err != nil || iconImg == nil {
			continue
		}
		bmp := imageToBitmap(iconImg)
		if bmp == nil {
			continue
		}
		c.cache[path] = bmp
	}
}

// Remove 移除并释放单个缓存
func (c *IconBmpCache) Remove(path string) {
	if bmp, ok := c.cache[path]; ok {
		bmp.Dispose()
		delete(c.cache, path)
	}
}

// Clear 清理并释放所有缓存
func (c *IconBmpCache) Clear() {
	for _, bmp := range c.cache {
		bmp.Dispose()
	}
	c.cache = make(map[string]*walk.Bitmap)
}

// extractAndCache 提取图标并加入缓存
func (c *IconBmpCache) extractAndCache(path string) *walk.Bitmap {
	extractor := NewIconExtractor()
	iconImg, err := extractor.GetIconImage(path)
	if err != nil || iconImg == nil {
		return nil
	}
	bmp := imageToBitmap(iconImg)
	if bmp == nil {
		return nil
	}
	c.cache[path] = bmp
	return bmp
}

// imageToBitmap 将 image.Image 转为 walk.Bitmap
func imageToBitmap(img image.Image) *walk.Bitmap {
	rgbaImg, ok := img.(*image.RGBA)
	if !ok {
		b := img.Bounds()
		rgbaImg = image.NewRGBA(b)
		for iy := b.Min.Y; iy < b.Max.Y; iy++ {
			for ix := b.Min.X; ix < b.Max.X; ix++ {
				rgbaImg.Set(ix, iy, img.At(ix, iy))
			}
		}
	}
	bmp, err := walk.NewBitmapFromImage(rgbaImg)
	if err != nil {
		return nil
	}
	return bmp
}
