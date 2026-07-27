package ui

import (
	"image"
	"image/draw"
	"io/fs"
	"strings"
	"unsafe"

	"github.com/lxn/walk"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
)

// EmbeddedIcoFS 嵌入的 ico 文件系统，由 main.go 在启动时注册
var EmbeddedIcoFS fs.FS

// GlobalIconBmpCache 全局图标 bitmap 缓存
// 所有访问均在 walk 主 UI 线程中，无需加锁
var GlobalIconBmpCache = newIconBmpCache()

// IconBmpCache 图标 bitmap 缓存
type IconBmpCache struct {
	cache     map[string]*walk.Bitmap
	extractor *IconExtractor // 复用图标提取器
}

func newIconBmpCache() *IconBmpCache {
	return &IconBmpCache{
		cache:     make(map[string]*walk.Bitmap),
		extractor: NewIconExtractor(),
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
	logger.Debug("IconBmpCache.GetOrLoad: cache MISS for %q, calling extractAndCache", path)
	return c.extractAndCache(path)
}

// LoadAll 全量刷新图标缓存：只保留 paths 中的条目，其余全部释放
func (c *IconBmpCache) LoadAll(paths []string) {
	oldSize := len(c.cache)

	// 标记需要保留的路径
	keep := make(map[string]bool, len(paths))
	for _, path := range paths {
		keep[path] = true
	}

	// 释放不再需要的缓存
	deleted := 0
	for path, bmp := range c.cache {
		if !keep[path] {
			bmp.Dispose()
			delete(c.cache, path)
			deleted++
		}
	}

	// 加载新路径的图标
	loaded := 0
	for _, path := range paths {
		if _, ok := c.cache[path]; ok {
			continue
		}
		if strings.HasPrefix(path, "shell:") {
			continue
		}
		iconImg, err := c.extractor.GetIconImage(path)
		if err != nil || iconImg == nil {
			continue
		}
		bmp := imageToBitmap(iconImg)
		if bmp == nil {
			continue
		}
		c.cache[path] = bmp
		loaded++
	}

	logger.Info("IconBmpCache.LoadAll: hit=%d new=%d removed=%d total=%d (was %d)",
		len(paths)-loaded, loaded, deleted, len(c.cache), oldSize)
}

// LoadAllFromManager 从 Manager 收集所有项目（分组+未分组）一次性预加载图标缓存
func (c *IconBmpCache) LoadAllFromManager(mgr *group.Manager) {
	all := mgr.GetAllItems()
	paths := make([]string, 0, len(all))
	for _, item := range all {
		paths = append(paths, item.Path)
	}
	c.LoadAll(paths)
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
	var iconImg image.Image
	var err error

	if strings.HasPrefix(path, "shell:") {
		iconImg, err = loadEmbeddedSystemIcon(path, c.extractor)
	} else {
		iconImg, err = c.extractor.GetIconImage(path)
	}

	if err != nil || iconImg == nil {
		logger.Warn("extractAndCache: failed for %q: %v", path, err)
		return nil
	}
	bmp := imageToBitmap(iconImg)
	if bmp == nil {
		logger.Warn("extractAndCache: imageToBitmap failed for %q", path)
		return nil
	}
	c.cache[path] = bmp
	bounds := iconImg.Bounds()
	logger.Debug("extractAndCache: cached %q image=%dx%d", path, bounds.Dx(), bounds.Dy())
	return bmp
}

// loadEmbeddedSystemIcon 加载系统图标：直接走系统提取（SHGetKnownFolderIDList + SHGetImageList），
// 不使用嵌入的 ico 文件。嵌入的 ico 在 BMP 格式解码时存在毛边和颜色问题，
// 系统 API 提取的图标是系统原生渲染，效果最佳。
func loadEmbeddedSystemIcon(shellPath string, extractor *IconExtractor) (image.Image, error) {
	logger.Debug("loadEmbeddedSystemIcon: system extraction for %s", shellPath)
	return extractor.GetSystemIconImage(shellPath)
}

// systemIconCLSID 根据 shell: 路径返回对应的 CLSID 路径（供 program.go 执行用）
func systemIconCLSID(path string) string {
	switch {
	case strings.HasPrefix(path, "shell:MyComputerFolder"):
		return "::{20D04FE0-3AEA-1069-A2D8-08002B30309D}"
	case strings.HasPrefix(path, "shell:NetworkFolder"):
		return "::{F02C1A0D-BE21-4350-A0B7-0B13B35AF3C9}"
	case strings.HasPrefix(path, "shell:RecycleBinFolder"):
		return "::{645FF040-5081-101B-9F08-00AA002F954E}"
	}
	return ""
}

// embeddedIcoPath 根据 shell: 路径返回嵌入的 ico 文件路径
func embeddedIcoPath(shellPath string) string {
	switch {
	case strings.HasPrefix(shellPath, "shell:MyComputerFolder"):
		return "ico/imageres_00105_1.ico"
	case strings.HasPrefix(shellPath, "shell:NetworkFolder"):
		return "ico/imageres_00021_1.ico"
	case strings.HasPrefix(shellPath, "shell:RecycleBinFolder"):
		return recycleBinIcoPath()
	}
	return ""
}

// recycleBinIcoPath 根据回收站状态返回对应的图标文件
func recycleBinIcoPath() string {
	// 用 SHQueryRecycleBinW 检测回收站状态
	var state SHQUERYRBINFO
	state.CbSize = uint32(unsafe.Sizeof(state))
	ProcSHQueryRecycleBinW.Call(0, uintptr(unsafe.Pointer(&state)))
	if state.II64Size > 0 || state.II64NumItems > 0 {
		return "ico/imageres_00050_1.ico" // 有内容
	}
	return "ico/imageres_00051.ico" // 清空
}

// imageToBitmap 将 image.Image 转为 walk.Bitmap
func imageToBitmap(img image.Image) *walk.Bitmap {
	rgbaImg, ok := img.(*image.RGBA)
	if !ok {
		b := img.Bounds()
		rgbaImg = image.NewRGBA(b)
		draw.Draw(rgbaImg, b, img, b.Min, draw.Src)
	}
	bmp, err := walk.NewBitmapFromImage(rgbaImg)
	if err != nil {
		bounds := img.Bounds()
		logger.Warn("imageToBitmap: NewBitmapFromImage failed size=%dx%d err=%v", bounds.Dx(), bounds.Dy(), err)
		return nil
	}
	return bmp
}
