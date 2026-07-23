package ui

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"desktop_go/internal/logger"
)

var (
	shell32                    = syscall.NewLazyDLL("shell32.dll")
	procSHGetFileInfoW         = shell32.NewProc("SHGetFileInfoW")
	procSHGetImageList         = shell32.NewProc("SHGetImageList")
	procExtractIconExW         = shell32.NewProc("ExtractIconExW")
	procSHGetKnownFolderIDList = shell32.NewProc("SHGetKnownFolderIDList")
	ProcSHQueryRecycleBinW     = shell32.NewProc("SHQueryRecycleBinW")

	ole32                  = syscall.NewLazyDLL("ole32.dll")

	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procGetObject          = gdi32.NewProc("GetObjectW")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")

	user32Icon          = syscall.NewLazyDLL("user32.dll")
	procGetIconInfo     = user32Icon.NewProc("GetIconInfo")
	procDestroyIcon     = user32Icon.NewProc("DestroyIcon")
	procLoadImageW      = user32Icon.NewProc("LoadImageW")
)

const (
	SHGFI_ICON         = 0x000000100
	SHGFI_LARGEICON    = 0x000000000
	SHGFI_SMALLICON    = 0x000000001
	SHGFI_SYSICONINDEX = 0x000004000
	SHGFI_PIDL         = 0x000000008

	// SHGetImageList image list types
	SHIL_LARGE      = 0 // 32x32
	SHIL_SMALL      = 1 // 16x16
	SHIL_EXTRALARGE = 2 // 48x48
	SHIL_JUMBO      = 4 // 256x256

	ILD_TRANSPARENT = 0x00000001

	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x00000010
)

// IID_IImageList GUID
var IID_IImageList = syscall.GUID{
	Data1: 0x46EB5926,
	Data2: 0x582E,
	Data3: 0x4017,
	Data4: [8]byte{0x9F, 0xDF, 0xE8, 0x99, 0x8D, 0xAA, 0x09, 0x50},
}

// SHFILEINFOW SHGetFileInfo 结构体
type SHFILEINFOW struct {
	HIcon         uintptr
	IIcon         int32
	DwAttributes  uint32
	SzDisplayName [260]uint16
	SzTypeName    [80]uint16
}

// SHQUERYRBINFO SHQueryRecycleBinW 结构体
type SHQUERYRBINFO struct {
	CbSize     uint32
	II64Size   int64
	II64NumItems int64
}

// ICONINFO GetIconInfo 结构体
type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// BITMAPINFOHEADER DIB 信息头
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// IconCache 图标缓存
var iconCache sync.Map

var iconCacheCleanOnce sync.Once

var (
	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
)

// ComInitThread 在当前线程初始化 COM（每个线程都需要独立初始化）
func ComInitThread() {
	procCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED; 重复调用是安全的
}

// IconExtractor 图标提取器
type IconExtractor struct{}

// NewIconExtractor 创建图标提取器
func NewIconExtractor() *IconExtractor {
	// 首次创建时清理旧的 PNG 缓存，确保使用新的高清图标
	iconCacheCleanOnce.Do(func() {
		home, _ := os.UserHomeDir()
		cacheDir := filepath.Join(home, ".desktop_go", "icon_cache")
		os.RemoveAll(cacheDir)
	})
	return &IconExtractor{}
}

