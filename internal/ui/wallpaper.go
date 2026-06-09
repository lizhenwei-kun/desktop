package ui

import (
	"os"
	"syscall"
	"unsafe"
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
func GetCurrentWallpaper() string {
	// 方法1：通过 SystemParametersInfo API
	buf := make([]uint16, 260)
	procSystemParametersInfoW.Call(
		uintptr(SPI_GETDESKWALLPAPER),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&buf[0])),
		0,
	)
	path := syscall.UTF16ToString(buf)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 方法2：通过注册表读取壁纸路径
	path = getWallpaperFromRegistry()
	if path != "" {
		return path
	}

	// 方法3：使用 Windows 缓存的壁纸文件（TranscodedWallpaper）
	path = getTranscodedWallpaper()
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
