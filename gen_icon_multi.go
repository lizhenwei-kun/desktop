//go:build ignore
// +build ignore

// 生成多尺寸应用图标 internal/resources/app.ico
package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	sizes := []int{16, 32, 48, 64, 128, 256}
	imgs := make([]*image.RGBA, len(sizes))
	for i, s := range sizes {
		imgs[i] = createAppIcon(s)
	}
	saveICO(imgs, "internal/resources/app.ico")
	// 也生成 png 方便查看
	f, _ := os.Create("internal/resources/app_256.png")
	png.Encode(f, imgs[len(imgs)-1])
	f.Close()
}

func createAppIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2
	radius := float64(size)/2 - 1
	bgColor := color.RGBA{R: 0x27, G: 0x6B, B: 0xA6, A: 0xFF}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= radius-0.5 {
				img.SetRGBA(x, y, bgColor)
			} else if dist <= radius+0.5 {
				alpha := uint8(float64(bgColor.A) * (radius + 0.5 - dist))
				img.SetRGBA(x, y, color.RGBA{R: bgColor.R, G: bgColor.G, B: bgColor.B, A: alpha})
			}
		}
	}
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	gap := size / 16
	gridStart := size / 4
	gridEnd := size * 3 / 4
	cellSize := (gridEnd - gridStart - gap) / 2
	fillRect(img, gridStart, gridStart, gridStart+cellSize, gridStart+cellSize, white)
	fillRect(img, gridStart+cellSize+gap, gridStart, gridEnd, gridStart+cellSize, white)
	fillRect(img, gridStart, gridStart+cellSize+gap, gridStart+cellSize, gridEnd, white)
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

// saveICO 保存多尺寸 ICO 文件
func saveICO(imgs []*image.RGBA, path string) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	count := len(imgs)
	sizes := make([]int, count)
	for i, img := range imgs {
		sizes[i] = img.Bounds().Dx()
	}
	// ICO Header
	binary.Write(f, binary.LittleEndian, uint16(0))  // reserved
	binary.Write(f, binary.LittleEndian, uint16(1))  // ICO type
	binary.Write(f, binary.LittleEndian, uint16(count)) // image count

	// Directory entries + image data offset
	dirSize := 6 + count*16
	offset := uint32(dirSize)

	// 收集所有图像的 PNG 压缩数据（256x256 用 PNG，其余用 BMP）
	type entry struct {
		w, h int
		data []byte
		bmp  bool // true=BMP format, false=PNG format
	}
	entries := make([]entry, count)

	for idx, img := range imgs {
		s := img.Bounds().Dx()
		w, h := img.Bounds().Dx(), img.Bounds().Dy()

		if s <= 64 {
			// BMP format (BGRA, bottom-up)
			pixels := make([]byte, w*h*4)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					c := img.RGBAAt(x, y)
					destY := h - 1 - y
					idx := (destY*w + x) * 4
					pixels[idx+0] = c.B
					pixels[idx+1] = c.G
					pixels[idx+2] = c.R
					pixels[idx+3] = c.A
				}
			}
			andMaskRowSize := ((w + 31) / 32) * 4
			andMask := make([]byte, andMaskRowSize*h)

			bmpHeaderSize := 40
			imageSize := len(pixels) + len(andMask)
			totalSize := bmpHeaderSize + imageSize

			buf := make([]byte, totalSize)
			// BITMAPINFOHEADER
			binary.LittleEndian.PutUint32(buf[0:], uint32(bmpHeaderSize))
			binary.LittleEndian.PutUint32(buf[4:], uint32(w))
			binary.LittleEndian.PutUint32(buf[8:], uint32(h*2))
			binary.LittleEndian.PutUint16(buf[12:], 1)
			binary.LittleEndian.PutUint16(buf[14:], 32)
			binary.LittleEndian.PutUint32(buf[16:], 0)
			binary.LittleEndian.PutUint32(buf[20:], uint32(imageSize))
			binary.LittleEndian.PutUint32(buf[24:], 0)
			binary.LittleEndian.PutUint32(buf[28:], 0)
			binary.LittleEndian.PutUint32(buf[32:], 0)
			binary.LittleEndian.PutUint32(buf[36:], 0)
			copy(buf[40:], pixels)
			copy(buf[40+len(pixels):], andMask)

			entries[i] = entry{w: w, h: h, data: buf, bmp: true}
		} else {
			// PNG format for large icons
			r, _ := os.CreateTemp("", "icon*.png")
			png.Encode(r, img)
			r.Close()
			pngData, _ := os.ReadFile(r.Name())
			os.Remove(r.Name())
			entries[i] = entry{w: 0, h: 0, data: pngData, bmp: false}
		}
	}

	// Write directory entries
	for i, e := range entries {
		iw := byte(e.w)
		if e.w >= 256 {
			iw = 0
		}
		ih := byte(e.h)
		if e.h >= 256 {
			ih = 0
		}
		f.Write([]byte{iw, ih, 0, 0}) // palette
		binary.Write(f, binary.LittleEndian, uint16(1))  // color planes
		binary.Write(f, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(f, binary.LittleEndian, uint32(len(e.data))) // size
		binary.Write(f, binary.LittleEndian, offset) // offset
		offset += uint32(len(e.data))
	}

	// Write image data
	for _, e := range entries {
		f.Write(e.data)
	}
}
