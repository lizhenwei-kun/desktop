package ui

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// 图标磁贴规格常量
const (
	desktopIconItemWidth  = 74
	desktopIconItemHeight = 96
	desktopIconSize       = 48
	desktopIconTop        = 4
	desktopIconLabelTop   = 56
	desktopIconLineHeight = 17
	desktopIconTextSize   = 9
	longPressDragDelay    = 3 * time.Second
)

// DraggableIcon 可拖动图标组件
type DraggableIcon struct {
	widget       *walk.CustomWidget
	filePath     string
	displayName  string
	iconImg      image.Image
	isPressed    bool
	isDragging   bool
	pressTime    time.Time
	onDoubleClick func()
	onDragStart  func(filePath string)
	onDragEnd    func(filePath string, x, y int)
	groupName    string
}

// NewDraggableIcon 创建可拖动图标
func NewDraggableIcon(parent walk.Container, filePath, groupName string, executor *ProgramExecutor) (*DraggableIcon, error) {
	di := &DraggableIcon{
		filePath:    filePath,
		displayName: getDisplayName(filePath),
		groupName:   groupName,
	}

	// 加载图标
	extractor := NewIconExtractor()
	di.iconImg, _ = extractor.GetIconImage(filePath)

	// 创建 CustomWidget
	var err error
	di.widget, err = walk.NewCustomWidgetPixels(parent, 0, di.paint)
	if err != nil {
		return nil, err
	}

	di.widget.SetMinMaxSizePixels(
		walk.Size{Width: desktopIconItemWidth, Height: desktopIconItemHeight},
		walk.Size{Width: desktopIconItemWidth, Height: desktopIconItemHeight},
	)
	di.widget.SetPaintMode(walk.PaintBuffered)
	di.widget.SetInvalidatesOnResize(true)

	// 双击打开
	di.onDoubleClick = func() {
		executor.Execute(filePath)
	}

	// 鼠标事件
	di.widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			di.isPressed = true
			di.pressTime = time.Now()
			// 启动长按检测
			go di.checkLongPress()
		}
	})

	di.widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		di.isPressed = false
		di.isDragging = false
	})

	// 双击
	di.widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			// walk 没有直接双击事件，通过计时判断
		}
	})

	return di, nil
}

// checkLongPress 检测长按触发拖拽
func (di *DraggableIcon) checkLongPress() {
	time.Sleep(longPressDragDelay)
	if di.isPressed {
		di.isDragging = true
		di.widget.Synchronize(func() {
			if di.onDragStart != nil {
				di.onDragStart(di.filePath)
			}
			// 改变光标为四向箭头
			di.widget.SetCursor(walk.CursorSizeAll())
		})
	}
}

// paint 绘制图标磁贴
func (di *DraggableIcon) paint(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := di.widget.ClientBoundsPixels()

	// 绘制图标
	if di.iconImg != nil {
		di.drawIcon(canvas, bounds)
	}

	// 绘制文字（白色 + 阴影效果）
	di.drawLabel(canvas, bounds)

	return nil
}

// drawIcon 绘制图标图像
func (di *DraggableIcon) drawIcon(canvas *walk.Canvas, bounds walk.Rectangle) {
	if di.iconImg == nil {
		return
	}

	// 将 image.Image 转为 walk.Bitmap
	rgbaImg, ok := di.iconImg.(*image.RGBA)
	if !ok {
		// 转换为 RGBA
		b := di.iconImg.Bounds()
		rgbaImg = image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				rgbaImg.Set(x, y, di.iconImg.At(x, y))
			}
		}
	}

	bmp, err := walk.NewBitmapFromImage(rgbaImg)
	if err != nil {
		return
	}
	defer bmp.Dispose()

	// 居中绘制图标
	iconX := (bounds.Width - desktopIconSize) / 2
	iconY := desktopIconTop

	canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{
		X:      bounds.X + iconX,
		Y:      bounds.Y + iconY,
		Width:  desktopIconSize,
		Height: desktopIconSize,
	}, 255)
}

// drawLabel 绘制文字标签
func (di *DraggableIcon) drawLabel(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 创建字体（微软雅黑，不加粗，类似系统桌面）
	font, err := walk.NewFont("Microsoft YaHei UI", desktopIconTextSize, 0)
	if err != nil {
		font, _ = walk.NewFont("Microsoft YaHei", desktopIconTextSize, 0)
	}
	if font == nil {
		return
	}
	defer font.Dispose()

	text := di.displayName
	// 最多显示2行，自动截断
	lines := splitTextToLines(text, 8) // 约8个字一行

	textColor := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	shadowColor := color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xCC}

	for i, line := range lines {
		if i >= 2 {
			break
		}
		if i == 1 && len(lines) > 2 {
			line = TruncateText(line, 7)
		}

		y := bounds.Y + desktopIconLabelTop + i*desktopIconLineHeight
		textBounds := walk.Rectangle{
			X:      bounds.X,
			Y:      y,
			Width:  bounds.Width,
			Height: desktopIconLineHeight,
		}

		// 绘制阴影（偏移1像素）
		shadowBounds := textBounds
		shadowBounds.X++
		shadowBounds.Y++

		format := walk.TextCenter | walk.TextSingleLine
		canvas.DrawTextPixels(line, font, walk.RGB(shadowColor.R, shadowColor.G, shadowColor.B), shadowBounds, format)
		canvas.DrawTextPixels(line, font, walk.RGB(textColor.R, textColor.G, textColor.B), textBounds, format)
	}
}

// Widget 返回底层 walk 控件
func (di *DraggableIcon) Widget() *walk.CustomWidget {
	return di.widget
}

// FilePath 返回文件路径
func (di *DraggableIcon) FilePath() string {
	return di.filePath
}

// SetOnDoubleClick 设置双击回调
func (di *DraggableIcon) SetOnDoubleClick(fn func()) {
	di.onDoubleClick = fn
}

// SetOnDragStart 设置拖拽开始回调
func (di *DraggableIcon) SetOnDragStart(fn func(filePath string)) {
	di.onDragStart = fn
}

// SetOnDragEnd 设置拖拽结束回调
func (di *DraggableIcon) SetOnDragEnd(fn func(filePath string, x, y int)) {
	di.onDragEnd = fn
}

// HandleDoubleClick 处理双击
func (di *DraggableIcon) HandleDoubleClick() {
	if di.onDoubleClick != nil {
		di.onDoubleClick()
	}
}

// getDisplayName 从文件路径获取显示名称
func getDisplayName(filePath string) string {
	name := filepath.Base(filePath)
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

// splitTextToLines 将文本按指定宽度拆分为多行
func splitTextToLines(text string, maxRunesPerLine int) []string {
	runes := []rune(text)
	if utf8.RuneCountInString(text) <= maxRunesPerLine {
		return []string{text}
	}

	var lines []string
	for len(runes) > 0 {
		end := maxRunesPerLine
		if end > len(runes) {
			end = len(runes)
		}
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
	}
	return lines
}

// CreateCursorFromImage 创建自定义光标（24x24）
func CreateCursorFromImage(img image.Image) win.HCURSOR {
	// 使用默认系统光标作为后备
	return 0
}
