package ui

import (
	"image"
	"image/color"
	"image/draw"
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

	actionBtnWidth   = 22
	actionBtnHeight  = 20
	actionBtnGap     = 2
	doubleClickMs    = 500 // 双击判定时间（毫秒）
	CardHeaderHeight = 30  // 导出的标题栏高度常量（供 desktop 包使用）
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
	manager    *group.Manager
	executor   *ProgramExecutor
	owner      walk.Form

	// 工作区尺寸（像素），用于坐标转换
	workW int
	workH int

	// 悬停与选中状态
	hoveredItemIdx  int // 当前悬停的图标索引，-1 表示无
	selectedItemIdx int // 当前选中的图标索引，-1 表示无

	// 双击检测
	lastClickTime time.Time
	lastClickIdx  int

	// 缓存的卡片背景 Bitmap（纯色半透明），卡片尺寸或颜色变化时重建
	bgCacheBmp *walk.Bitmap
	bgCacheW   int
	bgCacheH   int

	// 拖拽状态（卡片整体拖拽）
	isDragging    bool
	isPressed     bool // 鼠标是否按下（用于长按检测）
	dragStartX    int
	dragStartY    int
	dragStartTime time.Time
	dragScreenX   int // 按下时鼠标的屏幕绝对坐标
	dragScreenY   int
	dragCardX     int // 按下时卡片的屏幕绝对坐标
	dragCardY     int
	dragNewX      int // 拖拽中新位置（不移动容器，给 DesktopMode 画虚框用）
	dragNewY      int

	// 缩放状态（拖拽中只画边框，结束时才应用）
	isResizing   bool
	resizeEdge   ResizeEdge
	resizeStartX int
	resizeStartY int
	resizeStartW int
	resizeStartH int
	resizeNewX   int // 拖拽中计算的新的左上角 X（结束时应用）
	resizeNewY   int // 拖拽中计算的新的左上角 Y
	resizeNewW   int // 拖拽中计算的新宽度
	resizeNewH   int // 拖拽中计算的新高度

	// 回调
	onPositionChanged func(name string, x, y float64)
	onSizeChanged     func(name string, w, h float64)
	onRename          func(name string)
	onColor           func(name string)
	onDelete          func(name string)

	// 拖放目标指示
	isDropTarget bool

	// 卡片主体点击回调（通知 DesktopMode 清除选中）
	onCardBodyClick func()

	// 图标左键单击回调（通知 DesktopMode 选中项目）
	onIconLeftClick func(card *GroupCard, idx int, item group.GroupItem)

	// 图标右键回调（DesktopMode 设置，显示 Shell 扩展菜单）
	onIconRightClick func(card *GroupCard, idx int, item group.GroupItem, screenX, screenY int)

	// 图标按下回调（通知 DesktopMode，由 UnifiedDragState 统一管理拖拽延迟检测和状态）
	onIconPress func(card *GroupCard, idx int, item group.GroupItem, clientX, clientY int)

	// 图标释放回调（通知 DesktopMode 取消拖拽，防止点击变拖拽）
	onIconRelease func()

	// 卡片拖拽虚框位置更新回调（DesktopMode 在桌面层画虚框）
	onCardDragOutline    func(card *GroupCard, newX, newY int)
	onCardDragOutlineEnd func(card *GroupCard)
	// 缩放虚框回调（DesktopMode 在桌面层画边框）
	onResizeOutline    func(card *GroupCard, newX, newY, newW, newH int)
	onResizeOutlineEnd func(card *GroupCard)
	// 移动/缩放后全局刷新回调（通知 DesktopMode 刷新所有卡片和桌面）
	onRefreshAfterMove func()
	// 获取桌面壁纸位图（工作区尺寸），卡片背景从真实壁纸合成
	onGetWallpaper func() *walk.Bitmap
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
		groupName:       grp.Name,
		groupColor:      ParseHexColor(grp.Color),
		position:        grp.Position,
		size:            grp.Size,
		manager:         mgr,
		executor:        executor,
		owner:           owner,
		workW:           workW,
		workH:           workH,
		hoveredItemIdx:  -1,
		selectedItemIdx: -1,
		lastClickIdx:    -1,
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
	gc.bodyWidget.SetPaintMode(walk.PaintNoErase)
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

// setupMouseEvents 设置鼠标事件（全部走 walk bodyWidget 事件）
func (gc *GroupCard) setupMouseEvents() {
	gc.bodyWidget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.RightButton {
			idx := gc.getItemIndexAt(x, y)
			if idx >= 0 && idx < len(gc.items) && gc.onIconRightClick != nil {
				var screenPt win.POINT
				screenPt.X = int32(x)
				screenPt.Y = int32(y)
				win.ClientToScreen(gc.bodyWidget.Handle(), &screenPt)
				gc.onIconRightClick(gc, idx, gc.items[idx], int(screenPt.X), int(screenPt.Y))
			}
			return
		}
		if button != walk.LeftButton {
			return
		}

		// 记录按下状态（用于长按检测，卡片拖拽用）
		gc.isPressed = true

		// 缩放边缘检测
		edge := gc.getResizeEdge(x, y)
		if edge != ResizeNone {
			gc.startResize(x, y, edge)
			return
		}

		if y < cardHeaderHeight {
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
			// 标题栏长按拖拽
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

		// 图标区域：双击检测 + 通知 DesktopMode 处理拖拽
		idx := gc.getItemIndexAt(x, y)
		if idx >= 0 && idx < len(gc.items) {
			if gc.lastClickIdx == idx && !gc.lastClickTime.IsZero() &&
				time.Since(gc.lastClickTime) < doubleClickMs*time.Millisecond {
				// 双击→执行程序
				logger.Debug("GroupCard.MouseDown: DOUBLE-CLICK detected, path=%q", gc.items[idx].Path)
				gc.executor.Execute(gc.items[idx].Path)
				gc.lastClickTime = time.Time{}
				logger.Debug("GroupCard.MouseDown: Execute returned, returning")
				return
			}
			// 单击→选中
			if gc.onIconLeftClick != nil {
				gc.onIconLeftClick(gc, idx, gc.items[idx])
			}
			gc.lastClickTime = time.Now()
			gc.lastClickIdx = idx

			// 通知 DesktopMode 图标按下（由 UnifiedDragState 统一管理拖拽延迟检测）
			if gc.onIconPress != nil {
				gc.onIconPress(gc, idx, gc.items[idx], x, y)
			}
			return
		}

		// 空白区域→清除选中
		if gc.onCardBodyClick != nil {
			gc.onCardBodyClick()
		}
	})

	gc.bodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}

		// 通知 DesktopMode 取消图标拖拽（避免点击变拖拽，因为 card MouseUp 不会传播到 desktop BodyWidget）
		if gc.onIconRelease != nil {
			gc.onIconRelease()
		}

		isDragEnd := gc.isDragging
		gc.isPressed = false
		if gc.isResizing {
			gc.endResize()
		}
		gc.isDragging = false
		if isDragEnd {
			// 通知 DesktopMode 清除虚框
			if gc.onCardDragOutlineEnd != nil {
				gc.onCardDragOutlineEnd(gc)
			}
			// 应用拖拽新位置到容器
			gc.position.X = float64(gc.dragNewX) / float64(gc.workW)
			gc.position.Y = float64(gc.dragNewY) / float64(gc.workH)
			pixW := gc.pixelW()
			pixH := gc.pixelH()
			gc.applyBounds(gc.dragNewX, gc.dragNewY, pixW, pixH, true)
			gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)
		}
		// 通知 DesktopMode 重绘桌面 BodyWidget，清除卡片原位置残留
		if gc.onCardDragOutlineEnd != nil {
			gc.onCardDragOutlineEnd(gc)
		}
	})

	gc.bodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		if gc.isResizing {
			gc.handleResize(x, y)
		} else if gc.isDragging {
			gc.handleDrag(x, y)
		} else {
			gc.updateCursor(x, y)
			idx := gc.getItemIndexAt(x, y)
			if idx >= 0 && idx < len(gc.items) {
				if idx != gc.hoveredItemIdx {
					gc.hoveredItemIdx = idx
					gc.bodyWidget.Invalidate()
				}
			} else if gc.hoveredItemIdx != -1 {
				gc.hoveredItemIdx = -1
				gc.bodyWidget.Invalidate()
			}
		}
	})
}

