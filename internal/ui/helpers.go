package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/logger"
)

// 图标磁贴规格 —— 基准常量（96 DPI 下的像素值）
const (
	baseDesktopIconSize    = 48 // 图标位图尺寸（大档基准）
	baseDesktopIconTop     = 2
	baseDesktopIconLineH   = 24 // 文字行高
	baseFreeGridLeft       = 20 // 未分组网格左边距
	baseFreeGridTop        = 60 // 未分组网格上边距

	LongPressDragDelay = 1 * time.Second         // 卡片拖拽延迟（标题栏长按）
	IconDragDelay      = 300 * time.Millisecond  // 图标拖拽延迟（卡片内/未分组图标）
)

// ============================================================
// DPI 缩放基础设施
// ============================================================

// currentDPI 当前窗口 DPI（默认 96）。启动时由 SetCurrentDPI 设置，
// DPI 变化时（WM_DPICHANGED）更新。所有像素常量通过 DpiPx 缩放。
var (
	currentDPI   int = 96
	currentDPIMu sync.RWMutex
)

// SetCurrentDPI 更新当前 DPI。DPI 变化会触发磁贴尺寸重新测量。
func SetCurrentDPI(dpi int) {
	if dpi <= 0 {
		dpi = 96
	}
	currentDPIMu.Lock()
	currentDPI = dpi
	currentDPIMu.Unlock()
	ForceTileRemeasure()
}

// CurrentDPI 返回当前 DPI
func CurrentDPI() int {
	currentDPIMu.RLock()
	defer currentDPIMu.RUnlock()
	return currentDPI
}

// DpiScale 返回相对 96 DPI 的缩放因子（如 150% DPI 返回 1.5）
func DpiScale() float64 {
	return float64(CurrentDPI()) / 96.0
}

// DpiPx 将 96-DPI 基准像素值按当前 DPI 缩放
func DpiPx(base96 int) int {
	return int(float64(base96) * DpiScale())
}

// ============================================================
// 图标位图尺寸（随档位 + DPI 变化）
// ============================================================

// desktopIconSizeBase 当前档位下的图标位图基准尺寸（96 DPI）。
// 大档 48，中档 48（与大档同尺寸，仅字号/间距不同），小档 32。实际像素 = DpiPx(desktopIconSizeBase)。
var desktopIconSizeBase = baseDesktopIconSize

// DesktopIconSize 返回图标位图实际像素尺寸（随 DPI + 档位缩放）
func DesktopIconSize() int { return DpiPx(desktopIconSizeBase) }

// DesktopIconTop 返回图标在磁贴内的顶部偏移（随 DPI 缩放）
func DesktopIconTop() int { return DpiPx(baseDesktopIconTop) }

// DesktopIconLabelTop 返回标签文字起始 Y 偏移 = 图标高度 + 顶部偏移 + 2px 间隙
// 跟随图标尺寸，保证文字不被图标覆盖
func DesktopIconLabelTop() int {
	return DpiPx(desktopIconSizeBase+baseDesktopIconTop) + DpiPx(2)
}

// DesktopIconLineHeight 返回每行文字高度（随 DPI + 字号档位缩放）
//
// 行高与字号成正比，避免小字号下行高过大导致磁贴中间空白过多：
//   - 11pt(大档) → 24px（基准）
//   - 9pt(中档)  → 20px
//   - 8pt(小档)  → 17px
//
// 计算方式：baseDesktopIconLineH(24) × 当前字号 / 11（大档基准字号），再按 DPI 缩放
func DesktopIconLineHeight() int {
	iconFontMu.RLock()
	size := iconFontSize
	iconFontMu.RUnlock()
	if size <= 0 {
		size = 11
	}
	// 行高 = 基准行高 × (字号 / 基准字号)，向上取整避免行间挤压
	lineH := baseDesktopIconLineH * size / 11
	if lineH < 14 {
		lineH = 14 // 最小行高保护
	}
	lineH += 2 // 额外 padding 防止文字底部被裁剪
	return DpiPx(lineH)
}

