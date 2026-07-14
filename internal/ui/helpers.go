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

	"desktop_go/internal/logger"
)

// 图标磁贴规格常量
const (
	DesktopIconSize       = 48
	DesktopIconTop        = 2
	DesktopIconLabelTop   = 52
	DesktopIconLineHeight = 24
	DesktopIconGap        = 8  // 图标磁贴间距
	LongPressDragDelay    = 1 * time.Second  // 卡片拖拽延迟（标题栏长按）
	IconDragDelay         = 300 * time.Millisecond  // 图标拖拽延迟（卡片内/未分组图标）
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

	cardFontName = "宋体"
	cardFontSize = 14
	cardFontMu   sync.RWMutex
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

// GetCardTitleFont 获取卡片标题字体（带回退）
func GetCardTitleFont() *walk.Font {
	cardFontMu.RLock()
	name := cardFontName
	size := cardFontSize
	cardFontMu.RUnlock()

	font, err := walk.NewFont(name, size, walk.FontBold)
	if err != nil && name != "宋体" && name != "SimSun" {
		font, err = walk.NewFont("宋体", size, walk.FontBold)
	}
	if err != nil {
		font, _ = walk.NewFont("宋体", 14, walk.FontBold)
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

// SplitTextToLines 将文本拆分为多行，优先在空格处换行，最大 maxRunes 个汉字/行
//
// ASCII 连续段规则（半角符号与英文字母）：
//   - 长度 >=7 且 <=9：按一行算
//   - 长度 <6：合并到下一个段（追加到其头部），重新计算
//   - 长度 >9：分行，每次取 9 个作为一行，剩余部分合并到下一个段
func SplitTextToLines(text string, maxCJK int) []string {
	if maxCJK <= 0 {
		return nil
	}
	maxWidth := maxCJK * 2 // 全角2单位，半角1单位
	runes := []rune(text)

	// 解析为连续同类型字符段（ASCII/半角 vs 全角）
	const (
		segASCII = iota
		segFull
	)
	type segment struct {
		typ   int
		runes []rune
	}

	var segs []segment
	for i := 0; i < len(runes); {
		isAscii := runes[i] <= 0xFF
		j := i
		for j < len(runes) && (runes[j] <= 0xFF) == isAscii {
			j++
		}
		typ := segFull
		if isAscii {
			typ = segASCII
		}
		segs = append(segs, segment{typ: typ, runes: runes[i:j]})
		i = j
	}

	var lines []string
	for s := 0; s < len(segs); s++ {
		seg := segs[s]
		if seg.typ == segASCII {
			l := len(seg.runes)
			switch {
			case l >= 7 && l <= 9:
				lines = append(lines, string(seg.runes))
			case l < 6:
				if s+1 < len(segs) {
					// 合并到下一个段，按前面规则重新计算
					segs[s+1].runes = append(seg.runes, segs[s+1].runes...)
				} else {
					lines = append(lines, string(seg.runes))
				}
			case l > 9:
				pos := 0
				for pos < len(seg.runes) {
					end := pos + 9
					if end >= len(seg.runes) {
						// 剩余部分合并到下一个段
						if s+1 < len(segs) {
							segs[s+1].runes = append(seg.runes[pos:], segs[s+1].runes...)
						} else {
							lines = append(lines, string(seg.runes[pos:]))
						}
						break
					}
					lines = append(lines, string(seg.runes[pos:end]))
					pos = end
				}
			default: // l == 6，直接作为一行
				lines = append(lines, string(seg.runes))
			}
		} else {
			// 全角字符段（可能混有合并进来的 ASCII）：按原始宽度逻辑处理
			pos := 0
			for pos < len(seg.runes) {
				width := 0
				end := pos
				lastSpace := -1

				for end < len(seg.runes) {
					r := seg.runes[end]
					w := 2
					if r <= 0xFF {
						w = 1
					}
					if width+w > maxWidth {
						break
					}
					width += w
					end++
					if r == ' ' || r == '\t' {
						lastSpace = end - 1
					}
				}

				if end >= len(seg.runes) {
					lines = append(lines, string(seg.runes[pos:]))
					break
				}

				if lastSpace >= pos {
					lines = append(lines, string(seg.runes[pos:lastSpace]))
					pos = lastSpace + 1
				} else {
					if end > pos {
						lines = append(lines, string(seg.runes[pos:end]))
						pos = end
					} else {
						lines = append(lines, string(seg.runes[pos:pos+1]))
						pos++
					}
				}
			}
		}
	}

	return lines
}

// TruncateText 截断文字，超出部分用省略号
func TruncateText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes-1]) + "…"
}

func DrawHoverRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillColor := color.RGBA{R: 0x00, G: 0x45, B: 0x8A, A: 0x15}
	borderColor := color.RGBA{R: 0x00, G: 0x5A, B: 0xAD, A: 0x20}

	img := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			if x == 0 || x == bounds.Width-1 || y == 0 || y == bounds.Height-1 {
				img.SetRGBA(x, y, borderColor)
			} else {
				img.SetRGBA(x, y, fillColor)
			}
		}
	}
	bmp, err := walk.NewBitmapFromImage(img)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
}

// DrawSelectionRect 绘制选中高亮（深蓝色边框，半透明背景）
func DrawSelectionRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillColor := color.RGBA{R: 0x00, G: 0x4D, B: 0x96, A: 0x18}
	borderColor := color.RGBA{R: 0x00, G: 0x6B, B: 0xCC, A: 0x30}

	img := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	for y := 0; y < bounds.Height; y++ {
		for x := 0; x < bounds.Width; x++ {
			if x == 0 || x == bounds.Width-1 || y == 0 || y == bounds.Height-1 {
				img.SetRGBA(x, y, borderColor)
			} else {
				img.SetRGBA(x, y, fillColor)
			}
		}
	}
	// 外边框更亮
	outerBorder := color.RGBA{R: 0x00, G: 0x7D, B: 0xE0, A: 0x40}
	for x := 0; x < bounds.Width; x++ {
		img.SetRGBA(x, 0, outerBorder)
		img.SetRGBA(x, bounds.Height-1, outerBorder)
	}
	for y := 0; y < bounds.Height; y++ {
		img.SetRGBA(0, y, outerBorder)
		img.SetRGBA(bounds.Width-1, y, outerBorder)
	}

	bmp, err := walk.NewBitmapFromImage(img)
	if err == nil {
		defer bmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)
	}
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