// GetIconImage 获取文件图标图片
func (ie *IconExtractor) GetIconImage(filePath string) (image.Image, error) {
	// 检查缓存
	if cached, ok := iconCache.Load(filePath); ok {
		return cached.(image.Image), nil
	}

	// 直接用原始路径获取图标（SHGetFileInfo/SHGetImageList 能正确处理 .lnk/.url 等）
	img, err := ie.extractIcon(filePath)
	if img == nil {
		logger.Debug("extractIcon failed for %q: %v", filePath, err)
		// 尝试解析快捷方式目标路径后重试
		actualPath := ie.resolveIconPath(filePath)
		if actualPath != filePath {
			img, err = ie.extractIcon(actualPath)
			if img == nil {
				logger.Debug("extractIcon(resolved=%q) also failed: %v", actualPath, err)
			}
		}
	}
	if img == nil {
		// 尝试 ExtractIconExW（对 exe/dll 有效）
		img = ie.extractIconEx(filePath)
	}
	if img == nil {
		// 最后尝试解析 lnk 目标后用 ExtractIconExW
		actualPath := ie.resolveIconPath(filePath)
		if actualPath != filePath {
			img = ie.extractIconEx(actualPath)
		}
	}

	if img == nil {
		logger.Debug("all icon extraction failed for %q, using fallback", filePath)
		img = ie.getFallbackIcon(filePath)
	}

	// 缓存结果
	iconCache.Store(filePath, img)
	return img, nil
}

// GetIconPNGPath 获取图标的 PNG 缓存路径
func (ie *IconExtractor) GetIconPNGPath(filePath string) (string, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".desktop_go", "icon_cache")
	os.MkdirAll(cacheDir, 0755)

	// 使用文件路径的哈希作为缓存文件名
	safeName := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", " ", "_",
	).Replace(filePath)
	if len(safeName) > 100 {
		safeName = safeName[len(safeName)-100:]
	}
	pngPath := filepath.Join(cacheDir, safeName+".png")

	// 如果缓存存在，直接返回
	if _, err := os.Stat(pngPath); err == nil {
		return pngPath, nil
	}

	// 提取图标并保存
	img, err := ie.GetIconImage(filePath)
	if err != nil {
		return "", err
	}

	f, err := os.Create(pngPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		return "", err
	}

	return pngPath, nil
}

// resolveIconPath 解析图标路径（处理 .lnk 和 .url）
func (ie *IconExtractor) resolveIconPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".lnk":
		if target := ie.parseLnkTarget(filePath); target != "" {
			return target
		}
	case ".url":
		if iconFile := ie.parseURLIconFile(filePath); iconFile != "" {
			return iconFile
		}
	}

	return filePath
}

// parseLnkTarget 解析 LNK 快捷方式获取目标路径
func (ie *IconExtractor) parseLnkTarget(lnkPath string) string {
	data, err := os.ReadFile(lnkPath)
	if err != nil || len(data) < 76 {
		return ""
	}

	// 验证 LNK 魔数
	if data[0] != 0x4C || data[1] != 0x00 || data[2] != 0x00 || data[3] != 0x00 {
		return ""
	}

	// Flags 在偏移 0x14
	flags := binary.LittleEndian.Uint32(data[0x14:0x18])
	hasTargetIDList := (flags & 0x01) != 0
	hasLinkInfo := (flags & 0x02) != 0

	offset := 0x4C // header 大小

	// 跳过 TargetIDList
	if hasTargetIDList {
		if offset+2 > len(data) {
			return ""
		}
		idListSize := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2 + idListSize
	}

	// 解析 LinkInfo
	if hasLinkInfo && offset+4 <= len(data) {
		linkInfoSize := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		if linkInfoSize > 0 && offset+linkInfoSize <= len(data) {
			linkInfo := data[offset : offset+linkInfoSize]
			if len(linkInfo) >= 28 {
				localBasePathOffset := int(binary.LittleEndian.Uint32(linkInfo[16:20]))
				if localBasePathOffset > 0 && localBasePathOffset < len(linkInfo) {
					// 读取以 null 结尾的字符串
					end := localBasePathOffset
					for end < len(linkInfo) && linkInfo[end] != 0 {
						end++
					}
					target := string(linkInfo[localBasePathOffset:end])
					if target != "" {
						return target
					}
				}
			}
		}
	}

	return ""
}

// parseURLIconFile 解析 .url 文件的 IconFile 字段
func (ie *IconExtractor) parseURLIconFile(urlPath string) string {
	data, err := os.ReadFile(urlPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "iconfile=") {
			return strings.TrimPrefix(line, line[:9])
		}
	}
	return ""
}