// handleCardClick 处理卡片内左键点击（图标选中/双击 + 空白清除）
// checkDragStart 检查是否开始拖拽（长按3秒）
func (gc *GroupCard) checkDragStart() {
	time.Sleep(LongPressDragDelay)
	if gc.isPressed {
		gc.isDragging = true
		// 初始化虚框位置为当前位置（防止未移动直接释放时位置为0）
		gc.dragNewX = gc.pixelX()
		gc.dragNewY = gc.pixelY()
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
	// 初始化新值为当前值（防止未移动直接释放时位置为0）
	gc.resizeNewX = int(gc.position.X * float64(gc.workW))
	gc.resizeNewY = int(gc.position.Y * float64(gc.workH))
	gc.resizeNewW = gc.resizeStartW
	gc.resizeNewH = gc.resizeStartH

	// 捕获鼠标，确保拖出 bodyWidget 后仍能收到 MouseMove/MouseUp 事件
	win.SetCapture(gc.bodyWidget.Handle())
}

// handleResize 处理缩放（仅更新边框位置，不实际改变容器/body尺寸）
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

	// 仅保存计算结果（不改变容器/body尺寸），通知 DesktopMode 绘制缩放边框
	gc.resizeNewX = newX
	gc.resizeNewY = newY
	gc.resizeNewW = newW
	gc.resizeNewH = newH

	if gc.onResizeOutline != nil {
		gc.onResizeOutline(gc, newX, newY, newW, newH)
	}
}

