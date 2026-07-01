package ui

import (
	"image"
	"image/color"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
)

// 卡片最小尺寸
const (
	cardMinWidth     = 220
	cardMinHeight    = 160
	cardHeaderHeight = 30
	resizeHandleSize = 8

	actionBtnWidth  = 22
	actionBtnHeight = 20
	actionBtnGap    = 2
	doubleClickMs   = 500 // 双击判定时间（毫秒）
)

// GroupCard 分组卡片组件
type GroupCard struct {
	container  *walk.Composite
	headerBar  *walk.Composite
	bodyWidget *walk.CustomWidget
	groupName  string
	groupColor color.RGBA
	position   config.Position // 相对坐标 (0~1)
	size       config.Size     // 相对尺寸 (0~1)
	items      []group.GroupItem
	icons      []*DraggableIcon
	manager    *group.Manager
	executor   *ProgramExecutor
	owner      walk.Form

	// 工作区尺寸（像素），用于坐标转换
	workW int
	workH int

	// 拖拽状态
	isDragging    bool
	isPressed     bool // 鼠标是否按下（用于长按检测）
	dragStartX    int
	dragStartY    int
	dragStartTime time.Time
	dragScreenX   int // 按下时鼠标的屏幕绝对坐标
	dragScreenY   int
	dragCardX     int // 按下时卡片的屏幕绝对坐标
	dragCardY     int

	// 缩放状态
	isResizing   bool
	resizeEdge   ResizeEdge
	resizeStartX int
	resizeStartY int
	resizeStartW int
	resizeStartH int

	// 回调
	onPositionChanged func(name string, x, y float64)
	onSizeChanged     func(name string, w, h float64)
	onRename          func(name string)
	onColor           func(name string)
	onDelete          func(name string)
	onRefresh         func()

	// 双击检测状态
	lastClickTime time.Time
	lastClickIdx  int // 上次点击的图标索引
}

// ResizeEdge 缩放方向
type ResizeEdge int

const (
	ResizeNone ResizeEdge = iota
	ResizeLeft
	ResizeRight
	ResizeTop
	ResizeBottom
	ResizeTopLeft
	ResizeTopRight
	ResizeBottomLeft
	ResizeBottomRight
)

// NewGroupCard 创建分组卡片
func NewGroupCard(parent walk.Container, grp config.Group, mgr *group.Manager, executor *ProgramExecutor, owner walk.Form, workW, workH int) (*GroupCard, error) {
	gc := &GroupCard{
		groupName:  grp.Name,
		groupColor: ParseHexColor(grp.Color),
		position:   grp.Position,
		size:       grp.Size,
		manager:    mgr,
		executor:   executor,
		owner:      owner,
		workW:      workW,
		workH:      workH,
	}

	var err error
	gc.container, err = walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}

	// 不设置 Layout（保持 nil），让此容器不被父 VBox 布局管理
	// walk 的 createLayoutItemForWidgetWithContext 会跳过 Layout==nil 的 Container

	// 将相对坐标转换为像素坐标
	pixelX := int(grp.Position.X * float64(workW))
	pixelY := int(grp.Position.Y * float64(workH))
	pixelW := int(grp.Size.Width * float64(workW))
	pixelH := int(grp.Size.Height * float64(workH))

	gc.container.SetBoundsPixels(walk.Rectangle{
		X:      pixelX,
		Y:      pixelY,
		Width:  pixelW,
		Height: pixelH,
	})

	parentHwnd := win.GetParent(gc.container.Handle())
	logger.Debug("NewGroupCard: %q pos=(%d,%d) size=(%dx%d) containerHwnd=%v parentHwnd=%v",
		grp.Name, pixelX, pixelY, pixelW, pixelH, gc.container.Handle(), parentHwnd)

	// 使用自定义绘制作为背景
	gc.bodyWidget, err = walk.NewCustomWidgetPixels(gc.container, 0, gc.paintBody)
	if err != nil {
		return nil, err
	}
	gc.bodyWidget.SetPaintMode(walk.PaintBuffered)
	gc.bodyWidget.SetInvalidatesOnResize(true)

	// 手动设置 bodyWidget 填满 container（因为没有 Layout 自动管理）
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0,
		Width: pixelW, Height: pixelH,
	})

	logger.Debug("NewGroupCard: %q bodyWidget hwnd=%v, bounds=(0,0,%dx%d)",
		grp.Name, gc.bodyWidget.Handle(), pixelW, pixelH)

	// 鼠标事件用于拖拽和缩放
	gc.setupMouseEvents()

	// 加载分组项目
	gc.refreshItems()

	return gc, nil
}