// extractIcon 使用 SHGetImageList 提取 48x48 高清图标
func (ie *IconExtractor) extractIcon(filePath string) (image.Image, error) {
	// 先尝试获取 48x48 extra large 图标
	img, err := ie.extractIconExtraLarge(filePath)
	if err == nil && img != nil {
		return img, nil
	}

	// 回退到 SHGetFileInfo 获取 32x32 大图标
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)

	var shfi SHFILEINFOW
	ret, _, _ := procSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&shfi)),
		unsafe.Sizeof(shfi),
		SHGFI_ICON|SHGFI_LARGEICON,
	)

	if ret == 0 || shfi.HIcon == 0 {
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(shfi.HIcon)

	return ie.hIconToImage(shfi.HIcon)
}

// extractIconExtraLarge 使用 SHGetImageList 获取 48x48 图标
func (ie *IconExtractor) extractIconExtraLarge(filePath string) (image.Image, error) {
	// 锁定 goroutine 到 OS 线程，确保 COM 在当前线程正确初始化
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ComInitThread()

	pathPtr, _ := syscall.UTF16PtrFromString(filePath)

	// 获取文件在系统图标列表中的索引
	var shfi SHFILEINFOW
	ret, _, _ := procSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&shfi)),
		unsafe.Sizeof(shfi),
		SHGFI_SYSICONINDEX,
	)
	if ret == 0 {
		logger.Debug("extractIconExtraLarge: SHGetFileInfoW SYSICONINDEX failed for %q", filePath)
		return nil, os.ErrNotExist
	}
	iconIndex := shfi.IIcon

	// 获取 Extra Large (48x48) 图标列表 — 返回 IImageList COM 接口
	var pImageList uintptr
	hr, _, _ := procSHGetImageList.Call(
		SHIL_EXTRALARGE,
		uintptr(unsafe.Pointer(&IID_IImageList)),
		uintptr(unsafe.Pointer(&pImageList)),
	)
	if hr != 0 || pImageList == 0 {
		logger.Debug("extractIconExtraLarge: SHGetImageList failed hr=0x%X for %q", hr, filePath)
		return nil, os.ErrNotExist
	}

	// 通过 IImageList COM vtable 调用 GetIcon 方法
	// IImageList vtable: QueryInterface(0), AddRef(1), Release(2), Add(3), ReplaceIcon(4),
	// SetOverlayImage(5), Replace(6), AddMasked(7), Draw(8), Remove(9), GetIcon(10)
	vtable := *(*[64]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(pImageList))))
	var hIcon uintptr
	hr2, _, _ := syscall.SyscallN(vtable[10], // IImageList::GetIcon
		pImageList,
		uintptr(iconIndex),
		ILD_TRANSPARENT,
		uintptr(unsafe.Pointer(&hIcon)),
	)
	if hr2 != 0 || hIcon == 0 {
		logger.Debug("extractIconExtraLarge: GetIcon failed hr=0x%X idx=%d for %q", hr2, iconIndex, filePath)
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(hIcon)

	return ie.hIconToImage(hIcon)
}

// ExtractIcoFile 从 .ico 文件中提取指定尺寸的图标（默认 48x48）
// 使用 LoadImageW 直接读取文件，避免 SHGetImageList 取到通用图标
func (ie *IconExtractor) ExtractIcoFile(filePath string) image.Image {
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	hIcon, _, _ := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		IMAGE_ICON,
		uintptr(DesktopIconSize()), // 目标宽度（随 DPI + 档位）
		uintptr(DesktopIconSize()), // 目标高度（随 DPI + 档位）
		LR_LOADFROMFILE,
	)
	if hIcon == 0 {
		logger.Warn("ExtractIcoFile: LoadImageW failed for %q, fallback to ExtractIconExW", filePath)
		// 回退到 ExtractIconExW
		var hIconLarge uintptr
		ret, _, _ := procExtractIconExW.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			0,
			uintptr(unsafe.Pointer(&hIconLarge)),
			0,
			1,
		)
		if ret == 0 || hIconLarge == 0 {
			logger.Warn("ExtractIcoFile: ExtractIconExW also failed for %q", filePath)
			return nil
		}
		defer procDestroyIcon.Call(hIconLarge)
		img, err := ie.hIconToImage(hIconLarge)
		if err != nil {
			return nil
		}
		logger.Debug("ExtractIcoFile: ExtractIconExW fallback ok, size=%dx%d", img.Bounds().Dx(), img.Bounds().Dy())
		return img
	}
	defer procDestroyIcon.Call(hIcon)

	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		logger.Warn("ExtractIcoFile: hIconToImage failed for %q: %v", filePath, err)
		return nil
	}
	logger.Debug("ExtractIcoFile: LoadImageW ok, file=%q size=%dx%d", filePath, img.Bounds().Dx(), img.Bounds().Dy())
	return img
}