// endResize 结束缩放：实际应用容器/body尺寸
func (gc *GroupCard) endResize() {
	gc.isResizing = false

	// 释放鼠标捕获
	win.ReleaseCapture()

	// 清除 DesktopMode 上的缩放边框
	if gc.onResizeOutlineEnd != nil {
		gc.onResizeOutlineEnd(gc)
	}

	// 将拖拽中累积的新尺寸应用到实际容器
	gc.size.Width = float64(gc.resizeNewW) / float64(gc.workW)
	gc.size.Height = float64(gc.resizeNewH) / float64(gc.workH)
	gc.position.X = float64(gc.resizeNewX) / float64(gc.workW)
	gc.position.Y = float64(gc.resizeNewY) / float64(gc.workH)

	gc.applyBounds(gc.resizeNewX, gc.resizeNewY, gc.resizeNewW, gc.resizeNewH, true)

	gc.manager.UpdateGroupSize(gc.groupName, gc.size.Width, gc.size.Height)
	gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)

	if gc.onSizeChanged != nil {
		gc.onSizeChanged(gc.groupName, gc.size.Width, gc.size.Height)
	}
	if gc.onPositionChanged != nil {
		gc.onPositionChanged(gc.groupName, gc.position.X, gc.position.Y)
	}

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

// handleDrag 处理拖拽（不移动容器，只更新虚框位置）
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

	// 计算新位置但不移动容器
	gc.dragNewX = gc.dragCardX + dx
	gc.dragNewY = gc.dragCardY + dy

	if gc.onCardDragOutline != nil {
		gc.onCardDragOutline(gc, gc.dragNewX, gc.dragNewY)
	}
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

// ScreenBounds 返回卡片容器的屏幕坐标矩形
func (gc *GroupCard) ScreenBounds() walk.Rectangle {
	var rect win.RECT
	win.GetWindowRect(gc.container.Handle(), &rect)
	return walk.Rectangle{
		X:      int(rect.Left),
		Y:      int(rect.Top),
		Width:  int(rect.Right - rect.Left),
		Height: int(rect.Bottom - rect.Top),
	}
}

