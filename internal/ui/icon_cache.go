package ui

import (
	"image"
	"sync"

	"github.com/lxn/walk"
)

// globalIconBmpCache 全局图标 bitmap 缓存，避免每次重绘重复文件 I/O
var globalIconBmpCache = newIconBmpCache()

// iconBmpCache 图标 bitmap 缓存
type iconBmpCache struct {
	mu    sync.RWMutex
	cache map[string]*walk.Bitmap
}

func newIconBmpCache() *iconBmpCache {
	return &iconBmpCache{
		cache: make(map[string]*walk.Bitmap),
	}
}

// Get 获取缓存的 bitmap，未缓存时返回 nil
func (c *iconBmpCache) Get(path string) *walk.Bitmap {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[path]
}

// GetOrLoad 获取缓存的 bitmap，不存在则提取并缓存
func (c *iconBmpCache) GetOrLoad(path string) *walk.Bitmap {
	c.mu.RLock()
	if bmp, ok := c.cache[path]; ok {
		c.mu.RUnlock()
		return bmp
	}
	c.mu.RUnlock()

	// cache miss: 提取
	bmp := c.extractAndCache(path)
	return bmp
}

// LoadAll 批量预加载图标到缓存
func (c *iconBmpCache) LoadAll(paths []string) {
	extractor := NewIconExtractor()
	for _, path := range paths {
		c.mu.RLock()
		if _, ok := c.cache[path]; ok {
			c.mu.RUnlock()
			continue
		}
		c.mu.RUnlock()

		iconImg, err := extractor.GetIconImage(path)
		if err != nil || iconImg == nil {
			continue
		}
		bmp := imageToBitmap(iconImg)
		if bmp == nil {
			continue
		}
		c.mu.Lock()
		c.cache[path] = bmp
		c.mu.Unlock()
	}
}

// Remove 移除并释放单个缓存
func (c *iconBmpCache) Remove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if bmp, ok := c.cache[path]; ok {
		bmp.Dispose()
		delete(c.cache, path)
	}
}

// Clear 清理并释放所有缓存
func (c *iconBmpCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, bmp := range c.cache {
		bmp.Dispose()
	}
	c.cache = make(map[string]*walk.Bitmap)
}

// extractAndCache 提取图标并加入缓存（unlock 状态下调用）
func (c *iconBmpCache) extractAndCache(path string) *walk.Bitmap {
	extractor := NewIconExtractor()
	iconImg, err := extractor.GetIconImage(path)
	if err != nil || iconImg == nil {
		return nil
	}
	bmp := imageToBitmap(iconImg)
	if bmp == nil {
		return nil
	}
	c.mu.Lock()
	c.cache[path] = bmp
	c.mu.Unlock()
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