// setupMouseEvents 设置鼠标事件
func (gc *GroupCard) setupMouseEvents() {
	gc.bodyWidget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}

		// 记录按下状态（用于长按检测）
		gc.isPressed = true

		// 缩放边缘检测
		edge := gc.getResizeEdge(x, y)
		if edge != ResizeNone {
			gc.startResize(x, y, edge)
			return
		}

		if y < cardHeaderHeight {
			// 检查是否点击了操作按钮
			if btn := gc.getActionButtonAt(x); btn != "" {
				switch btn {
				case "rename":
					if gc.onRename != nil {
						gc.onRename(gc.groupName)
					}
				case "color":
					if gc.onColor != nil {
						gc.onColor(gc.groupName)
					}
				case "delete":
					if gc.onDelete != nil {
						gc.onDelete(gc.groupName)
					}
				}
				return
			}

			// 标题栏空白处长按3秒拖拽
			// 记录按下时的屏幕绝对坐标和卡片位置，用于拖拽时精确跟随鼠标
			var screenPt win.POINT
			screenPt.X = int32(x)
			screenPt.Y = int32(y)
			win.ClientToScreen(gc.bodyWidget.Handle(), &screenPt)
			gc.dragScreenX = int(screenPt.X)
			gc.dragScreenY = int(screenPt.Y)
			gc.dragCardX = gc.pixelX()
			gc.dragCardY = gc.pixelY()
			gc.dragStartTime = time.Now()
			gc.isDragging = false
			go gc.checkDragStart()
			return
		}

		// 图标区域：检测双击
		idx := gc.getItemIndexAt(x, y)
		if idx >= 0 && idx < len(gc.items) {
			now := time.Now()
			if idx == gc.lastClickIdx && now.Sub(gc.lastClickTime) < doubleClickMs*time.Millisecond {
				// 双击：执行程序
				gc.executor.Execute(gc.items[idx].Path)
				gc.lastClickTime = time.Time{} // 重置，防止三击
				return
			}
			gc.lastClickTime = now
			gc.lastClickIdx = idx
		}
	})

	gc.bodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		isDragEnd := gc.isDragging
		gc.isPressed = false
		if gc.isResizing {
			gc.endResize()
		}
		gc.isDragging = false
		if isDragEnd {
			// 拖拽结束后触发重绘，清除残留痕迹
			gc.bodyWidget.Invalidate()
		}
	})

	gc.bodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		if gc.isResizing {
			gc.handleResize(x, y)
		} else if gc.isDragging {
			gc.handleDrag(x, y)
		} else {
			gc.updateCursor(x, y)
		}
	})
}

// checkDragStart 检查是否开始拖拽（长按3秒）
func (gc *GroupCard) checkDragStart() {
	time.Sleep(longPressDragDelay)
	if gc.isPressed {
		gc.isDragging = true
	}
}

// getResizeEdge 获取鼠标位置对应的缩放方向
func (gc *GroupCard) getResizeEdge(x, y int) ResizeEdge {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	w, h := bounds.Width, bounds.Height

	onLeft := x < resizeHandleSize
	onRight := x > w-resizeHandleSize
	onTop := y < resizeHandleSize
	onBottom := y > h-resizeHandleSize

	if onTop && onLeft {
		return ResizeTopLeft
	}
	if onTop && onRight {
		return ResizeTopRight
	}
	if onBottom && onLeft {
		return ResizeBottomLeft
	}
	if onBottom && onRight {
		return ResizeBottomRight
	}
	if onLeft {
		return ResizeLeft
	}
	if onRight {
		return ResizeRight
	}
	if onTop {
		return ResizeTop
	}
	if onBottom {
		return ResizeBottom
	}
	return ResizeNone
}