// paintBody 绘制卡片主体
func (gc *GroupCard) paintBody(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := gc.bodyWidget.ClientBoundsPixels()

	// 拖拽中只画虚框，不画实际内容
	if gc.isDragging {
		gc.paintDragOutline(canvas, bounds)
		return nil
	}

	// 绘制半透明背景
	gc.paintBackground(canvas, bounds)

	// 绘制标题栏
	gc.paintHeader(canvas, bounds)

	// 绘制图标网格
	gc.paintIconGrid(canvas, bounds)

	// 绘制拖放目标高亮边框
	if gc.isDropTarget {
		pen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x4A, 0xA0, 0xFF))
		if err == nil {
			defer pen.Dispose()
			// 2px 宽边框
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: bounds.Width, Y: 0})
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: bounds.Height - 1}, walk.Point{X: bounds.Width, Y: bounds.Height - 1})
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: 0, Y: bounds.Height})
			canvas.DrawLinePixels(pen, walk.Point{X: bounds.Width - 1, Y: 0}, walk.Point{X: bounds.Width - 1, Y: bounds.Height})
		}
	}

	return nil
}

// paintDragOutline 绘制卡片拖拽虚框（拖动时不渲染实际内容）
func (gc *GroupCard) paintDragOutline(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 2px 白色虚线边框
	pen, err := walk.NewCosmeticPen(walk.PenDash, walk.RGB(0xFF, 0xFF, 0xFF))
	if err != nil {
		return
	}
	defer pen.Dispose()

	canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: bounds.Width, Y: 0})
	canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: bounds.Height - 1}, walk.Point{X: bounds.Width, Y: bounds.Height - 1})
	canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: 0, Y: bounds.Height})
	canvas.DrawLinePixels(pen, walk.Point{X: bounds.Width - 1, Y: 0}, walk.Point{X: bounds.Width - 1, Y: bounds.Height})
}