// DesktopIconGap 返回图标磁贴间距（随 DPI + 档位缩放）
// 间距随档位变化：大档 10px、中档 8px、小档 6px（接近系统桌面小图标观感）
func DesktopIconGap() int {
	iconGapMu.RLock()
	gap := iconGap
	iconGapMu.RUnlock()
	return DpiPx(gap)
}

// setIconGap 线程安全地设置磁贴间距（按图标档位调整）
func setIconGap(gap int) {
	iconGapMu.Lock()
	defer iconGapMu.Unlock()
	iconGap = gap
}

// FreeGridLeft 返回未分组网格左边距（随 DPI 缩放）
func FreeGridLeft() int { return DpiPx(baseFreeGridLeft) }

// FreeGridTop 返回未分组网格上边距（随 DPI 缩放）
func FreeGridTop() int { return DpiPx(baseFreeGridTop) }

// AutoSelectIconSizeByResolution 根据屏幕分辨率自动选择图标大小档位。
// 低分辨率（高度 < 1080）使用小图标，高分辨率（高度 >= 1080）使用中图标。
// 中档图标尺寸与高档相同（48px），仅字号和间距更紧凑。
// 仅在用户未手动设置过时生效（启动时调用）。
func AutoSelectIconSizeByResolution(screenW, screenH int) {
	var level int
	switch {
	case screenH < 1080:
		level = 2 // iconSizeSmall (32px)
	default:
		level = 1 // iconSizeMedium (48px)
	}
	SetDesktopIconSize(level)
	logger.Debug("autoSelectIconSize: screen=%dx%d, level=%d", screenW, screenH, level)
}

// ============================================================
// 磁贴尺寸（宽度动态测量，高度公式计算）
// ============================================================

var (
	desktopIconItemWidth  = 132
	desktopIconItemHeight = 104 // = DesktopIconLabelTop + DesktopIconLineHeight*2 + 4
	tileSizeOnce          sync.Once
	tileSizeMu            sync.Mutex // 保护 tileSizeOnce 重置的并发安全
)

// TileWidth 返回图标磁贴像素宽度（动态测量）
func TileWidth() int { return desktopIconItemWidth }

// TileHeight 返回图标磁贴像素高度（动态计算）
func TileHeight() int { return desktopIconItemHeight }

// TileColWidth 返回图标磁贴列宽（磁贴宽度 + 间距）
func TileColWidth() int { return desktopIconItemWidth + DesktopIconGap() }

