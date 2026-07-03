package ui

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// 图标磁贴规格常量
const (
	desktopIconSize       = 48
	desktopIconTop        = 2
	desktopIconLabelTop   = 52
	desktopIconLineHeight = 24
	desktopIconGap        = 8  // 图标磁贴间距
	longPressDragDelay    = 3 * time.Second  // 卡片拖拽延迟（标题栏长按）
	iconDragDelay         = 1 * time.Second  // 图标拖拽延迟（卡片内/未分组图标）
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
	// 首次绘制时用 canvas 精确测量磁贴尺寸
	ensureTileSizeMeasured(canvas)

	// 同步 widget 尺寸（确保与动态计算的磁贴尺寸一致）
	di.widget.SetMinMaxSizePixels(
		walk.Size{Width: desktopIconItemWidth, Height: desktopIconItemHeight},
		walk.Size{Width: desktopIconItemWidth, Height: desktopIconItemHeight},
	)

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
	font := GetIconFont()
	if font == nil {
		return
	}
	defer font.Dispose()

	text := di.displayName
	lines := splitTextToLines(text, 4)

	textColor := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	shadowColor := color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xCC}

	labelTop := bounds.Y + desktopIconLabelTop

	for i, line := range lines {
		if i >= 2 {
			break
		}
		if i == 1 && len(lines) > 2 {
			line = TruncateText(line, 3)
		}

		y := labelTop + i*desktopIconLineHeight
		textBounds := walk.Rectangle{
			X:      bounds.X,
			Y:      y,
			Width:  desktopIconItemWidth,
			Height: desktopIconLineHeight,
		}

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

// splitTextToLines 将文本拆分为多行，优先在空格处换行，最大 maxRunes 个汉字/行
func splitTextToLines(text string, maxCJK int) []string {
	maxWidth := maxCJK * 2 // 全角2单位，半角1单位，最大宽度8（4中文/8英文）
	runes := []rune(text)

	var lines []string
	pos := 0
	for pos < len(runes) {
		width := 0
		end := pos
		lastSpace := -1

		for end < len(runes) {
			r := runes[end]
			w := 2               // 默认全角
			if r <= 0xFF {       // ASCII 及半角符号
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

		if end >= len(runes) {
			lines = append(lines, string(runes[pos:]))
			break
		}

		if lastSpace >= pos {
			lines = append(lines, string(runes[pos:lastSpace]))
			pos = lastSpace + 1 // 跳过分隔空格
		} else {
			lines = append(lines, string(runes[pos:end]))
			pos = end
		}
	}
	return lines
}

// CreateCursorFromImage 创建自定义光标（24x24）
func CreateCursorFromImage(img image.Image) win.HCURSOR {
	// 使用默认系统光标作为后备
	return 0
}