// paintBackground 绘制卡片背景（半透明颜色）
// 使用 PaintNoErase 模式。先从缓存的桌面壁纸位图中取卡片当前位置的区域
// 作为底，再 AlphaBlend 半透明色——这样每帧都从干净壁纸开始，不会累积。
func (gc *GroupCard) paintBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	// 1) 从桌面壁纸位图取卡片当前位置的壁纸区域作为底
	if gc.onGetWallpaper != nil {
		if wp := gc.onGetWallpaper(); wp != nil {
			src := walk.Rectangle{
				X:      gc.pixelX(),
				Y:      gc.pixelY(),
				Width:  bounds.Width,
				Height: bounds.Height,
			}
			// 绘制壁纸底；如果失败（如尺寸不匹配），canvas 保持原样（屏幕壁纸）
			_ = canvas.DrawBitmapPartWithOpacityPixels(wp, bounds, src, 255)
		}
	}

	// 2) 半透明颜色叠加
	if gc.bgCacheBmp != nil && gc.bgCacheW == bounds.Width && gc.bgCacheH == bounds.Height {
		canvas.DrawBitmapWithOpacityPixels(gc.bgCacheBmp, bounds, gc.groupColor.A)
		return
	}
	if gc.bgCacheBmp != nil {
		gc.bgCacheBmp.Dispose()
		gc.bgCacheBmp = nil
	}
	bgImg := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	draw.Draw(bgImg, bgImg.Bounds(), &image.Uniform{gc.groupColor}, image.Point{}, draw.Src)
	bmp, err := walk.NewBitmapFromImage(bgImg)
	if err != nil {
		return
	}
	gc.bgCacheBmp = bmp
	gc.bgCacheW = bounds.Width
	gc.bgCacheH = bounds.Height
	canvas.DrawBitmapWithOpacityPixels(bmp, bounds, gc.groupColor.A)
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
			{"×", btnRight - actionBtnWidth},                                   // 删除
			{"色", btnRight - (actionBtnWidth+actionBtnGap)*2 + actionBtnGap},   // 颜色
			{"✎", btnRight - (actionBtnWidth+actionBtnGap)*3 + actionBtnGap*2}, // 重命名
		}

		for _, b := range btns {
			// 按钮背景（半透明）
			btnRect := walk.Rectangle{
				X: b.x, Y: btnY,
				Width: actionBtnWidth, Height: actionBtnHeight,
			}
			if btnBrush, err := walk.NewSolidColorBrush(walk.RGB(0, 0, 0)); err == nil {
				canvas.FillRectanglePixels(btnBrush, btnRect)
				btnBrush.Dispose()
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

// paintIconGrid 绘制卡片内图标网格（从左到右，从上到下）
func (gc *GroupCard) paintIconGrid(canvas *walk.Canvas, bounds walk.Rectangle) {
	startY := bounds.Y + cardHeaderHeight + 4
	startX := bounds.X + 4
	colWidth := TileColWidth()
	if colWidth <= 0 {
		return
	}
	maxCols := (bounds.Width - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}
	logger.Debug("paintIconGrid: card=%q items=%d bounds=(%d,%d,%dx%d) startY=%d colWidth=%d maxCols=%d tileH=%d",
		gc.groupName, len(gc.items), bounds.X, bounds.Y, bounds.Width, bounds.Height,
		startY, colWidth, maxCols, desktopIconItemHeight)

	for i, item := range gc.items {
		col := i % maxCols
		row := i / maxCols

		x := startX + col*colWidth
		y := startY + row*desktopIconItemHeight

		if y+desktopIconItemHeight > bounds.Y+bounds.Height {
			logger.Debug("paintIconGrid: card=%q BREAK at item[%d] name=%q (y=%d + tileH=%d > bottom=%d)",
				gc.groupName, i, item.Name, y, desktopIconItemHeight, bounds.Y+bounds.Height)
			break
		}

		hovered := i == gc.hoveredItemIdx
		selected := i == gc.selectedItemIdx
		gc.paintIconTile(canvas, item, x, y, hovered, selected)
	}
}

// paintIconTile 绘制单个图标磁贴（使用缓存 bitmap）
func (gc *GroupCard) paintIconTile(canvas *walk.Canvas, item group.GroupItem, x, y int, hovered, selected bool) {
	EnsureTileSizeMeasured(canvas)

	// 计算选中时的扩展高度（包含所有文字行）
	lines := SplitTextToLines(item.Name, 4)
	selH := desktopIconItemHeight
	if selected {
		selH = DesktopIconLabelTop() + len(lines)*DesktopIconLineHeight() + 4
	}

	// 绘制选中/悬停高亮
	if selected {
		DrawSelectionRect(canvas, walk.Rectangle{
			X: x, Y: y,
			Width: desktopIconItemWidth, Height: selH,
		})
	} else if hovered {
		DrawHoverRect(canvas, walk.Rectangle{
			X: x, Y: y,
			Width: desktopIconItemWidth, Height: desktopIconItemHeight,
		})
	}

	bmp := GlobalIconBmpCache.GetOrLoad(item.Path)
	if bmp != nil {
		iconX := x + (desktopIconItemWidth-DesktopIconSize())/2
		iconY := y + DesktopIconTop()
		canvas.DrawBitmapWithOpacityPixels(bmp, walk.Rectangle{
			X: iconX, Y: iconY, Width: DesktopIconSize(), Height: DesktopIconSize(),
		}, 255)
	} else {
		logger.Warn("paintIconTile: bmp is NIL for path=%q name=%q card=%q (iconSize=%d tileW=%d)",
			item.Path, item.Name, gc.groupName, DesktopIconSize(), desktopIconItemWidth)
	}

	// 绘制名称
	font := GetIconFont()
	if font != nil {
		defer font.Dispose()
		labelTop := y + DesktopIconLabelTop()
		// 4 方向阴影（上下左右），保证白字在浅色卡片背景上也清晰可读
		// 之前用单方向 (x+1,y+1) 偏移太弱，在浅紫/浅蓝卡片背景上白字几乎看不清
		shadowOffsets := [4]struct{ dx, dy int }{
			{0, -1}, {-1, 0}, {1, 0}, {0, 1},
		}
		drawLabel := func(line string, lineY int) {
			textBounds := walk.Rectangle{X: x, Y: lineY, Width: desktopIconItemWidth, Height: DesktopIconLineHeight()}
			for _, off := range shadowOffsets {
				shadowBounds := walk.Rectangle{
					X:      textBounds.X + off.dx,
					Y:      textBounds.Y + off.dy,
					Width:  textBounds.Width,
					Height: textBounds.Height,
				}
				canvas.DrawTextPixels(line, font, walk.RGB(0, 0, 0), shadowBounds, walk.TextCenter|walk.TextSingleLine)
			}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
		if selected {
			// 选中状态：显示所有行，不加省略号
			for i, line := range lines {
				lineY := labelTop + i*DesktopIconLineHeight()
				drawLabel(line, lineY)
			}
		} else {
			// 非选中：最多显示2行，超出省略
			displayLines := GetIconDisplayLines(item.Name, 4)
			for i, line := range displayLines {
				lineY := labelTop + i*DesktopIconLineHeight()
				drawLabel(line, lineY)
			}
		}
	}
}

// getItemIndexAt 获取指定像素位置对应的图标索引（与 paintIconGrid 使用相同的布局算法）
func (gc *GroupCard) getItemIndexAt(x, y int) int {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	startY := bounds.Y + cardHeaderHeight + 4
	startX := bounds.X + 4
	colWidth := TileColWidth()
	if colWidth <= 0 {
		return -1
	}
	maxCols := (bounds.Width - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}

	for i := range gc.items {
		col := i % maxCols
		row := i / maxCols
		tileX := startX + col*colWidth
		tileY := startY + row*desktopIconItemHeight
		if x >= tileX && x <= tileX+colWidth &&
			y >= tileY && y <= tileY+desktopIconItemHeight {
			return i
		}
	}
	return -1
}

// getIconTileBounds 获取指定索引图标在 bodyWidget 中的左上角像素位置
func (gc *GroupCard) getIconTileBounds(idx int) (x, y int) {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	startY := cardHeaderHeight + 4
	startX := 4
	colWidth := TileColWidth()
	if colWidth <= 0 {
		return 0, 0
	}
	maxCols := (bounds.Width - 8) / colWidth
	if maxCols < 1 {
		maxCols = 1
	}
	col := idx % maxCols
	row := idx / maxCols
	return startX + col*colWidth, startY + row*desktopIconItemHeight
}

// isCardItemInLabelArea 判断点击是否在卡片内图标磁贴的标签区域
func (gc *GroupCard) isCardItemInLabelArea(y int, idx int) bool {
	_, tileY := gc.getIconTileBounds(idx)
	labelStart := tileY + DesktopIconLabelTop()
	labelEnd := labelStart + 2*DesktopIconLineHeight()
	return y >= labelStart && y < labelEnd
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

// GroupColor 返回卡片颜色
func (gc *GroupCard) GroupColor() color.RGBA {
	return gc.groupColor
}

// SetGroupColor 直接更新卡片颜色并重绘（避免销毁重建）
func (gc *GroupCard) SetGroupColor(colorStr string) {
	gc.groupColor = ParseHexColor(colorStr)
	// 颜色变了，必须清除背景缓存，否则 paintBackground 会复用旧颜色的缓存位图
	gc.clearBgCache()
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

// clearBgCache 释放缓存的背景位图
func (gc *GroupCard) clearBgCache() {
	if gc.bgCacheBmp != nil {
		gc.bgCacheBmp.Dispose()
		gc.bgCacheBmp = nil
	}
}

// SetOnDelete 设置删除回调
func (gc *GroupCard) SetOnDelete(fn func(name string)) {
	gc.onDelete = fn
}

// refreshItems 从 Manager 加载分组项目并预加载图标缓存
func (gc *GroupCard) refreshItems() {
	gc.items = gc.manager.GetGroupItems(gc.groupName)
	gc.rebuildIconCache()
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

// rebuildIconCache 预加载当前分组所有图标到全局缓存
func (gc *GroupCard) rebuildIconCache() {
	paths := make([]string, len(gc.items))
	for i, item := range gc.items {
		paths[i] = item.Path
	}
	GlobalIconBmpCache.LoadAll(paths)
}

// Refresh 刷新卡片内容
func (gc *GroupCard) Refresh() {
	gc.refreshItems()
}

// Items 返回分组中的所有项目
func (gc *GroupCard) Items() []group.GroupItem { return gc.items }

// SetOnIconLeftClick 设置图标左键单击回调（通知 DesktopMode 选中）
func (gc *GroupCard) SetOnIconLeftClick(fn func(card *GroupCard, idx int, item group.GroupItem)) {
	gc.onIconLeftClick = fn
}

// SetOnIconRightClick 设置图标右键点击回调
func (gc *GroupCard) SetOnIconRightClick(fn func(card *GroupCard, idx int, item group.GroupItem, screenX, screenY int)) {
	gc.onIconRightClick = fn
}

// applyBounds 统一应用容器与 body 的像素 bounds。
// 关键：先产生一次真实的尺寸变化（宽度 +1 再移回），强制触发 walk 的
// WM_WINDOWPOSCHANGED → FormBase.startLayout 用新的客户区大小重新布局，
// 否则 PaintNoErase 模式下 bodyWidget 的 canvas DC 原点不会更新，
// AlphaBlend 会叠加到旧屏幕位置的壁纸上（表现为"卡片显示了非当前区域的桌面背景"）。
// 注意：SetWindowPos/SetBoundsPixels 若带 SWP_NOSIZE 会被 walk 跳过布局，
// 所以这里必须让尺寸真正变化一次。
// triggerRefresh: 是否触发 onRefreshAfterMove 回调（true=用户拖拽/缩放后，false=布局恢复时）
func (gc *GroupCard) applyBounds(x, y, w, h int, triggerRefresh ...bool) {
	// 1) 真实尺寸变化触发重新布局
	gc.container.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w + 1, Height: h + 1})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: w + 1, Height: h + 1})
	// 2) 移回目标尺寸（这次 WM_WINDOWPOSCHANGED 带 SWP_NOSIZE，但第 1 步已让布局刷新）
	gc.container.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w, Height: h})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: w, Height: h})
	// 位置/尺寸变了，背景缓存不再适用，清除后下次重绘用新位置重建
	gc.clearBgCache()
	gc.bodyWidget.Invalidate()
	// 默认触发刷新（用户拖拽/缩放后），布局恢复时 caller 传 false
	if len(triggerRefresh) == 0 || triggerRefresh[0] {
		if gc.onRefreshAfterMove != nil {
			gc.onRefreshAfterMove()
		}
	}
}