// startResize 开始缩放
func (gc *GroupCard) startResize(x, y int, edge ResizeEdge) {
	gc.isResizing = true
	gc.resizeEdge = edge
	gc.resizeStartX = x
	gc.resizeStartY = y
	gc.resizeStartW = int(gc.size.Width * float64(gc.workW))
	gc.resizeStartH = int(gc.size.Height * float64(gc.workH))
}

// handleResize 处理缩放
func (gc *GroupCard) handleResize(x, y int) {
	dx := x - gc.resizeStartX
	dy := y - gc.resizeStartY

	// 当前像素值
	curPixelX := int(gc.position.X * float64(gc.workW))
	curPixelY := int(gc.position.Y * float64(gc.workH))

	newW := gc.resizeStartW
	newH := gc.resizeStartH
	newX := curPixelX
	newY := curPixelY

	switch gc.resizeEdge {
	case ResizeRight:
		newW += dx
	case ResizeBottom:
		newH += dy
	case ResizeLeft:
		newW -= dx
		newX += dx
	case ResizeTop:
		newH -= dy
		newY += dy
	case ResizeBottomRight:
		newW += dx
		newH += dy
	case ResizeTopLeft:
		newW -= dx
		newH -= dy
		newX += dx
		newY += dy
	case ResizeTopRight:
		newW += dx
		newH -= dy
		newY += dy
	case ResizeBottomLeft:
		newW -= dx
		newH += dy
		newX += dx
	}

	// 限制最小尺寸
	newW = ClampInt(newW, cardMinWidth, 2000)
	newH = ClampInt(newH, cardMinHeight, 2000)

	// 转回相对坐标
	gc.size.Width = float64(newW) / float64(gc.workW)
	gc.size.Height = float64(newH) / float64(gc.workH)
	gc.position.X = float64(newX) / float64(gc.workW)
	gc.position.Y = float64(newY) / float64(gc.workH)

	gc.container.SetBoundsPixels(walk.Rectangle{
		X: newX, Y: newY, Width: newW, Height: newH,
	})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0, Width: newW, Height: newH,
	})

	if gc.onSizeChanged != nil {
		gc.onSizeChanged(gc.groupName, gc.size.Width, gc.size.Height)
	}
	if gc.onPositionChanged != nil {
		gc.onPositionChanged(gc.groupName, gc.position.X, gc.position.Y)
	}
}

// endResize 结束缩放
func (gc *GroupCard) endResize() {
	gc.isResizing = false
	gc.manager.UpdateGroupSize(gc.groupName, gc.size.Width, gc.size.Height)
	gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)
}

// pixelX 获取当前像素X坐标
func (gc *GroupCard) pixelX() int {
	return int(gc.position.X * float64(gc.workW))
}

// pixelY 获取当前像素Y坐标
func (gc *GroupCard) pixelY() int {
	return int(gc.position.Y * float64(gc.workH))
}

// pixelW 获取当前像素宽度
func (gc *GroupCard) pixelW() int {
	return int(gc.size.Width * float64(gc.workW))
}

// pixelH 获取当前像素高度
func (gc *GroupCard) pixelH() int {
	return int(gc.size.Height * float64(gc.workH))
}

// handleDrag 处理拖拽
func (gc *GroupCard) handleDrag(x, y int) {
	if !gc.isDragging {
		return
	}

	// 将当前鼠标位置转为屏幕绝对坐标，计算与按下时的偏移
	var screenPt win.POINT
	screenPt.X = int32(x)
	screenPt.Y = int32(y)
	win.ClientToScreen(gc.bodyWidget.Handle(), &screenPt)
	dx := int(screenPt.X) - gc.dragScreenX
	dy := int(screenPt.Y) - gc.dragScreenY

	if dx == 0 && dy == 0 {
		return
	}

	// 根据屏幕偏移量计算卡片新位置
	newCardX := gc.dragCardX + dx
	newCardY := gc.dragCardY + dy

	gc.position.X = float64(newCardX) / float64(gc.workW)
	gc.position.Y = float64(newCardY) / float64(gc.workH)

	pixW := gc.pixelW()
	pixH := gc.pixelH()

	// bodyWidget 是 container 的子控件，会跟随自动移动
	gc.container.SetBoundsPixels(walk.Rectangle{
		X: newCardX, Y: newCardY,
		Width: pixW, Height: pixH,
	})

	gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)
}

