package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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

	user32Icon                  = syscall.NewLazyDLL("user32.dll")
	procGetIconInfo             = user32Icon.NewProc("GetIconInfo")
	procDestroyIcon             = user32Icon.NewProc("DestroyIcon")
	procLoadImageW              = user32Icon.NewProc("LoadImageW")
	procCreateIconFromResourceEx = user32Icon.NewProc("CreateIconFromResourceEx")
	procLookupIconIdFromDirEx   = user32Icon.NewProc("LookupIconIdFromDirectoryEx")
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
	LR_DEFAULTSIZE  = 0x00000040
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
	return &IconExtractor{}
}

// GetIconImage 获取文件图标图片。
// 采用"逐尺寸双路径交替"策略：按尺寸阶梯（当前档位优先，其余从大到小）逐个尝试，
// 每个尺寸先走 extractIcon（SHGetImageList），失败再走 extractIconEx（LoadImageW）。
// 最大化成功概率，避免直接落到 32x32 被放大导致箭头过大。
func (ie *IconExtractor) GetIconImage(filePath string) (image.Image, error) {
	logger.Debug("GetIconImage: ENTER path=%q iconSizeBase=%d dpi=%d target=%d",
		filePath, desktopIconSizeBase, CurrentDPI(), DesktopIconSize())

	// 尝试原始路径
	if img := ie.extractIconAlternate(filePath); img != nil {
		return img, nil
	}
	// 解析快捷方式目标路径后重试
	actualPath := ie.resolveIconPath(filePath)
	if actualPath != filePath {
		if img := ie.extractIconAlternate(actualPath); img != nil {
			return img, nil
		}
	}

	logger.Warn("GetIconImage: ALL extraction failed for %q, using fallback", filePath)
	img := ie.getFallbackIcon(filePath)
	return img, nil
}

// extractIconAlternate 按尺寸阶梯逐尺寸双路径交替提取图标。
// 每个尺寸先 extractIcon（SHGetImageList），失败再 extractIconEx（LoadImageW），
// 成功且非低质量即返回。
func (ie *IconExtractor) extractIconAlternate(filePath string) image.Image {
	for _, size := range iconSizeLadder() {
		// 路径1：SHGetImageList
		if img := ie.extractIconBySize(filePath, size); img != nil && !ie.isLowQualityIcon(img) {
			logger.Debug("extractIconAlternate: %q size=%d SHGetImageList ok, got=%dx%d",
				filePath, size, img.Bounds().Dx(), img.Bounds().Dy())
			return img
		}
		// 路径2：LoadImageW（只对 .ico 文件有效，无 ExtractIconExW 兜底）
		if img := ie.extractIconExBySize(filePath, size); img != nil && !ie.isLowQualityIcon(img) {
			logger.Debug("extractIconAlternate: %q size=%d LoadImageW ok, got=%dx%d",
				filePath, size, img.Bounds().Dx(), img.Bounds().Dy())
			return img
		}
	}

	// 阶梯全部跑完后，最后用 ExtractIconExW 兜底（对 exe/dll 提取图标）。
	// 注意：必须放在最后，避免 32x32 兜底抢占中间档的 SHGetImageList。
	if img := ie.extractIconExFallback(filePath); img != nil && !ie.isLowQualityIcon(img) {
		logger.Debug("extractIconAlternate: %q ExtractIconExW fallback ok, got=%dx%d",
			filePath, img.Bounds().Dx(), img.Bounds().Dy())
		return img
	}
	return nil
}

// iconSizeLadder 返回图标尺寸阶梯，当前档位目标尺寸放在最前，其余按从大到小排列。
// 大档(64): [64, 128, 256, 48, 32]
// 中档(48): [48, 128, 256, 64, 32]
// 小档(32): [32, 128, 256, 64, 48]
func iconSizeLadder() []int {
	target := DesktopIconSize()
	order := []int{128, 256, 64, 48, 32}
	ladder := make([]int, 0, len(order))
	// 目标尺寸放最前
	ladder = append(ladder, target)
	for _, s := range order {
		if s != target {
			ladder = append(ladder, s)
		}
	}
	return ladder
}