// SetPosition 设置位置（相对坐标）
func (gc *GroupCard) SetPosition(x, y float64) {
	gc.position = config.Position{X: x, Y: y}
	w, h := gc.pixelW(), gc.pixelH()
	gc.applyBounds(gc.pixelX(), gc.pixelY(), w, h, true)
}

// SetSize 设置尺寸（相对坐标）
func (gc *GroupCard) SetSize(w, h float64) {
	gc.size = config.Size{Width: w, Height: h}
	pw, ph := gc.pixelW(), gc.pixelH()
	gc.applyBounds(gc.pixelX(), gc.pixelY(), pw, ph, true)
}

// ReapplyBounds 重新应用位置和尺寸（用于布局变更后恢复绝对定位）
func (gc *GroupCard) ReapplyBounds() {
	w := gc.pixelW()
	h := gc.pixelH()
	x := gc.pixelX()
	y := gc.pixelY()
	gc.applyBounds(x, y, w, h, false)

	// 验证实际 bounds
	// actualContainer := gc.container.BoundsPixels()
	// actualBody := gc.bodyWidget.BoundsPixels()
	// logger.Debug("ReapplyBounds: %q container=(%d,%d,%dx%d) body=(%d,%d,%dx%d) visible=%v",
	// 	gc.groupName,
	// 	actualContainer.X, actualContainer.Y, actualContainer.Width, actualContainer.Height,
	// 	actualBody.X, actualBody.Y, actualBody.Width, actualBody.Height,
	// 	gc.container.Visible())
}

