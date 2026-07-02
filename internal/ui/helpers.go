package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/logger"
)

var (
	desktopIconItemWidth  = 132
	desktopIconItemHeight = 132
	tileSizeOnce          sync.Once
)

// TileWidth 返回图标磁贴像素宽度（动态计算）
func TileWidth() int { return desktopIconItemWidth }

// TileHeight 返回图标磁贴像素高度（动态计算）
func TileHeight() int { return desktopIconItemHeight }

// end用 Win32 GetTextExtentPoint32 真实测量文本尺寸，
// 确保磁贴宽度能容纳 4 个汉字或 9 个西文字符（取较大值）。
func ensureTileSizeMeasured(_ *walk.Canvas) {
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
		desktopIconItemHeight = desktopIconLabelTop + desktopIconLineHeight*2 + 4

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

// TruncateText 截断文字，超出部分用省略号
func TruncateText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes-1]) + "…"
}

func drawHoverRect(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillColor := color.RGBA{R: 0xE8, G: 0xF4, B: 0xFF, A: 0x0D}
	borderColor := color.RGBA{R: 0x4A, G: 0xA0, B: 0xFF, A: 0x0D}

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
