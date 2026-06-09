package ui

import (
	"image/color"
	"strconv"
	"strings"
)

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