// extractIconEx 使用 ExtractIconExW 提取图标（对 exe/dll/ico 文件有效）
func (ie *IconExtractor) extractIconEx(filePath string) image.Image {
	return ie.ExtractIcoFile(filePath)
}

// hIconToImage 将 HICON 转换为 image.Image
func (ie *IconExtractor) hIconToImage(hIcon uintptr) (image.Image, error) {
	var iconInfo ICONINFO
	ret, _, _ := procGetIconInfo.Call(hIcon, uintptr(unsafe.Pointer(&iconInfo)))
	if ret == 0 {
		return nil, os.ErrNotExist
	}
	defer func() {
		if iconInfo.HbmColor != 0 {
			procDeleteObject.Call(iconInfo.HbmColor)
		}
		if iconInfo.HbmMask != 0 {
			procDeleteObject.Call(iconInfo.HbmMask)
		}
	}()

	if iconInfo.HbmColor == 0 {
		return nil, os.ErrNotExist
	}

	// 获取位图信息
	type BITMAP struct {
		BmType       int32
		BmWidth      int32
		BmHeight     int32
		BmWidthBytes int32
		BmPlanes     uint16
		BmBitsPixel  uint16
		BmBits       uintptr
	}
	var bmp BITMAP
	procGetObject.Call(iconInfo.HbmColor, unsafe.Sizeof(bmp), uintptr(unsafe.Pointer(&bmp)))

	width := int(bmp.BmWidth)
	height := int(bmp.BmHeight)
	if width == 0 || height == 0 {
		logger.Debug("hIconToImage: zero dimensions (w=%d h=%d)", width, height)
		return nil, os.ErrNotExist
	}
	logger.Debug("hIconToImage: source HICON size=%dx%d", width, height)

	// 准备 DIB
	bmi := BITMAPINFOHEADER{
		BiSize:      uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:     int32(width),
		BiHeight:    -int32(height), // 顶部向下
		BiPlanes:    1,
		BiBitCount:  32,
		BiCompression: 0, // BI_RGB
	}

	pixels := make([]byte, width*height*4)

	hdc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(hdc)

	procGetDIBits.Call(
		hdc,
		iconInfo.HbmColor,
		0, uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
	)

	// 转换为 image.RGBA (BGRA -> RGBA)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	hasAlpha := false
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			if i+3 < len(pixels) {
				a := pixels[i+3]
				if a != 0 {
					hasAlpha = true
				}
				img.SetRGBA(x, y, color.RGBA{
					R: pixels[i+2],
					G: pixels[i+1],
					B: pixels[i+0],
					A: a,
				})
			}
		}
	}

	// 如果 alpha 通道全为 0，使用 mask 来确定透明度
	if !hasAlpha {
		usedMask := false
		if iconInfo.HbmMask != 0 {
			maskBmi := BITMAPINFOHEADER{
				BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
				BiWidth:       int32(width),
				BiHeight:      -int32(height),
				BiPlanes:      1,
				BiBitCount:    32,
				BiCompression: 0,
			}
			maskPixels := make([]byte, width*height*4)
			procGetDIBits.Call(
				hdc,
				iconInfo.HbmMask,
				0, uintptr(height),
				uintptr(unsafe.Pointer(&maskPixels[0])),
				uintptr(unsafe.Pointer(&maskBmi)),
				0,
			)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					i := (y*width + x) * 4
					if i+3 < len(maskPixels) {
						// mask=0 表示不透明, mask=白色表示透明
						if maskPixels[i] == 0 && maskPixels[i+1] == 0 && maskPixels[i+2] == 0 {
							c := img.RGBAAt(x, y)
							c.A = 255
							img.SetRGBA(x, y, c)
							usedMask = true
						}
					}
				}
			}
		}
		// 如果 mask 也没有提供有效数据，将所有非全黑像素设为不透明
		if !usedMask {
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					c := img.RGBAAt(x, y)
					if c.R != 0 || c.G != 0 || c.B != 0 {
						c.A = 255
						img.SetRGBA(x, y, c)
					}
				}
			}
		}
	}

	// 检查图标是否完全空白（全透明），如果是则返回错误让上层尝试其他方法
	allTransparent := true
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			allTransparent = false
			break
		}
	}
	if allTransparent {
		return nil, os.ErrNotExist
	}

	// 预乘 alpha（AlphaBlend API 要求 premultiplied alpha 格式）
	premultiplyAlpha(img)

	return img, nil
}