// extractIconBySize 用 SHGetImageList 按指定尺寸提取图标。
// 尺寸无对应 SHIL 档位时跳过（返回 nil）。
func (ie *IconExtractor) extractIconBySize(filePath string, size int) image.Image {
	shil, shilName, ok := shilForSizeExact(size)
	if !ok {
		logger.Debug("extractIconBySize: size=%d has no SHIL slot, skip for %q", size, filePath)
		return nil
	}
	img, err := ie.extractIconFromSHIL(filePath, shil, shilName)
	if err != nil {
		return nil
	}
	return img
}

// extractIconExBySize 用 LoadImageW 按指定尺寸提取图标。
// 对 .lnk/.url 先解析目标路径再用 LoadImageW，避免直接对快捷方式调用
// LoadImageW（非 .ico 必然失败，产生大量无效 WARN 日志）。
// 注意：这里只做 LoadImageW（对 .ico 文件有效），不做 ExtractIconExW 兜底，
// 避免 32x32 兜底在中间档提前返回，抢占后续更大尺寸档位的 SHGetImageList。
func (ie *IconExtractor) extractIconExBySize(filePath string, size int) image.Image {
	// 解析 .lnk/.url 目标路径（LoadImageW 需要真实文件 .ico/.exe 等）
	target := ie.resolveIconPath(filePath)
	if img := ie.loadImageWBySize(target, size); img != nil {
		return img
	}
	// 若解析出目标但与原始不同，再尝试原始路径兜底
	if target != filePath {
		return ie.loadImageWBySize(filePath, size)
	}
	return nil
}

// loadImageWBySize 用 LoadImageW 按指定尺寸从 .ico 文件加载图标。
// 只对 .ico 文件有效，失败返回 nil（不做任何兜底）。
func (ie *IconExtractor) loadImageWBySize(filePath string, size int) image.Image {
	if filePath == "" {
		return nil
	}
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	hIcon, _, _ := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		IMAGE_ICON,
		uintptr(size),
		uintptr(size),
		LR_LOADFROMFILE,
	)
	if hIcon == 0 {
		return nil
	}
	defer procDestroyIcon.Call(hIcon)
	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		return nil
	}
	return img
}

// extractIconExFallback 用 ExtractIconExW 从 exe/dll 提取图标（最后兜底）。
func (ie *IconExtractor) extractIconExFallback(filePath string) image.Image {
	// 解析 .lnk/.url 目标路径
	target := ie.resolveIconPath(filePath)
	if img := ie.extractIconExW(target); img != nil {
		return img
	}
	if target != filePath {
		return ie.extractIconExW(filePath)
	}
	return nil
}

// extractIconExW 用 ExtractIconExW 提取指定文件（exe/dll/ico）的第一个图标。
func (ie *IconExtractor) extractIconExW(filePath string) image.Image {
	if filePath == "" {
		return nil
	}
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)
	var hIcon uintptr
	ret, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&hIcon)),
		0,
		1,
	)
	if ret == 0 || hIcon == 0 {
		return nil
	}
	defer procDestroyIcon.Call(hIcon)
	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		return nil
	}
	return img
}