// SetIsDropTarget 设置是否为当前拖放目标（绘制高亮边框）
func (gc *GroupCard) SetIsDropTarget(v bool) {
	if gc.isDropTarget != v {
		gc.isDropTarget = v
		gc.bodyWidget.Invalidate()
	}
}

// Cleanup 清理卡片资源
func (gc *GroupCard) Cleanup() {
}

// SelectItem 选中卡片内指定索引的图标
func (gc *GroupCard) SelectItem(idx int) {
	if gc.selectedItemIdx != idx {
		gc.selectedItemIdx = idx
		gc.bodyWidget.Invalidate()
	}
}

// ClearSelection 清除卡片内图标选中
func (gc *GroupCard) ClearSelection() {
	if gc.selectedItemIdx != -1 {
		gc.selectedItemIdx = -1
		gc.bodyWidget.Invalidate()
	}
}

// SetOnCardBodyClick 设置卡片主体点击回调（通知 DesktopMode 清除选中）
func (gc *GroupCard) SetOnCardBodyClick(fn func()) {
	gc.onCardBodyClick = fn
}

// SetOnCardDragOutline 设置卡片拖拽虚框回调
func (gc *GroupCard) SetOnCardDragOutline(fn func(card *GroupCard, newX, newY int)) {
	gc.onCardDragOutline = fn
}