// premultiplyAlpha 将 straight alpha 图像转换为 premultiplied alpha
// Windows AlphaBlend API 要求源位图使用预乘 alpha 格式
func premultiplyAlpha(img *image.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		a := uint32(img.Pix[i+3])
		if a == 0 {
			img.Pix[i+0] = 0
			img.Pix[i+1] = 0
			img.Pix[i+2] = 0
		} else if a < 255 {
			img.Pix[i+0] = byte(uint32(img.Pix[i+0]) * a / 255)
			img.Pix[i+1] = byte(uint32(img.Pix[i+1]) * a / 255)
			img.Pix[i+2] = byte(uint32(img.Pix[i+2]) * a / 255)
		}
	}
}

// isLowQualityIcon 判断图标是否质量过低
func (ie *IconExtractor) isLowQualityIcon(img image.Image) bool {
	bounds := img.Bounds()
	totalPixels := 0
	brightPixels := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a > 0 {
				totalPixels++
				brightness := (r + g + b) / 3
				if brightness > 0x8000 {
					brightPixels++
				}
			}
		}
	}

	// 如果可见像素太少，认为质量低
	area := (bounds.Max.X - bounds.Min.X) * (bounds.Max.Y - bounds.Min.Y)
	if totalPixels < area/10 {
		return true
	}
	return false
}

// getFallbackIcon 获取回退图标
func (ie *IconExtractor) getFallbackIcon(filePath string) image.Image {
	info, err := os.Stat(filePath)
	if err == nil && info.IsDir() {
		return ie.createFolderIcon()
	}
	return ie.createFileIcon(filepath.Ext(filePath))
}