// end用 Win32 GetTextExtentPoint32 真实测量文本尺寸，
// 确保磁贴宽度能容纳 4 个汉字或 9 个西文字符（取较大值）。
func EnsureTileSizeMeasured(_ *walk.Canvas) {
	// 检查是否需要强制重新测量（图标大小变更后）
	if IsTileRemeasureNeeded() {
		tileSizeMu.Lock()
		tileSizeOnce = sync.Once{}
		tileSizeMu.Unlock()
	}
	tileSizeOnce.Do(func() {
		font := GetIconFont()
		if font == nil {
			logger.Debug("ensureTileSizeMeasured: GetIconFont returned nil")
			return
		}
		defer font.Dispose()

		family := font.Family()
		ptSize := font.PointSize()
		style := font.Style()

		// 使用 Win32 API 精确测量文本（避免 walk.MeasureTextPixels 的误差）
		hdc := win.CreateCompatibleDC(0)
		if hdc == 0 {
			logger.Debug("ensureTileSizeMeasured: CreateCompatibleDC failed")
			return
		}
		defer win.DeleteDC(hdc)

		dpi := int(win.GetDeviceCaps(hdc, win.LOGPIXELSY))
		if dpi <= 0 {
			dpi = 96
		}

		// 用 CreateFontIndirect 创建与 GetIconFont 相同属性的 Win32 字体
		var lf win.LOGFONT
		lf.LfHeight = -win.MulDiv(int32(ptSize), int32(dpi), 72)
		if style&walk.FontBold > 0 {
			lf.LfWeight = win.FW_BOLD
		} else {
			lf.LfWeight = win.FW_NORMAL
		}
		if style&walk.FontItalic > 0 {
			lf.LfItalic = 1
		}
		if style&walk.FontUnderline > 0 {
			lf.LfUnderline = 1
		}
		if style&walk.FontStrikeOut > 0 {
			lf.LfStrikeOut = 1
		}
		lf.LfCharSet = win.DEFAULT_CHARSET
		lf.LfOutPrecision = win.OUT_TT_PRECIS
		lf.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
		lf.LfQuality = win.CLEARTYPE_QUALITY
		lf.LfPitchAndFamily = win.VARIABLE_PITCH | win.FF_SWISS

		src := syscall.StringToUTF16(family)
		copy(lf.LfFaceName[:], src)

		hFont := win.CreateFontIndirect(&lf)
		if hFont == 0 {
			logger.Debug("ensureTileSizeMeasured: CreateFontIndirect failed")
			return
		}
		defer win.DeleteObject(win.HGDIOBJ(hFont))

		oldFont := win.SelectObject(hdc, win.HGDIOBJ(hFont))
		if oldFont == 0 {
			logger.Debug("ensureTileSizeMeasured: SelectObject failed")
			return
		}
		defer win.SelectObject(hdc, oldFont)

		// 测量 4 个汉字的实际像素宽度
		var cjkSize win.SIZE
		cjkText, _ := syscall.UTF16PtrFromString("中中中中")
		win.GetTextExtentPoint32(hdc, cjkText, 4, &cjkSize)

		// 测量 9 个西文字符的实际像素宽度
		var asciiSize win.SIZE
		asciiText, _ := syscall.UTF16PtrFromString("ABCDEFGHI")
		win.GetTextExtentPoint32(hdc, asciiText, 9, &asciiSize)

		cjkW := int(cjkSize.CX)
		asciiW := int(asciiSize.CX)
		tileW := cjkW
		if asciiW > tileW {
			tileW = asciiW
		}
		tileW += 8 // 左右安全 padding

		// 磁贴宽度 = max(图标尺寸 + 24, 文字宽度 + padding)
		// 基础磁贴宽按图标档位缩放，避免小图标下左右空白过多：
		//   大档(48)→72，中档(48)→72，小档(32)→56
		baseTileW := desktopIconSizeBase + 24

		desktopIconItemWidth = baseTileW
		if tileW > desktopIconItemWidth {
			desktopIconItemWidth = tileW
		}
		// 封顶防止极端字体导致磁贴过宽
		if desktopIconItemWidth > 160 {
			desktopIconItemWidth = 160
		}
		// 磁贴高度 = 标签起始 Y + 2 行文字高度 + 4px 底部 padding
		desktopIconItemHeight = DesktopIconLabelTop() + DesktopIconLineHeight()*2 + 4

		logger.Debug("ensureTileSizeMeasured: font=%s %dpt, tile=%dx%d (baseTileW=%d, measured cjk=%d ascii=%d, dpi=%d)",
			family, ptSize, desktopIconItemWidth, desktopIconItemHeight, baseTileW, cjkW, asciiW, CurrentDPI())
	})
}

var (
	iconFontName = "宋体"
	iconFontSize = 11
	iconFontMu   sync.RWMutex
	iconGap      = 8  // 当前图标档位对应的磁贴间距（基线像素），实际像素 = DpiPx(iconGap)
	iconGapMu    sync.RWMutex

	cardFontName   = "Consolas"
	cardFontSize   = 14
	cardFontPreset = "consolas"
	cardFontMu     sync.RWMutex
)

// InitIconFont 初始化图标标签字体配置（应用启动时调用）
func InitIconFont(name string, size int) {
	iconFontMu.Lock()
	defer iconFontMu.Unlock()
	if name != "" {
		iconFontName = name
	}
	if size > 0 {
		iconFontSize = size
	}
}

// GetIconFont 获取图标标签字体（带回退）
func GetIconFont() *walk.Font {
	iconFontMu.RLock()
	name := iconFontName
	size := iconFontSize
	iconFontMu.RUnlock()

	font, err := walk.NewFont(name, size, 0)
	if err != nil && name != "宋体" && name != "SimSun" {
		font, err = walk.NewFont("宋体", size, 0)
	}
	if err != nil {
		font, _ = walk.NewFont("宋体", 11, 0)
	}
	return font
}

