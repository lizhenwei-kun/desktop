package ui

import (
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lxn/walk"

	"desktop_go/internal/logger"
)

// EmbeddedIcoFS 嵌入的 ico 文件系统，由 main.go 在启动时注册
var EmbeddedIcoFS fs.FS

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

var systemIcoTempFiles = make(map[string]string) // shell:ID -> temp file path

// extractAndCache 提取图标并加入缓存
func (c *IconBmpCache) extractAndCache(path string) *walk.Bitmap {
	extractor := NewIconExtractor()

	var iconImg image.Image
	var err error

	if strings.HasPrefix(path, "shell:") {
		iconImg, err = loadEmbeddedSystemIcon(path, extractor)
	} else {
		iconImg, err = extractor.GetIconImage(path)
	}

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

// loadEmbeddedSystemIcon 从嵌入的 ico 文件加载系统图标
func loadEmbeddedSystemIcon(shellPath string, extractor *IconExtractor) (image.Image, error) {
	embeddedFile := embeddedIcoPath(shellPath)
	if embeddedFile == "" || EmbeddedIcoFS == nil {
		return nil, os.ErrNotExist
	}

	// 从嵌入 FS 读取 ico 文件
	data, err := fs.ReadFile(EmbeddedIcoFS, embeddedFile)
	if err != nil {
		return nil, err
	}

	// 写入临时文件
	tempDir := filepath.Join(os.TempDir(), "desktop_go_icons")
	os.MkdirAll(tempDir, 0755)
	tempPath := filepath.Join(tempDir, filepath.Base(embeddedFile))

	// 缓存临时文件路径（用于避免重复写入）
	if cached, ok := systemIcoTempFiles[shellPath]; ok {
		tempPath = cached
	}

	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return nil, err
		}
		systemIcoTempFiles[shellPath] = tempPath
	}

	logger.Debug("loadEmbeddedSystemIcon: %s -> %s", shellPath, tempPath)
	return extractor.GetIconImage(tempPath)
}

// systemIconCLSID 根据 shell: 路径返回对应的 CLSID 路径（供 program.go 执行用）
func systemIconCLSID(path string) string {
	switch {
	case strings.HasPrefix(path, "shell:MyComputerFolder"):
		return "::{20D04FE0-3AEA-1069-A2D8-08002B30309D}"
	}
	return ""
}

// embeddedIcoPath 根据 shell: 路径返回嵌入的 ico 文件路径
func embeddedIcoPath(shellPath string) string {
	switch {
	case strings.HasPrefix(shellPath, "shell:MyComputerFolder"):
		return "ico/imageres_00105_1.ico"
	}
	return ""
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
