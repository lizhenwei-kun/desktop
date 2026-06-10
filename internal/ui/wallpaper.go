package ui

import (
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"desktop_go/internal/logger"
)

var (
	user32Wallpaper           = syscall.NewLazyDLL("user32.dll")
	advapi32                  = syscall.NewLazyDLL("advapi32.dll")
	procSystemParametersInfoW = user32Wallpaper.NewProc("SystemParametersInfoW")
	procRegOpenKeyExW         = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW      = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey           = advapi32.NewProc("RegCloseKey")
)

const (
	SPI_GETDESKWALLPAPER = 0x0073
	HKEY_CURRENT_USER    = 0x80000001
	KEY_QUERY_VALUE      = 0x0001
)

// GetCurrentWallpaper 获取当前桌面壁纸路径
// 优先使用 TranscodedWallpaper（已按屏幕分辨率和填充模式处理的缓存版本）
func GetCurrentWallpaper() string {
	// 方法1：使用 Windows 缓存的壁纸文件（已适配当前分辨率）
	path := getTranscodedWallpaper()
	if path != "" {
		return path
	}

	// 方法2：通过 SystemParametersInfo API（原始图片，可能需要自行缩放）
	buf := make([]uint16, 260)
	procSystemParametersInfoW.Call(
		uintptr(SPI_GETDESKWALLPAPER),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&buf[0])),
		0,
	)
	path = syscall.UTF16ToString(buf)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 方法3：通过注册表读取壁纸路径
	path = getWallpaperFromRegistry()
	if path != "" {
		return path
	}

	return ""
}

// getWallpaperFromRegistry 从注册表读取壁纸路径
func getWallpaperFromRegistry() string {
	subKey, _ := syscall.UTF16PtrFromString(`Control Panel\Desktop`)

	var hKey syscall.Handle
	ret, _, _ := procRegOpenKeyExW.Call(
		uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(subKey)),
		0,
		uintptr(KEY_QUERY_VALUE),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return ""
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	// 读取 Wallpaper 值
	valueName, _ := syscall.UTF16PtrFromString("Wallpaper")
	buf := make([]uint16, 260)
	bufSize := uint32(len(buf) * 2)
	ret, _, _ = procRegQueryValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valueName)),
		0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ret == 0 {
		path := syscall.UTF16ToString(buf)
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	return ""
}

// getTranscodedWallpaper 获取 Windows 缓存的壁纸文件
func getTranscodedWallpaper() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	// Windows 缓存壁纸位置
	path := appData + `\Microsoft\Windows\Themes\TranscodedWallpaper`
	if _, err := os.Stat(path); err == nil {
		return path
	}
	// 旧版 Windows
	path = appData + `\Microsoft\Windows\Themes\CachedFiles\CachedImage_1920_1080_POS4.jpg`
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// WallpaperExists 检查壁纸文件是否存在
func WallpaperExists() bool {
	path := GetCurrentWallpaper()
	return path != ""
}

// LoadWallpaperImage 使用 Go 标准库加载壁纸并按 Fill 模式裁剪到目标尺寸
// Fill 模式：等比缩放使图像完全覆盖目标区域，多余部分居中裁剪
func LoadWallpaperImage(targetW, targetH int) *image.RGBA {
	wallpaperPath := GetCurrentWallpaper()
	if wallpaperPath == "" {
		return nil
	}

	f, err := os.Open(wallpaperPath)
	if err != nil {
		logger.Debug("LoadWallpaperImage: open failed: %v", err)
		return nil
	}
	defer f.Close()

	// 尝试解码（先试 JPEG，再试 PNG，最后用通用 Decode）
	var img image.Image
	img, err = jpeg.Decode(f)
	if err != nil {
		f.Seek(0, 0)
		img, err = png.Decode(f)
		if err != nil {
			f.Seek(0, 0)
			img, _, err = image.Decode(f)
			if err != nil {
				logger.Debug("LoadWallpaperImage: decode failed: %v", err)
				return nil
			}
		}
	}

	srcBounds := img.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	logger.Debug("LoadWallpaperImage: src=%dx%d, target=%dx%d", srcW, srcH, targetW, targetH)

	// Fill 模式：计算缩放比例，取较大的缩放因子，使图像完全覆盖目标区域
	scaleX := float64(targetW) / float64(srcW)
	scaleY := float64(targetH) / float64(srcH)
	scale := scaleX
	if scaleY > scaleX {
		scale = scaleY
	}

	// 缩放后的尺寸
	scaledW := int(float64(srcW) * scale)
	scaledH := int(float64(srcH) * scale)

	// 如果缩放比接近1且只需要裁剪，直接从源图裁剪（避免缩放损失）
	var result *image.RGBA
	if scale > 0.99 && scale < 1.01 {
		// 几乎不需要缩放，直接居中裁剪
		offsetX := (srcW - targetW) / 2
		offsetY := (srcH - targetH) / 2
		if offsetX < 0 {
			offsetX = 0
		}
		if offsetY < 0 {
			offsetY = 0
		}
		cropRect := image.Rect(offsetX, offsetY, offsetX+targetW, offsetY+targetH)
		result = image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		draw.Draw(result, result.Bounds(), img, cropRect.Min, draw.Src)
	} else {
		// 需要缩放：先缩放再裁剪
		// 简单的缩放实现（使用最近邻）
		scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
		for y := 0; y < scaledH; y++ {
			srcY := int(float64(y) / scale)
			if srcY >= srcH {
				srcY = srcH - 1
			}
			for x := 0; x < scaledW; x++ {
				srcX := int(float64(x) / scale)
				if srcX >= srcW {
					srcX = srcW - 1
				}
				scaled.Set(x, y, img.At(srcX+srcBounds.Min.X, srcY+srcBounds.Min.Y))
			}
		}
		// 居中裁剪
		offsetX := (scaledW - targetW) / 2
		offsetY := (scaledH - targetH) / 2
		if offsetX < 0 {
			offsetX = 0
		}
		if offsetY < 0 {
			offsetY = 0
		}
		result = image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		draw.Draw(result, result.Bounds(), scaled, image.Pt(offsetX, offsetY), draw.Src)
	}

	logger.Debug("LoadWallpaperImage: done, result=%dx%d", targetW, targetH)
	return result
}
