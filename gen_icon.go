//go:build ignore
// +build ignore

// 生成应用图标 internal/resources/app.ico，供 rsrc 嵌入到 exe 中
package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"os"
)

func main() {
	img := createAppIcon(64)
	saveICO(img, "internal/resources/app.ico")
}

func createAppIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	center := float64(size) / 2
	radius := float64(size)/2 - 1
	bgColor := color.RGBA{R: 0x27, G: 0x6B, B: 0xA6, A: 0xFF}

	// 绘制圆形背景（带抗锯齿）
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= radius-0.5 {
				img.SetRGBA(x, y, bgColor)
			} else if dist <= radius+0.5 {
				// 边缘抗锯齿
				alpha := uint8(float64(bgColor.A) * (radius + 0.5 - dist))
				img.SetRGBA(x, y, color.RGBA{R: bgColor.R, G: bgColor.G, B: bgColor.B, A: alpha})
			}
		}
	}

	// 绘制白色 2x2 网格（代表桌面分组）
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	gap := size / 16
	gridStart := size / 4
	gridEnd := size * 3 / 4
	cellSize := (gridEnd - gridStart - gap) / 2

	// 左上格子
	fillRect(img, gridStart, gridStart, gridStart+cellSize, gridStart+cellSize, white)
	// 右上格子
	fillRect(img, gridStart+cellSize+gap, gridStart, gridEnd, gridStart+cellSize, white)
	// 左下格子
	fillRect(img, gridStart, gridStart+cellSize+gap, gridStart+cellSize, gridEnd, white)
	// 右下格子
	fillRect(img, gridStart+cellSize+gap, gridStart+cellSize+gap, gridEnd, gridEnd, white)

	return img
}

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func saveICO(img *image.RGBA, path string) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// 像素数据（ICO 中 BMP 是自底向上的 BGRA）
	pixelData := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := img.RGBAAt(x, y)
			destY := height - 1 - y
			idx := (destY*width + x) * 4
			pixelData[idx+0] = c.B
			pixelData[idx+1] = c.G
			pixelData[idx+2] = c.R
			pixelData[idx+3] = c.A
		}
	}

	// AND mask
	andMaskRowSize := ((width + 31) / 32) * 4
	andMask := make([]byte, andMaskRowSize*height)

	bmpHeaderSize := 40
	imageSize := len(pixelData) + len(andMask)

	// ICO Header
	binary.Write(f, binary.LittleEndian, uint16(0))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(1))

	// Directory Entry
	w := byte(width)
	h := byte(height)
	if width >= 256 {
		w = 0
	}
	if height >= 256 {
		h = 0
	}
	f.Write([]byte{w, h, 0, 0})
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(32))
	binary.Write(f, binary.LittleEndian, uint32(bmpHeaderSize+imageSize))
	binary.Write(f, binary.LittleEndian, uint32(22))

	// BITMAPINFOHEADER
	binary.Write(f, binary.LittleEndian, uint32(bmpHeaderSize))
	binary.Write(f, binary.LittleEndian, int32(width))
	binary.Write(f, binary.LittleEndian, int32(height*2))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(32))
	binary.Write(f, binary.LittleEndian, uint32(0))
	binary.Write(f, binary.LittleEndian, uint32(imageSize))
	binary.Write(f, binary.LittleEndian, int32(0))
	binary.Write(f, binary.LittleEndian, int32(0))
	binary.Write(f, binary.LittleEndian, uint32(0))
	binary.Write(f, binary.LittleEndian, uint32(0))

	f.Write(pixelData)
	f.Write(andMask)
}