// InitCardFont 初始化卡片标题字体配置（应用启动时调用）
// name/size 仅在 preset == "custom" 或 preset 为空时使用
func InitCardFont(name string, size int) {
	cardFontMu.Lock()
	defer cardFontMu.Unlock()
	if name != "" {
		cardFontName = name
	}
	if size > 0 {
		cardFontSize = size
	}
}

// InitCardFontPreset 初始化卡片标题字体预设（应用启动时调用）
// preset 为 "consolas" / "segoeui" / "yahei" / "custom" 之一
// 当 preset 为 "custom" 或未识别时，保留 cardFontName/cardFontSize 不变
func InitCardFontPreset(preset string) {
	cardFontMu.Lock()
	defer cardFontMu.Unlock()
	if p, ok := config.CardFontPresets[preset]; ok {
		cardFontPreset = preset
		cardFontName = p.Name
		cardFontSize = p.Size
		return
	}
	// 未识别或 "custom"：保持现有 name/size
	cardFontPreset = "custom"
}

// GetCardFontPreset 返回当前预设名
func GetCardFontPreset() string {
	cardFontMu.RLock()
	defer cardFontMu.RUnlock()
	return cardFontPreset
}

// GetCardTitleFont 获取卡片标题字体（带回退）
func GetCardTitleFont() *walk.Font {
	cardFontMu.RLock()
	name := cardFontName
	size := cardFontSize
	cardFontMu.RUnlock()

	font, err := walk.NewFont(name, size, 0)
	if err != nil && name != "Consolas" {
		font, err = walk.NewFont("Consolas", size, 0)
	}
	if err != nil {
		font, _ = walk.NewFont("Consolas", 14, 0)
	}
	return font
}

// ParseHexColor 解析 #RRGGBB 或 #RRGGBBAA 格式的颜色
func ParseHexColor(hex string) color.RGBA {
	hex = strings.TrimPrefix(hex, "#")

	if len(hex) == 6 {
		hex += "FF"
	}
	if len(hex) != 8 {
		return color.RGBA{0x34, 0x23, 0x33, 0xB8} // 默认深紫
	}

	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	a, _ := strconv.ParseUint(hex[6:8], 16, 8)

	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
}

// ColorToHex 将颜色转换为 #RRGGBBAA 格式
func ColorToHex(c color.RGBA) string {
	return "#" + hexByte(c.R) + hexByte(c.G) + hexByte(c.B) + hexByte(c.A)
}

// ColorToHexRGB 将颜色转换为 #RRGGBB 格式（忽略 Alpha，仅 RGB）
func ColorToHexRGB(c color.RGBA) string {
	return "#" + hexByte(c.R) + hexByte(c.G) + hexByte(c.B)
}

func hexByte(b byte) string {
	const hexChars = "0123456789ABCDEF"
	return string([]byte{hexChars[b>>4], hexChars[b&0x0F]})
}