// shilForSizeExact 精确映射尺寸到 SHIL 档位。
// 返回 (shil, 名称, 是否有对应档位)。
//   - 256/64：JUMBO(256)，绘制时缩放
//   - 48：EXTRALARGE(48)
//   - 32：LARGE(32)
//   - 16 等：无对应 SHIL 档，返回 ok=false
func shilForSizeExact(size int) (int, string, bool) {
	switch size {
	case 256:
		return SHIL_JUMBO, "SHIL_JUMBO", true
	case 64:
		return SHIL_JUMBO, "SHIL_JUMBO", true
	case 48:
		return SHIL_EXTRALARGE, "SHIL_EXTRALARGE", true
	case 32:
		return SHIL_LARGE, "SHIL_LARGE", true
	default:
		return 0, "", false
	}
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
		target := ie.parseLnkTarget(filePath)
		if target != "" {
			logger.Debug("resolveIconPath: lnk %q -> %q", filePath, target)
			return target
		}
		logger.Debug("resolveIconPath: lnk %q -> (no target found)", filePath)
	case ".url":
		iconFile := ie.parseURLIconFile(filePath)
		if iconFile != "" {
			logger.Debug("resolveIconPath: url %q -> %q", filePath, iconFile)
			return iconFile
		}
		logger.Debug("resolveIconPath: url %q -> (no IconFile found)", filePath)
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

// extractIconExtraLarge 使用 SHGetImageList 获取图标，按当前图标档位选择对应的尺寸列表。
//   - 大档(64)：优先 SHIL_JUMBO (256x256)，若 256 提取失败或无数据空壳，
//     回退 SHIL_EXTRALARGE (48x48)，避免落到 SHGetFileInfo 的 32x32 被放大 2 倍（箭头过大）
//   - 中档(48)：SHIL_EXTRALARGE (48x48)
//   - 小档(32)：SHIL_LARGE (32x32)，原生尺寸无需缩放
func (ie *IconExtractor) extractIconExtraLarge(filePath string) (image.Image, error) {
	// 锁定 goroutine 到 OS 线程，确保 COM 在当前线程正确初始化
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ComInitThread()

	// 按当前档位选择 SHIL 图标列表
	shil, shilName, _ := shilForSize(DesktopIconSize())
	logger.Debug("extractIconExtraLarge: target=%d choose %s", DesktopIconSize(), shilName)

	img, err := ie.extractIconFromSHIL(filePath, shil, shilName)

	// 大档回退：JUMBO(256) 提取失败（GetIcon 无效句柄）或提取出的图标是空壳
	// （alpha 全零/可见像素过少）时，降级到 EXTRALARGE(48) 重新提取。
	// 避免直接落到外层 SHGetFileInfo 的 32x32 —— 32 源放大到 64 会放大 2 倍，
	// 导致快捷方式箭头过大、图标发糊。
	if shil == SHIL_JUMBO && (err != nil || ie.isLowQualityIcon(img)) {
		logger.Debug("extractIconExtraLarge: %q JUMBO(256) unavailable (err=%v), fallback to EXTRALARGE(48)", filePath, err)
		fallback, ferr := ie.extractIconFromSHIL(filePath, SHIL_EXTRALARGE, "SHIL_EXTRALARGE")
		if ferr == nil && fallback != nil && !ie.isLowQualityIcon(fallback) {
			return fallback, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return img, nil
}

// extractIconFromSHIL 从指定 SHIL 图标列表提取指定文件的图标。
func (ie *IconExtractor) extractIconFromSHIL(filePath string, shil int, shilName string) (image.Image, error) {
	pathPtr, _ := syscall.UTF16PtrFromString(filePath)

	// 获取文件在系统图标列表中的索引
	var shfi SHFILEINFOW
	ret, _, err1 := procSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&shfi)),
		unsafe.Sizeof(shfi),
		SHGFI_SYSICONINDEX,
	)
	if ret == 0 {
		logger.Debug("extractIconFromSHIL: SHGetFileInfoW(SYSICONINDEX) failed for %q ret=0 err=%v", filePath, err1)
		return nil, os.ErrNotExist
	}
	iconIndex := shfi.IIcon
	logger.Debug("extractIconFromSHIL: path=%q iconIndex=%d shil=%s", filePath, iconIndex, shilName)

	// 获取对应尺寸的图标列表 — 返回 IImageList COM 接口
	var pImageList uintptr
	hr, _, err2 := procSHGetImageList.Call(
		uintptr(shil),
		uintptr(unsafe.Pointer(&IID_IImageList)),
		uintptr(unsafe.Pointer(&pImageList)),
	)
	if hr != 0 || pImageList == 0 {
		logger.Debug("extractIconFromSHIL: SHGetImageList(%s) failed for %q hr=0x%X err=%v", shilName, filePath, hr, err2)
		return nil, os.ErrNotExist
	}

	// 通过 IImageList COM vtable 调用 GetIcon 方法
	// IImageList vtable: QueryInterface(0), AddRef(1), Release(2), Add(3), ReplaceIcon(4),
	// SetOverlayImage(5), Replace(6), AddMasked(7), Draw(8), Remove(9), GetIcon(10)
	vtable := *(*[64]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(pImageList))))
	// SHGetImageList 返回的 IImageList 每次调用都会 AddRef，必须 Release 平衡引用计数，
	// 否则每次刷新提取图标都会累积 COM 引用计数，长时间运行导致 COM 资源耗尽。
	defer syscall.SyscallN(vtable[2], pImageList) // IImageList::Release
	var hIcon uintptr
	hr2, _, err3 := syscall.SyscallN(vtable[10], // IImageList::GetIcon
		pImageList,
		uintptr(iconIndex),
		ILD_TRANSPARENT,
		uintptr(unsafe.Pointer(&hIcon)),
	)
	// 验证 HICON 句柄有效性：
	// 1. hIcon == 0 表示完全无效
	// 2. 在 64 位系统上，有效 HICON 的高 32 位应为 0，若高 32 位非零说明
	//    IImageList::GetIcon 返回了被符号扩展的无效句柄（常见于图标索引对应
	//    的图标加载失败时），此时 GetIconInfo/GetDIBits 可能读到垃圾数据，
	//    导致出现全白/异常图标（如"飞鸽传书"中档显示白色框的问题）
	if hr2 != 0 || hIcon == 0 || (hIcon>>32) != 0 {
		logger.Debug("extractIconFromSHIL: GetIcon failed for %q hr=0x%X idx=%d hIcon=0x%X err=%v",
			filePath, hr2, iconIndex, hIcon, err3)
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(hIcon)
	logger.Debug("extractIconFromSHIL: GetIcon ok for %q hIcon=0x%X", filePath, hIcon)

	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		logger.Debug("extractIconFromSHIL: hIconToImage failed for %q: %v", filePath, err)
		return nil, err
	}
	logger.Debug("extractIconFromSHIL: ok for %q size=%dx%d shil=%s", filePath, img.Bounds().Dx(), img.Bounds().Dy(), shilName)
	return img, nil
}