// updateCursor 根据位置更新光标
func (gc *GroupCard) updateCursor(x, y int) {
	edge := gc.getResizeEdge(x, y)
	switch edge {
	case ResizeLeft, ResizeRight:
		gc.bodyWidget.SetCursor(walk.CursorSizeWE())
	case ResizeTop, ResizeBottom:
		gc.bodyWidget.SetCursor(walk.CursorSizeNS())
	case ResizeTopLeft, ResizeBottomRight:
		gc.bodyWidget.SetCursor(walk.CursorSizeNWSE())
	case ResizeTopRight, ResizeBottomLeft:
		gc.bodyWidget.SetCursor(walk.CursorSizeNESW())
	default:
		gc.bodyWidget.SetCursor(walk.CursorArrow())
	}
}

// getActionButtonAt 根据 x 坐标判断点击了哪个操作按钮
func (gc *GroupCard) getActionButtonAt(x int) string {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	// 按钮从右到左排列：[×] [色] [✎]
	btnRight := bounds.X + bounds.Width - 4
	btnLeft := btnRight - actionBtnWidth

	// × 删除（最右）
	if x > btnLeft && x < btnRight {
		return "delete"
	}
	btnRight = btnLeft - actionBtnGap
	btnLeft = btnRight - actionBtnWidth
	// 色 颜色（中间）
	if x > btnLeft && x < btnRight {
		return "color"
	}
	btnRight = btnLeft - actionBtnGap
	btnLeft = btnRight - actionBtnWidth
	// ✎ 重命名（最左）
	if x > btnLeft && x < btnRight {
		return "rename"
	}
	return ""
}

// getItemIndexAt 获取指定像素位置对应的图标索引
func (gc *GroupCard) getItemIndexAt(x, y int) int {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	startY := bounds.Y + cardHeaderHeight + 4
	startX := bounds.X + 4
	colWidth := desktopIconItemWidth + 8 + 8
	if colWidth <= 0 {
		return -1
	}
	maxCols := (bounds.Width - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}

	// 计算点击的行列
	col := (x - startX) / colWidth
	row := (y - startY) / desktopIconItemHeight

	if col < 0 || col >= maxCols || row < 0 {
		return -1
	}

	idx := row*maxCols + col
	if idx >= len(gc.items) {
		return -1
	}

	// 精确校验：确保点击在图标磁贴范围内
	tileX := startX + col*colWidth
	tileY := startY + row*desktopIconItemHeight
	if x < tileX || x > tileX+colWidth || y < tileY || y > tileY+desktopIconItemHeight {
		return -1
	}

	return idx
}

// paintBody 绘制卡片主体
func (gc *GroupCard) paintBody(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := gc.bodyWidget.ClientBoundsPixels()

	// 绘制半透明背景
	gc.paintBackground(canvas, bounds)

	// 绘制标题栏
	gc.paintHeader(canvas, bounds)

	// 绘制图标网格
	gc.paintIconGrid(canvas, bounds)

	return nil
}

// paintBackground 绘制卡片背景（半透明颜色）
func (gc *GroupCard) paintBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	bgBmp := gc.createColorBitmap(bounds.Width, bounds.Height, gc.groupColor)
	if bgBmp != nil {
		defer bgBmp.Dispose()
		canvas.DrawBitmapWithOpacityPixels(bgBmp, bounds, gc.groupColor.A)
	}
}

