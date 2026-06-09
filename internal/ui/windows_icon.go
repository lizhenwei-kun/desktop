package ui

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	procSHGetFileInfoW = shell32.NewProc("SHGetFileInfoW")

	gdi32          = syscall.NewLazyDLL("gdi32.dll")
	procGetDIBits  = gdi32.NewProc("GetDIBits")
	procGetObject  = gdi32.NewProc("GetObjectW")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC   = gdi32.NewProc("DeleteDC")
	procDeleteObject = gdi32.NewProc("DeleteObject")

	user32Icon         = syscall.NewLazyDLL("user32.dll")
	procGetIconInfo    = user32Icon.NewProc("GetIconInfo")
	procDestroyIcon    = user32Icon.NewProc("DestroyIcon")
)

const (
	SHGFI_ICON      = 0x000000100
	SHGFI_LARGEICON = 0x000000000
	SHGFI_SMALLICON = 0x000000001
)

// SHFILEINFOW SHGetFileInfo 结构体
type SHFILEINFOW struct {
	HIcon         uintptr
	IIcon         int32
	DwAttributes  uint32
	SzDisplayName [260]uint16
	SzTypeName    [80]uint16
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

// IconExtractor 图标提取器
type IconExtractor struct{}

// NewIconExtractor 创建图标提取器
func NewIconExtractor() *IconExtractor {
	return &IconExtractor{}
}

// GetIconImage 获取文件图标图片
func (ie *IconExtractor) GetIconImage(filePath string) (image.Image, error) {
	// 检查缓存
	if cached, ok := iconCache.Load(filePath); ok {
		return cached.(image.Image), nil
	}

	// 解析实际图标路径
	actualPath := ie.resolveIconPath(filePath)

	// 使用 SHGetFileInfo 获取图标
	img, err := ie.extractIcon(actualPath)
	if err != nil {
		// 使用回退图标
		img = ie.getFallbackIcon(filePath)
	}

	// 检查图标质量，必要时使用回退
	if img != nil && ie.isLowQualityIcon(img) {
		img = ie.getFallbackIcon(filePath)
	}

	if img == nil {
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

// extractIcon 使用 SHGetFileInfo 提取图标
func (ie *IconExtractor) extractIcon(filePath string) (image.Image, error) {
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
		return nil, os.ErrNotExist
	}

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
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			if i+3 < len(pixels) {
				img.SetRGBA(x, y, color.RGBA{
					R: pixels[i+2],
					G: pixels[i+1],
					B: pixels[i+0],
					A: pixels[i+3],
				})
			}
		}
	}

	return img, nil
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