// SetOnCardDragOutlineEnd 设置卡片拖拽结束回调
func (gc *GroupCard) SetOnCardDragOutlineEnd(fn func(card *GroupCard)) {
	gc.onCardDragOutlineEnd = fn
}

// SetOnIconPress 设置图标按下回调（通知 DesktopMode 统一管理拖拽）
func (gc *GroupCard) SetOnIconPress(fn func(card *GroupCard, idx int, item group.GroupItem, clientX, clientY int)) {
	gc.onIconPress = fn
}

// SetOnIconRelease 设置图标释放回调（通知 DesktopMode 取消拖拽）
func (gc *GroupCard) SetOnIconRelease(fn func()) {
	gc.onIconRelease = fn
}

// SetOnResizeOutline 设置缩放虚框回调（DesktopMode 在桌面层绘制边框）
func (gc *GroupCard) SetOnResizeOutline(fn func(card *GroupCard, newX, newY, newW, newH int)) {
	gc.onResizeOutline = fn
}

// SetOnResizeOutlineEnd 设置缩放虚框结束回调
func (gc *GroupCard) SetOnResizeOutlineEnd(fn func(card *GroupCard)) {
	gc.onResizeOutlineEnd = fn
}

// SetOnRefreshAfterMove 设置移动/缩放后全局刷新回调
func (gc *GroupCard) SetOnRefreshAfterMove(fn func()) {
	gc.onRefreshAfterMove = fn
}

// SetOnGetWallpaper 设置获取桌面壁纸位图的回调
func (gc *GroupCard) SetOnGetWallpaper(fn func() *walk.Bitmap) {
	gc.onGetWallpaper = fn
}

// GroupName 返回分组名称
func (gc *GroupCard) GroupName() string { return gc.groupName }

// BodyWidgetHandle 返回卡片 bodyWidget 的窗口句柄
func (gc *GroupCard) BodyWidgetHandle() win.HWND {
	return gc.bodyWidget.Handle()
}

// HitTestIcon 命中检测指定坐标处是否有图标，返回图标索引（-1 表示无）
func (gc *GroupCard) HitTestIcon(x, y int) int {
	return gc.getItemIndexAt(x, y)
}

// PixelW 返回像素宽度
func (gc *GroupCard) PixelW() int { return gc.pixelW() }

// PixelH 返回像素高度
func (gc *GroupCard) PixelH() int { return gc.pixelH() }