// paintHeader 绘制标题栏（含操作按钮）
func (gc *GroupCard) paintHeader(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 标题文字（预留按钮空间）
	btnAreaW := (actionBtnWidth+actionBtnGap)*3 + 4
	titleFont := GetCardTitleFont()
	if titleFont != nil {
		defer titleFont.Dispose()
		headerBounds := walk.Rectangle{
			X: bounds.X + 8, Y: bounds.Y + 4,
			Width: bounds.Width - 16 - btnAreaW, Height: cardHeaderHeight,
		}
		canvas.DrawTextPixels(gc.groupName, titleFont, walk.RGB(0xFF, 0xFF, 0xFF),
			headerBounds, walk.TextSingleLine|walk.TextVCenter)
	}
	// 操作按钮字体
	btnFont, _ := walk.NewFont("Microsoft YaHei", 11, walk.FontBold)
	if btnFont != nil {
		defer btnFont.Dispose()
		btnY := bounds.Y + (cardHeaderHeight-actionBtnHeight)/2
		btnRight := bounds.X + bounds.Width - 4

		type btnDef struct {
			label string
			x     int
		}
		btns := []btnDef{
			{"×", btnRight - actionBtnWidth},                                       // 删除
			{"色", btnRight - (actionBtnWidth+actionBtnGap)*2 + actionBtnGap},      // 颜色
			{"✎", btnRight - (actionBtnWidth+actionBtnGap)*3 + actionBtnGap*2},     // 重命名
		}

		for _, b := range btns {
			// 按钮背景（半透明）
			btnRect := walk.Rectangle{
				X: b.x, Y: btnY,
				Width: actionBtnWidth, Height: actionBtnHeight,
			}
			bgImg := image.NewRGBA(image.Rect(0, 0, actionBtnWidth, actionBtnHeight))
			for py := 0; py < actionBtnHeight; py++ {
				for px := 0; px < actionBtnWidth; px++ {
					bgImg.SetRGBA(px, py, color.RGBA{0, 0, 0, 80})
				}
			}
			if bgBmp, err := walk.NewBitmapFromImage(bgImg); err == nil {
				canvas.DrawBitmapWithOpacityPixels(bgBmp, btnRect, 80)
				bgBmp.Dispose()
			}
			// 按钮文字
			canvas.DrawTextPixels(b.label, btnFont, walk.RGB(0xFF, 0xFF, 0xFF),
				btnRect, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
		}
	}

	// 绘制分隔线
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0xFF, 0xFF, 0xFF))
	if pen != nil {
		defer pen.Dispose()
		y := bounds.Y + cardHeaderHeight
		canvas.DrawLinePixels(pen, walk.Point{X: bounds.X + 4, Y: y}, walk.Point{X: bounds.X + bounds.Width - 4, Y: y})
	}
}

// paintIconGrid 绘制图标网格
func (gc *GroupCard) paintIconGrid(canvas *walk.Canvas, bounds walk.Rectangle) {
	startY := bounds.Y + cardHeaderHeight + 4
	startX := bounds.X + 4
	colWidth := desktopIconItemWidth + 8 + 8
	if colWidth <= 0 {
		return
	}
	maxCols := (bounds.Width - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}

	for i, item := range gc.items {
		col := i % maxCols
		row := i / maxCols

		x := startX + col*colWidth
		y := startY + row*desktopIconItemHeight

		if y+desktopIconItemHeight > bounds.Y+bounds.Height {
			break
		}

		gc.paintIconTile(canvas, item, x, y)
	}
}

// paintIconTile 绘制单个图标磁贴
func (gc *GroupCard) paintIconTile(canvas *walk.Canvas, item group.GroupItem, x, y int) {
	// 首次绘制时用 canvas 精确测量磁贴尺寸
	ensureTileSizeMeasured(canvas)

	// 获取图标
	extractor := NewIconExtractor()
	iconImg, _ := extractor.GetIconImage(item.Path)

	if iconImg != nil {
		rgbaImg, ok := iconImg.(*image.RGBA)
		if !ok {
			b := iconImg.Bounds()
			rgbaImg = image.NewRGBA(b)
			for iy := b.Min.Y; iy < b.Max.Y; iy++ {
				for ix := b.Min.X; ix < b.Max.X; ix++ {
					rgbaImg.Set(ix, iy, iconImg.At(ix, iy))
				}
			}
		}

		bmp, err := walk.NewBitmapFromImage(rgbaImg)
		if err == nil {
			defer bmp.Dispose()
			iconX := x + (desktopIconItemWidth-desktopIconSize)/2
			iconY := y + desktopIconTop
			canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{
				X: iconX, Y: iconY, Width: desktopIconSize, Height: desktopIconSize,
			}, 255)
		}
	}

	// 绘制名称（宋体，不加粗，自动换行）
	font := GetIconFont()
	if font != nil {
		defer font.Dispose()
		displayName := item.Name
		lines := splitTextToLines(displayName, 4)

		labelTop := y + desktopIconLabelTop

		for i, line := range lines {
			if i >= 2 {
				break
			}
			if i == 1 && len(lines) > 2 {
				line = TruncateText(line, 3)
			}

			lineY := labelTop + i*desktopIconLineHeight
			textBounds := walk.Rectangle{
				X: x, Y: lineY,
				Width: desktopIconItemWidth, Height: desktopIconLineHeight,
			}

			shadowBounds := textBounds
			shadowBounds.X++
			shadowBounds.Y++
			canvas.DrawTextPixels(line, font, walk.RGB(0, 0, 0), shadowBounds, walk.TextCenter|walk.TextSingleLine)
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
	}
}

