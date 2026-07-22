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

// 图标磁贴规格常量
const (
	DesktopIconSize       = 48
	DesktopIconTop        = 2
	DesktopIconLabelTop   = 52
	DesktopIconLineHeight = 24
	DesktopIconGap        = 8                      // 图标磁贴间距
	LongPressDragDelay    = 1 * time.Second        // 卡片拖拽延迟（标题栏长按）
	IconDragDelay         = 300 * time.Millisecond // 图标拖拽延迟（卡片内/未分组图标）
)

var (
	desktopIconItemWidth  = 132
	desktopIconItemHeight = 104 // DesktopIconLabelTop(52) + DesktopIconLineHeight(24)*2 + 4
	tileSizeOnce          sync.Once
)

// TileWidth 返回图标磁贴像素宽度（动态计算）
func TileWidth() int { return desktopIconItemWidth }

// TileHeight 返回图标磁贴像素高度（动态计算）
func TileHeight() int { return desktopIconItemHeight }

// TileColWidth 返回图标磁贴列宽（磁贴宽度 + 间距）
func TileColWidth() int { return desktopIconItemWidth + DesktopIconGap }

// end用 Win32 GetTextExtentPoint32 真实测量文本尺寸，
// 确保磁贴宽度能容纳 4 个汉字或 9 个西文字符（取较大值）。
func EnsureTileSizeMeasured(_ *walk.Canvas) {
	// 检查是否需要强制重新测量（图标大小变更后）
	if IsTileRemeasureNeeded() {
		// 重置 sync.Once，允许重新测量
		tileSizeOnce = sync.Once{}
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

		desktopIconItemWidth = tileW
		if desktopIconItemWidth < 80 {
			desktopIconItemWidth = 80
		}
		desktopIconItemHeight = DesktopIconLabelTop + DesktopIconLineHeight*2 + 4

		logger.Debug("ensureTileSizeMeasured: font=%s %dpt, tile=%dx%d (measured cjk=%d ascii=%d)",
			family, ptSize, desktopIconItemWidth, desktopIconItemHeight, cjkW, asciiW)
	})
}

var (
	iconFontName = "宋体"
	iconFontSize = 11
	iconFontMu   sync.RWMutex

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

// drawBorderedRect 创建填充+1px边框的 RGBA 图像
func drawBorderedRect(bounds walk.Rectangle, fill, border color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			if x == 0 || x == bounds.Width-1 || y == 0 || y == bounds.Height-1 {
				img.SetRGBA(x, y, border)
			} else {
				img.SetRGBA(x, y, fill)
			}
		}
	}
	return img
}

// drawRGBAImage 将 RGBA 图像绘制到 canvas
func drawRGBAImage(canvas *walk.Canvas, img *image.RGBA, bounds walk.Rectangle) {
	bmp, err := walk.NewBitmapFromImage(img)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
}

// DrawHoverRect 绘制悬停高亮（半透明背景，细边框）
func DrawHoverRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillColor := color.RGBA{R: 0x00, G: 0x45, B: 0x8A, A: 0x15}
	borderColor := color.RGBA{R: 0x00, G: 0x5A, B: 0xAD, A: 0x20}
	img := drawBorderedRect(bounds, fillColor, borderColor)
	drawRGBAImage(canvas, img, bounds)
}

// DrawSelectionRect 绘制选中高亮（半透明填充，无边框）
func DrawSelectionRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillColor := color.RGBA{R: 0x00, G: 0x55, B: 0xAA, A: 0x80}
	img := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			img.SetRGBA(x, y, fillColor)
		}
	}
	drawRGBAImage(canvas, img, bounds)
}

// CreateColorBitmap 创建纯色 RGBA 位图（公开方法，供 desktop 包使用）
func CreateColorBitmap(w, h int, r, g, b, a byte) *walk.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return nil
	}
	return bmp
}