// shilForSize 根据目标图标尺寸选择对应的 SHGetImageList 列表。
// 返回 (shil 值, 列表名称, 源尺寸)。
//   - 目标 >= 128：JUMBO (256)
//   - 目标 64：JUMBO (256)，绘制时缩放
//   - 目标 48：EXTRALARGE (48)
//   - 目标 32：LARGE (32)
//   - 其他：默认 EXTRALARGE (48)
func shilForSize(target int) (int, string, int) {
	switch {
	case target >= 128:
		return SHIL_JUMBO, "SHIL_JUMBO", 256
	case target == 64:
		return SHIL_JUMBO, "SHIL_JUMBO", 256
	case target == 48:
		return SHIL_EXTRALARGE, "SHIL_EXTRALARGE", 48
	case target == 32:
		return SHIL_LARGE, "SHIL_LARGE", 32
	default:
		return SHIL_EXTRALARGE, "SHIL_EXTRALARGE", 48
	}
}

// ExtractIcoFile 从 .ico 文件中提取指定尺寸的图标（默认 48x48）
// 使用 LoadImageW 直接读取文件，避免 SHGetImageList 取到通用图标
//
// 关键：LoadImageW 加载 .ico 资源时，请求的尺寸必须与 .ico 内置资源尺寸完全一致才会成功，
// 否则会返回 0。Windows .ico 文件通常内置 16/32/48 等标准尺寸，但很少有 28/40 这种非标尺寸。
// 之前用 DesktopIconSize()（中档=40, 小档=28）请求会导致部分 .ico 加载失败，
// 表现为中档/小档时某些 .ico/.lnk 图标变成空白 fallback。
// 修复：始终用 48（最大标准尺寸）请求，缩放由调用方通过 DrawBitmapWithOpacityPixels
// 按目标 rect 自动完成（walk 内部用高质量拉伸），避免反复重新提取。
// ExtractIcoFromMemory 从内存中的 .ico 数据加载图标，无需写入临时文件
func (ie *IconExtractor) ExtractIcoFromMemory(data []byte) image.Image {
	// ICO 格式解码（Go 原生，不依赖 CreateIconFromResourceEx Win32 API）
	// ICO 文件格式：
	//   [6 bytes header] reserved(2) + type(2) + count(2)
	//   [16*count bytes directory entries]
	//   [image data blocks] 每个条目可以是 PNG 或 BMP（BGRA）
	if len(data) < 6 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 {
		return nil
	}
	dirOffset := 6
	if len(data) < dirOffset+count*16 {
		return nil
	}

	// 优先选最大尺寸的条目（256x256 通常用 PNG 存储，解码质量最高）
	type icoEntry struct {
		offset uint32
		size   uint32
		w, h  int
	}
	entries := make([]icoEntry, 0, count)
	for i := 0; i < count; i++ {
		base := dirOffset + i*16
		w := int(data[base])
		h := int(data[base+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size := binary.LittleEndian.Uint32(data[base+8:])
		offset := binary.LittleEndian.Uint32(data[base+12:])
		if int(offset)+int(size) > len(data) {
			continue
		}
		entries = append(entries, icoEntry{offset: offset, size: size, w: w, h: h})
	}
	if len(entries) == 0 {
		return nil
	}

	// 按尺寸从大到小排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].w > entries[j].w
	})

	for _, entry := range entries {
		imgData := data[entry.offset : entry.offset+entry.size]

		// 尝试 PNG 解码（大尺寸图标通常用 PNG 格式）
		if img, err := png.Decode(bytes.NewReader(imgData)); err == nil {
			return img
		}

		// BMP 格式解码（小尺寸图标用 BGRA 位图）
		// BMP 结构：BITMAPINFOHEADER(40 bytes) + BGRA pixels + AND mask
		if len(imgData) < 40 {
			continue
		}
		bmpHeaderSize := int(binary.LittleEndian.Uint32(imgData[0:4]))
		if bmpHeaderSize < 40 || len(imgData) < bmpHeaderSize {
			continue
		}
		bmpW := int(binary.LittleEndian.Uint32(imgData[4:8]))
		bmpH := int(binary.LittleEndian.Uint32(imgData[8:12]))
		bpp := binary.LittleEndian.Uint16(imgData[14:16])
		if bpp != 32 {
			continue
		}
		// BMP 高度 = 实际高度 * 2（包含 AND 掩码），所以实际高度 = bmpH/2
		actualH := bmpH / 2
		if actualH <= 0 {
			continue
		}
		pixelOffset := bmpHeaderSize
		rowSize := bmpW * 4
		if len(imgData) < pixelOffset+rowSize*actualH {
			continue
		}

		img := image.NewRGBA(image.Rect(0, 0, bmpW, actualH))
		for y := 0; y < actualH; y++ {
			// BMP 是 bottom-up 存储
			srcY := actualH - 1 - y
			for x := 0; x < bmpW; x++ {
				px := pixelOffset + (srcY*bmpW+x)*4
				if px+3 >= len(imgData) {
					continue
				}
				// BGRA -> RGBA
				img.SetRGBA(x, y, color.RGBA{
					R: imgData[px+2],
					G: imgData[px+1],
					B: imgData[px+0],
					A: imgData[px+3],
				})
			}
		}
		return img
	}

	logger.Warn("ExtractIcoFromMemory: all entries failed to decode")
	return nil
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
		logger.Debug("hIconToImage: HbmColor==0 (no color bitmap) hIcon=0x%X", hIcon)
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
		logger.Debug("hIconToImage: zero dimensions (w=%d h=%d) hIcon=0x%X", width, height, hIcon)
		return nil, os.ErrNotExist
	}
	logger.Debug("hIconToImage: source HICON hIcon=0x%X size=%dx%d bitsPixel=%d hasMask=%v",
		hIcon, width, height, bmp.BmBitsPixel, iconInfo.HbmMask != 0)

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
		logger.Debug("hIconToImage: alpha channel all-zero, trying mask fallback (hIcon=0x%X size=%dx%d)", hIcon, width, height)
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
		logger.Debug("hIconToImage: mask fallback result usedMask=%v (hIcon=0x%X)", usedMask, hIcon)
		// 如果 mask 也没有提供有效数据，将所有非全黑像素设为不透明
		if !usedMask {
			logger.Debug("hIconToImage: mask did not help, forcing non-black pixels opaque (hIcon=0x%X)", hIcon)
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
		logger.Warn("hIconToImage: icon fully transparent (hIcon=0x%X size=%dx%d hasAlpha=%v) -> returning nil",
			hIcon, width, height, hasAlpha)
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
func (ie *IconExtractor) GetSystemIconImage(shellPath string) (image.Image, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ComInitThread()

	// 根据 shell: 路径选择对应的 FOLDERID
	var folderID [16]byte
	switch {
	case strings.HasPrefix(shellPath, "shell:RecycleBinFolder"):
		// FOLDERID_RecycleBinFolder = {B7534046-3ECB-4C18-BE4E-64CD4CB7D6AC}
		folderID = [...]byte{
			0x46, 0x40, 0x53, 0xB7,
			0xCB, 0x3E,
			0x18, 0x4C,
			0xBE, 0x4E,
			0x64, 0xCD, 0x4C, 0xB7, 0xD6, 0xAC,
		}
	case strings.HasPrefix(shellPath, "shell:NetworkFolder"):
		// FOLDERID_NetworkFolder = {D20BEEC4-5CA8-4905-AE3B-BF251EA09B53}
		folderID = [...]byte{
			0xC4, 0xEE, 0x0B, 0xD2,
			0xA8, 0x5C,
			0x05, 0x49,
			0xAE, 0x3B,
			0xBF, 0x25, 0x1E, 0xA0, 0x9B, 0x53,
		}
	default:
		// FOLDERID_ComputerFolder = {0AC0837C-BBF8-452A-850D-79D08E667CA7}
		folderID = [...]byte{
			0x7C, 0x83, 0xC0, 0x0A,
			0xF8, 0xBB,
			0x2A, 0x45,
			0x85, 0x0D,
			0x79, 0xD0, 0x8E, 0x66, 0x7C, 0xA7,
		}
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

	// 按当前图标档位选择 SHIL 列表（与 extractIconExtraLarge 一致）：
	// 大档(64)用 JUMBO(256)，中档(48)用 EXTRALARGE(48)，小档(32)用 LARGE(32)
	shil, shilName, _ := shilForSize(DesktopIconSize())
	logger.Debug("GetSystemIconImage: target=%d choose %s", DesktopIconSize(), shilName)

	img, err := ie.extractSystemIconFromSHIL(pidl, iconIndex, shil, shilName)

	// 大档回退：JUMBO(256) 提取失败或提取出的系统图标是空壳
	// （alpha 全零/可见像素过少）时，降级到 EXTRALARGE(48) 重新提取。
	if shil == SHIL_JUMBO && (err != nil || ie.isLowQualityIcon(img)) {
		logger.Debug("GetSystemIconImage: shell:%x JUMBO(256) unavailable (err=%v), fallback to EXTRALARGE(48)", iconIndex, err)
		fallback, ferr := ie.extractSystemIconFromSHIL(pidl, iconIndex, SHIL_EXTRALARGE, "SHIL_EXTRALARGE")
		if ferr == nil && fallback != nil && !ie.isLowQualityIcon(fallback) {
			return fallback, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return img, nil
}

// extractSystemIconFromSHIL 从指定 SHIL 图标列表提取系统图标（使用 PIDL + 索引）。
func (ie *IconExtractor) extractSystemIconFromSHIL(pidl uintptr, iconIndex int32, shil int, shilName string) (image.Image, error) {
	var pImageList uintptr
	hr2, _, _ := procSHGetImageList.Call(
		uintptr(shil),
		uintptr(unsafe.Pointer(&IID_IImageList)),
		uintptr(unsafe.Pointer(&pImageList)),
	)
	if hr2 != 0 || pImageList == 0 {
		logger.Warn("extractSystemIconFromSHIL: SHGetImageList(%s) failed hr=0x%X", shilName, hr2)
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
	// SHGetImageList 返回的 IImageList 每次调用都会 AddRef，必须 Release 平衡引用计数，
	// 否则长时间反复刷新（每次刷新都提取系统图标）会累积 COM 引用计数，
	// 最终导致 COM 资源耗尽（GetIcon failed hr=0x8007000E / CreateDIBSection failed）。
	defer syscall.SyscallN(vtable[2], pImageList) // IImageList::Release
	var hIcon uintptr
	hr3, _, _ := syscall.SyscallN(vtable[10], pImageList, uintptr(iconIndex), ILD_TRANSPARENT, uintptr(unsafe.Pointer(&hIcon)))
	if hr3 != 0 || hIcon == 0 {
		logger.Warn("extractSystemIconFromSHIL: GetIcon failed hr=0x%X idx=%d", hr3, iconIndex)
		return nil, os.ErrNotExist
	}
	defer procDestroyIcon.Call(hIcon)

	img, err := ie.hIconToImage(hIcon)
	if err != nil {
		return nil, err
	}
	logger.Debug("extractSystemIconFromSHIL: ok size=%dx%d shil=%s", img.Bounds().Dx(), img.Bounds().Dy(), shilName)
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