// createColorBitmap 创建纯色位图
func (gc *GroupCard) createColorBitmap(w, h int, c color.RGBA) *walk.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return nil
	}
	return bmp
}

// refreshItems 刷新分组项目
func (gc *GroupCard) refreshItems() {
	gc.items = gc.manager.GetGroupItems(gc.groupName)
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

// Container 返回卡片容器
func (gc *GroupCard) Container() *walk.Composite {
	return gc.container
}

// SetOnPositionChanged 设置位置变更回调
func (gc *GroupCard) SetOnPositionChanged(fn func(name string, x, y float64)) {
	gc.onPositionChanged = fn
}

// SetOnSizeChanged 设置尺寸变更回调
func (gc *GroupCard) SetOnSizeChanged(fn func(name string, w, h float64)) {
	gc.onSizeChanged = fn
}

// SetOnRename 设置重命名回调
func (gc *GroupCard) SetOnRename(fn func(name string)) {
	gc.onRename = fn
}

// SetOnColor 设置修改颜色回调
func (gc *GroupCard) SetOnColor(fn func(name string)) {
	gc.onColor = fn
}

// SetOnDelete 设置删除回调
func (gc *GroupCard) SetOnDelete(fn func(name string)) {
	gc.onDelete = fn
}

// Refresh 刷新卡片内容
func (gc *GroupCard) Refresh() {
	gc.refreshItems()
}

// SetPosition 设置位置（相对坐标）
func (gc *GroupCard) SetPosition(x, y float64) {
	gc.position = config.Position{X: x, Y: y}
	w, h := gc.pixelW(), gc.pixelH()
	gc.container.SetBoundsPixels(walk.Rectangle{
		X: gc.pixelX(), Y: gc.pixelY(), Width: w, Height: h,
	})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0, Width: w, Height: h,
	})
}

// SetSize 设置尺寸（相对坐标）
func (gc *GroupCard) SetSize(w, h float64) {
	gc.size = config.Size{Width: w, Height: h}
	pw, ph := gc.pixelW(), gc.pixelH()
	gc.container.SetBoundsPixels(walk.Rectangle{
		X: gc.pixelX(), Y: gc.pixelY(), Width: pw, Height: ph,
	})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0, Width: pw, Height: ph,
	})
}

// ReapplyBounds 重新应用位置和尺寸（用于布局变更后恢复绝对定位）
func (gc *GroupCard) ReapplyBounds() {
	w := gc.pixelW()
	h := gc.pixelH()
	x := gc.pixelX()
	y := gc.pixelY()
	gc.container.SetBoundsPixels(walk.Rectangle{
		X: x, Y: y,
		Width: w, Height: h,
	})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0,
		Width: w, Height: h,
	})
	// 强制触发重绘，确保卡片内容可见
	gc.bodyWidget.Invalidate()

	// 验证实际 bounds
	actualContainer := gc.container.BoundsPixels()
	actualBody := gc.bodyWidget.BoundsPixels()
	logger.Debug("ReapplyBounds: %q container=(%d,%d,%dx%d) body=(%d,%d,%dx%d) visible=%v",
		gc.groupName,
		actualContainer.X, actualContainer.Y, actualContainer.Width, actualContainer.Height,
		actualBody.X, actualBody.Y, actualBody.Width, actualBody.Height,
		gc.container.Visible())
}