// GetSystemIconImage 获取系统图标（如"此电脑"），使用 SHGetKnownFolderIDList + SHGetImageList
// 通过 KNOWNFOLDERID 获取系统文件夹 PIDL，从系统图标列表提取 48x48 高清图标
func (ie *IconExtractor) GetSystemIconImage() (image.Image, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ComInitThread()

	// FOLDERID_ComputerFolder = {0AC0837C-BBF8-452A-850D-79D08E667CA7}
	folderID := [...]byte{
		0x7C, 0x83, 0xC0, 0x0A,
		0xF8, 0xBB,
		0x2A, 0x45,
		0x85, 0x0D,
		0x79, 0xD0, 0x8E, 0x66, 0x7C, 0xA7,
	}
	var pidl uintptr
	hr, _, _ := procSHGetKnownFolderIDList.Call(
		uintptr(unsafe.Pointer(&folderID[0])),
		0, // dwFlags
		0, // hToken (NULL)
		uintptr(unsafe.Pointer(&pidl)),
	)
	if hr != 0 || pidl == 0 {
		logger.Warn("GetSystemIconImage: SHGetKnownFolderIDList failed hr=0x%X", hr)
		return nil, os.ErrNotExist
	}
	defer procCoTaskMemFree.Call(pidl)

	// 通过 PIDL 获取系统图标列表索引
	var shfi SHFILEINFOW
	ret, _, _ := procSHGetFileInfoW.Call(
		pidl,
		0,
		uintptr(unsafe.Pointer(&shfi)),
		unsafe.Sizeof(shfi),
		SHGFI_SYSICONINDEX|SHGFI_PIDL,
	)
	if ret == 0 {
		logger.Warn("GetSystemIconImage: SHGetFileInfoW SYSICONINDEX failed")
		return nil, os.ErrNotExist
	}
	iconIndex := shfi.IIcon
	logger.Debug("GetSystemIconImage: iconIndex=%d", iconIndex)

	// 从 SHIL_EXTRALARGE (48x48) 图标列表提取
	var pImageList uintptr
	hr2, _, _ := procSHGetImageList.Call(
		SHIL_EXTRALARGE,
		uintptr(unsafe.Pointer(&IID_IImageList)),
		uintptr(unsafe.Pointer(&pImageList)),
	)
	if hr2 != 0 || pImageList == 0 {
		logger.Warn("GetSystemIconImage: SHGetImageList failed hr=0x%X", hr2)
		// 回退：SHGetFileInfoW 直接获取 32x32 图标
		var shfi2 SHFILEINFOW
		ret2, _, _ := procSHGetFileInfoW.Call(
			pidl,
			0,
			uintptr(unsafe.Pointer(&shfi2)),
			unsafe.Sizeof(shfi2),
			SHGFI_ICON|SHGFI_LARGEICON|SHGFI_PIDL,
		)
		if ret2 == 0 || shfi2.HIcon == 0 {
			return nil, os.ErrNotExist
		}
		defer procDestroyIcon.Call(shfi2.HIcon)
		return ie.hIconToImage(shfi2.HIcon)
	}

	vtable := *(*[64]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(pImageList))))
	var hIcon uintptr
	hr3, _, _ := syscall.SyscallN(vtable[10], pImageList, uintptr(iconIndex), ILD_TRANSPARENT, uintptr(unsafe.Pointer(&hIcon)))
	if hr3 != 0 || hIcon == 0 {
		logger.Warn("GetSystemIconImage: GetIcon failed hr=0x%X idx=%d", hr3, iconIndex)
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(hIcon)

	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		return nil, err
	}
	logger.Debug("GetSystemIconImage: ok size=%dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	return img, nil
}

// extractIconExtraLargeFS 使用路径字符串方式提取系统图标（回退方案）
func (ie *IconExtractor) extractIconExtraLargeFS(filePath string) (image.Image, error) {
	img, err := ie.extractIconExtraLarge(filePath)
	if err == nil && img != nil {
		return img, nil
	}
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	var shfi SHFILEINFOW
	ret, _, _ := procSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&shfi)),
		unsafe.Sizeof(shfi),
		SHGFI_ICON|SHGFI_LARGEICON,
	)
	if ret == 0 || shfi.HIcon == 0 {
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(shfi.HIcon)
	return ie.hIconToImage(shfi.HIcon)
}

// createFolderIcon 创建文件夹回退图标（黄色文件夹）
func (ie *IconExtractor) createFolderIcon() image.Image {
	const size = 48
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 绘制黄色文件夹形状
	folderColor := color.RGBA{R: 0xFC, G: 0xC6, B: 0x2D, A: 0xFF}
	tabColor := color.RGBA{R: 0xE6, G: 0xB0, B: 0x20, A: 0xFF}

	// 文件夹标签页（上方小凸起）
	for y := 10; y < 16; y++ {
		for x := 6; x < 22; x++ {
			img.SetRGBA(x, y, tabColor)
		}
	}
	// 文件夹主体
	for y := 16; y < 40; y++ {
		for x := 4; x < 44; x++ {
			img.SetRGBA(x, y, folderColor)
		}
	}

	return img
}