// ClampInt 限制整数在指定范围内
func ClampInt(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// SplitTextToLines 将文本拆分为多行。
//
// 使用视觉宽度感知的拆分方式：
//   - ASCII/半角字符 = 1 个宽度单位，全角/CJK 字符 = 2 个宽度单位
//   - maxCJK 表示一行最多容纳的 全角字符数（视觉宽度上限 = maxCJK × 2）
//   - 例如 maxCJK=4 时：最多 4 个汉字，或 8 个半角字符，或混合搭配不超过视觉宽度 8
func SplitTextToLines(text string, maxCJK int) []string {
	if maxCJK <= 0 {
		return nil
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	charWidth := func(r rune) int {
		if r <= 0xFF {
			return 1
		}
		return 2
	}

	var lines []string
	start := 0
	currentWidth := 0

	maxVisualWidth := maxCJK * 2
	for i := 0; i < len(runes); i++ {
		w := charWidth(runes[i])
		if currentWidth+w > maxVisualWidth {
			lines = append(lines, string(runes[start:i]))
			start = i
			currentWidth = w
		} else {
			currentWidth += w
		}
	}

	// 剩余部分
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}

	return lines
}

// TruncateText 按视觉宽度截断文字，超出部分用省略号。
// CJK/全角字符计 2 个宽度单位，ASCII/半角字符计 1 个宽度单位。
// maxWidth 为总视觉宽度上限（含省略号 "…"）。
func TruncateText(text string, maxWidth int) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}

	charWidth := func(r rune) int {
		if r <= 0xFF {
			return 1
		}
		return 2
	}

	// 计算全文视觉宽度
	totalWidth := 0
	for _, r := range runes {
		totalWidth += charWidth(r)
	}
	if totalWidth <= maxWidth {
		return text
	}

	// 需要截断：为 "…"（宽度 2）预留空间
	available := maxWidth - 2
	if available <= 0 {
		return "…"
	}

	width := 0
	endIdx := 0
	for i, r := range runes {
		w := charWidth(r)
		if width+w > available {
			break
		}
		width += w
		endIdx = i + 1
	}

	return string(runes[:endIdx]) + "…"
}

// GetIconDisplayLines 将图标名称拆分为至多 2 行显示文本。
// 若超出 2 行，第 2 行末尾用省略号截断（非选中状态的通用展示逻辑）。
func GetIconDisplayLines(name string, maxCJK int) []string {
	lines := SplitTextToLines(name, maxCJK)
	if len(lines) > 2 {
		lines = lines[:2]
		lines[1] = TruncateText(lines[1], 7)
	}
	return lines
}

// drawAlphaRect 用完全不透明的实色位图 + opacity 参数实现半透明矩形。
// 透明度通过 DrawBitmapWithOpacityPixels 的 opacity 参数控制（而非位图自身的 alpha 通道），
// 这样 walk 的 alphaBlendPart 中 opacity != 255 就不会走 StretchBlt 优化分支，
// 始终走 AlphaBlend，保证透明裁剪正确，半透明高亮可以叠加在卡片半透明背景和壁纸之上。
func drawAlphaRect(canvas *walk.Canvas, bounds walk.Rectangle, r, g, b, a uint8) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix[0] = r
	img.Pix[1] = g
	img.Pix[2] = b
	img.Pix[3] = 255 // 位图自身完全不透明，透明度由 opacity 参数控制
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	defer bmp.Dispose()
	canvas.DrawBitmapWithOpacityPixels(bmp, bounds, a) // 用 a 作为 opacity 参数
}

// DrawHoverRect 绘制悬停高亮（半透明蓝色背景，细边框）
func DrawHoverRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 半透明填充（固定 ~20% 蓝）
	drawAlphaRect(canvas, bounds, 0x00, 0x45, 0x8A, 50)
	// 1px 边框
	borderPen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x00, 0x3A, 0x7A))
	if err == nil {
		canvas.DrawRectanglePixels(borderPen, walk.Rectangle{
			X: bounds.X, Y: bounds.Y,
			Width: bounds.Width - 1, Height: bounds.Height - 1,
		})
		borderPen.Dispose()
	}
}

// DrawSelectionRect 绘制选中/焦点高亮（半透明蓝色背景，细边框）
func DrawSelectionRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 半透明填充（固定 ~28% 蓝），叠加在卡片半透明背景之上
	drawAlphaRect(canvas, bounds, 0x00, 0x55, 0xAA, 70)
	// 1px 边框
	borderPen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x00, 0x3A, 0x7A))
	if err == nil {
		canvas.DrawRectanglePixels(borderPen, walk.Rectangle{
			X: bounds.X, Y: bounds.Y,
			Width: bounds.Width - 1, Height: bounds.Height - 1,
		})
		borderPen.Dispose()
	}
}

// ============================================================
// Win32 函数（供组内 group_card.go 等文件使用）
// ============================================================

var (
	user32DLL           = syscall.NewLazyDLL("user32.dll")
	procCreateWindowExW = user32DLL.NewProc("CreateWindowExW")
)