// createFileIcon 创建文件回退图标（白色文档 + 折角）
func (ie *IconExtractor) createFileIcon(ext string) image.Image {
	const size = 48
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 白色文档
	docColor := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	foldColor := color.RGBA{R: 0xCC, G: 0xCC, B: 0xCC, A: 0xFF}

	// 文档主体
	for y := 4; y < 44; y++ {
		for x := 10; x < 38; x++ {
			img.SetRGBA(x, y, docColor)
		}
	}
	// 折角
	for i := 0; i < 8; i++ {
		for j := 0; j <= i; j++ {
			img.SetRGBA(38-8+j, 4+i, foldColor)
		}
	}
	// 边框
	borderColor := color.RGBA{R: 0x99, G: 0x99, B: 0x99, A: 0xFF}
	for y := 4; y < 44; y++ {
		img.SetRGBA(10, y, borderColor)
		img.SetRGBA(37, y, borderColor)
	}
	for x := 10; x < 38; x++ {
		img.SetRGBA(x, 4, borderColor)
		img.SetRGBA(x, 43, borderColor)
	}

	return img
}

// SaveIconToFile 保存图标到文件
func SaveIconToFile(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// GetCachedIconPath 获取缓存的图标路径，如果不存在则提取并缓存
func GetCachedIconPath(filePath string) string {
	extractor := NewIconExtractor()
	pngPath, err := extractor.GetIconPNGPath(filePath)
	if err != nil {
		return ""
	}
	return pngPath
}

// CreateTrayIcon 创建系统托盘图标（蓝色圆形背景 + 白色网格图标 16x16）
func CreateTrayIcon() *image.RGBA {
	const size = 16
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	center := float64(size) / 2
	radius := float64(size)/2 - 0.5
	bgColor := color.RGBA{R: 0x27, G: 0x6B, B: 0xA6, A: 0xFF}

	// 绘制圆形背景
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			if dx*dx+dy*dy <= radius*radius {
				img.SetRGBA(x, y, bgColor)
			}
		}
	}

	// 绘制白色网格图标（代表桌面分组）
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	// 左上格子
	for y := 4; y < 7; y++ {
		for x := 4; x < 7; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 右上格子
	for y := 4; y < 7; y++ {
		for x := 9; x < 12; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 左下格子
	for y := 9; y < 12; y++ {
		for x := 4; x < 7; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 右下格子
	for y := 9; y < 12; y++ {
		for x := 9; x < 12; x++ {
			img.SetRGBA(x, y, white)
		}
	}

	return img
}

// CreateAppIconImage 创建应用程序图标（蓝色圆形背景 + 白色网格 32x32）
func CreateAppIconImage() *image.RGBA {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	center := float64(size) / 2
	radius := float64(size)/2 - 0.5
	bgColor := color.RGBA{R: 0x27, G: 0x6B, B: 0xA6, A: 0xFF}

	// 绘制圆形背景
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			if dx*dx+dy*dy <= radius*radius {
				img.SetRGBA(x, y, bgColor)
			}
		}
	}

	// 绘制白色网格图标（代表桌面分组）
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	// 左上格子
	for y := 8; y < 14; y++ {
		for x := 8; x < 14; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 右上格子
	for y := 8; y < 14; y++ {
		for x := 18; x < 24; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 左下格子
	for y := 18; y < 24; y++ {
		for x := 8; x < 14; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	// 右下格子
	for y := 18; y < 24; y++ {
		for x := 18; x < 24; x++ {
			img.SetRGBA(x, y, white)
		}
	}

	return img
}

// SaveImageAsICO 将 RGBA 图像保存为 .ico 文件
func SaveImageAsICO(img *image.RGBA, path string) error {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// ICO 文件格式：Header + DirEntry + BMP数据
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// 准备像素数据（ICO 中 BMP 是自底向上的 BGRA）
	pixelData := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := img.RGBAAt(x, y)
			// 自底向上
			destY := height - 1 - y
			idx := (destY*width + x) * 4
			pixelData[idx+0] = c.B
			pixelData[idx+1] = c.G
			pixelData[idx+2] = c.R
			pixelData[idx+3] = c.A
		}
	}

	// AND mask（全零 = 全部显示，由 alpha 通道控制）
	andMaskRowSize := ((width + 31) / 32) * 4
	andMask := make([]byte, andMaskRowSize*height)

	// BMP 信息头
	bmpHeaderSize := 40
	imageSize := len(pixelData) + len(andMask)

	// ICO Header (6 bytes)
	binary.Write(f, binary.LittleEndian, uint16(0))     // reserved
	binary.Write(f, binary.LittleEndian, uint16(1))     // type: ICO
	binary.Write(f, binary.LittleEndian, uint16(1))     // count: 1 image

	// ICO Directory Entry (16 bytes)
	w := byte(width)
	h := byte(height)
	if width >= 256 {
		w = 0
	}
	if height >= 256 {
		h = 0
	}
	f.Write([]byte{w})                                          // width
	f.Write([]byte{h})                                          // height
	f.Write([]byte{0})                                          // color palette
	f.Write([]byte{0})                                          // reserved
	binary.Write(f, binary.LittleEndian, uint16(1))             // color planes
	binary.Write(f, binary.LittleEndian, uint16(32))            // bits per pixel
	binary.Write(f, binary.LittleEndian, uint32(bmpHeaderSize+imageSize)) // data size
	binary.Write(f, binary.LittleEndian, uint32(22))            // data offset (6+16)

	// BITMAPINFOHEADER (40 bytes)
	binary.Write(f, binary.LittleEndian, uint32(bmpHeaderSize)) // header size
	binary.Write(f, binary.LittleEndian, int32(width))          // width
	binary.Write(f, binary.LittleEndian, int32(height*2))       // height (includes AND mask)
	binary.Write(f, binary.LittleEndian, uint16(1))             // planes
	binary.Write(f, binary.LittleEndian, uint16(32))            // bit count
	binary.Write(f, binary.LittleEndian, uint32(0))             // compression
	binary.Write(f, binary.LittleEndian, uint32(imageSize))     // image size
	binary.Write(f, binary.LittleEndian, int32(0))              // x ppm
	binary.Write(f, binary.LittleEndian, int32(0))              // y ppm
	binary.Write(f, binary.LittleEndian, uint32(0))             // colors used
	binary.Write(f, binary.LittleEndian, uint32(0))             // colors important

	// pixel data + AND mask
	f.Write(pixelData)
	f.Write(andMask)

	return nil
}

// SaveTrayIconToFile 保存托盘图标为 .ico 文件
func SaveTrayIconToFile() string {
	home, _ := os.UserHomeDir()
	iconDir := filepath.Join(home, ".desktop_go")
	os.MkdirAll(iconDir, 0755)
	iconPath := filepath.Join(iconDir, "tray_icon.ico")

	// 检查是否存在且有效（非空文件）
	if info, err := os.Stat(iconPath); err == nil && info.Size() > 0 {
		return iconPath
	}

	img := CreateTrayIcon()
	if err := SaveImageAsICO(img, iconPath); err != nil {
		return ""
	}
	return iconPath
}

// SaveAppIconToFile 保存应用程序图标为 .ico 文件
func SaveAppIconToFile() string {
	home, _ := os.UserHomeDir()
	iconDir := filepath.Join(home, ".desktop_go")
	os.MkdirAll(iconDir, 0755)
	iconPath := filepath.Join(iconDir, "app_icon.ico")

	// 检查是否存在且有效（非空文件）
	if info, err := os.Stat(iconPath); err == nil && info.Size() > 0 {
		return iconPath
	}

	img := CreateAppIconImage()
	if err := SaveImageAsICO(img, iconPath); err != nil {
		return ""
	}
	return iconPath
}
