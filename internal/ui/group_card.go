package ui

import (
	"image"
	"image/color"
	"image/draw"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/safego"
)

const (
	cardMinWidth     = 220
	cardMinHeight    = 160
	cardHeaderHeight = 30
	resizeHandleSize = 8

	actionBtnWidth   = 22
	actionBtnHeight  = 20
	actionBtnGap     = 2
	doubleClickMs    = 500
	CardHeaderHeight = 30
)

// Selection 全局选中/悬停状态：定位到具体图标。
// Path 当前路径；Card 所在卡片名（空字符串表示未分组）。
type Selection struct {
	Path string
	Card string
}

// SelectionProvider 提供全局 hover/选中 状态（由桌面层注入）。
// 未分组与分组图标共用一套全局状态，GroupCard 通过该接口读写，
// 避免 ui 包与 desktop 包循环依赖。
type SelectionProvider interface {
	GetSelected() Selection
	SetSelected(sel Selection)
	GetHovered() Selection
	SetHovered(sel Selection)
	ClearSelection()
}

type GroupCard struct {
	container  *walk.Composite
	headerBar  *walk.Composite
	bodyWidget *walk.CustomWidget
	groupName  string
	groupColor color.RGBA
	position   config.Position
	size       config.Size
	isCollapsed bool
	items      []group.GroupItem
	manager    *group.Manager
	executor   *ProgramExecutor
	owner      walk.Form

	workW int
	workH int

	selection SelectionProvider

	lastClickTime time.Time
	lastClickIdx  int

	bgCacheBmp *walk.Bitmap
	bgCacheW   int
	bgCacheH   int

	isDragging    bool
	isPressed     bool
	dragStartX    int
	dragStartY    int
	dragStartTime time.Time
	dragScreenX   int
	dragScreenY   int
	dragCardX     int
	dragCardY     int
	dragNewX      int
	dragNewY      int

	isResizing   bool
	resizeEdge   ResizeEdge
	resizeStartX int
	resizeStartY int
	resizeStartW int
	resizeStartH int
	resizeNewX   int
	resizeNewY   int
	resizeNewW   int
	resizeNewH   int

	onPositionChanged func(name string, x, y float64)
	onSizeChanged     func(name string, w, h float64)
	onRename          func(name string)
	onColor           func(name string)
	onDelete          func(name string)
	onCollapseToggle  func(name string, collapsed bool)
	onCollapseStart   func(card *GroupCard)

	isDropTarget bool

	onCardBodyClick func()

	onCardClicked func(card *GroupCard)

	onIconLeftClick func(card *GroupCard, idx int, item group.GroupItem)

	onIconRightClick func(card *GroupCard, idx int, item group.GroupItem, screenX, screenY int)

	onIconPress func(card *GroupCard, idx int, item group.GroupItem, clientX, clientY int)

	onIconRelease func()

	onItemRename func(oldPath, newName string)

	editingPath string
	editHwnd    win.HWND

	onCardDragOutline    func(card *GroupCard, newX, newY int)
	onCardDragOutlineEnd func(card *GroupCard)
	onResizeOutline    func(card *GroupCard, newX, newY, newW, newH int)
	onResizeOutlineEnd func(card *GroupCard)
	onGetWallpaper func() *walk.Bitmap
}

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

func NewGroupCard(parent walk.Container, grp config.Group, mgr *group.Manager, executor *ProgramExecutor, owner walk.Form, workW, workH int) (*GroupCard, error) {
	gc := &GroupCard{
		groupName:       grp.Name,
		groupColor:      ParseHexColor(grp.Color),
		position:        grp.Position,
		size:            grp.Size,
		isCollapsed:     grp.Collapsed,
		manager:         mgr,
		executor:        executor,
		owner:           owner,
		workW:           workW,
		workH:           workH,
		lastClickIdx:    -1,
	}

	var err error
	gc.container, err = walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}

	pixelX := int(grp.Position.X * float64(workW))
	pixelY := int(grp.Position.Y * float64(workH))
	pixelW := int(grp.Size.Width * float64(workW))
	pixelH := int(grp.Size.Height * float64(workH))
	if gc.isCollapsed {
		pixelH = cardHeaderHeight + 4
	}

	gc.container.SetBoundsPixels(walk.Rectangle{
		X: pixelX, Y: pixelY,
		Width: pixelW, Height: pixelH,
	})

	parentHwnd := win.GetParent(gc.container.Handle())
	logger.Debug("NewGroupCard: %q pos=(%d,%d) size=(%dx%d) containerHwnd=%v parentHwnd=%v",
		grp.Name, pixelX, pixelY, pixelW, pixelH, gc.container.Handle(), parentHwnd)

	gc.bodyWidget, err = walk.NewCustomWidgetPixels(gc.container, 0, gc.paintBody)
	if err != nil {
		return nil, err
	}
	gc.bodyWidget.SetPaintMode(walk.PaintNoErase)
	gc.bodyWidget.SetInvalidatesOnResize(true)

	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{
		X: 0, Y: 0,
		Width: pixelW, Height: pixelH,
	})

	logger.Debug("NewGroupCard: %q bodyWidget hwnd=%v, bounds=(0,0,%dx%d)",
		grp.Name, gc.bodyWidget.Handle(), pixelW, pixelH)

	gc.setupMouseEvents()
	gc.refreshItems()

	return gc, nil
}

func (gc *GroupCard) setupMouseEvents() {
	gc.bodyWidget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if gc.editingPath != "" {
			gc.endCardItemEdit(true)
			if gc.editHwnd != 0 {
				var rect win.RECT
				win.GetWindowRect(gc.editHwnd, &rect)
				var pt win.POINT
				pt.X = rect.Left
				pt.Y = rect.Top
				win.ScreenToClient(gc.bodyWidget.Handle(), &pt)
				editX := int(pt.X)
				editY := int(pt.Y)
				editW := int(rect.Right - rect.Left)
				editH := int(rect.Bottom - rect.Top)
				if x >= editX && x <= editX+editW && y >= editY && y <= editY+editH {
					return
				}
			}
		}

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

		gc.isPressed = true

		if gc.onCardClicked != nil {
			gc.onCardClicked(gc)
		}

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
				case "collapse":
					gc.toggleCollapse()
				}
				return
			}
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
			safego.Go("checkDragStart", gc.checkDragStart)
			return
		}

		if gc.isCollapsed {
			if gc.onCardBodyClick != nil {
				gc.onCardBodyClick()
			}
			return
		}

		idx := gc.getItemIndexAt(x, y)
		if idx >= 0 && idx < len(gc.items) {
			if gc.selection != nil && gc.selection.GetSelected().Path == gc.items[idx].Path &&
				gc.isCardItemInLabelArea(y, idx) {
				gc.startCardItemEdit(idx)
				return
			}

			if gc.lastClickIdx == idx && !gc.lastClickTime.IsZero() &&
				time.Since(gc.lastClickTime) < doubleClickMs*time.Millisecond {
				logger.Debug("GroupCard.MouseDown: DOUBLE-CLICK detected, path=%q", gc.items[idx].Path)
				gc.executor.Execute(gc.items[idx].Path)
				gc.lastClickTime = time.Time{}
				logger.Debug("GroupCard.MouseDown: Execute returned, returning")
				return
			}
			if gc.onIconLeftClick != nil {
				gc.onIconLeftClick(gc, idx, gc.items[idx])
			}
			gc.lastClickTime = time.Now()
			gc.lastClickIdx = idx

			if gc.onIconPress != nil {
				gc.onIconPress(gc, idx, gc.items[idx], x, y)
			}
			return
		}

		if gc.onCardBodyClick != nil {
			gc.onCardBodyClick()
		}
	})

	gc.bodyWidget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}

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
			if gc.onCardDragOutlineEnd != nil {
				gc.onCardDragOutlineEnd(gc)
			}
			pixW := gc.pixelW()
			pixH := gc.pixelH()
			if gc.isCollapsed {
				pixH = cardHeaderHeight + 4
			}
			maxX := gc.workW - pixW
			if maxX < 0 {
				maxX = 0
			}
			maxY := gc.workH - pixH
			if maxY < 0 {
				maxY = 0
			}
			finalX := ClampInt(gc.dragNewX, 0, maxX)
			finalY := ClampInt(gc.dragNewY, 0, maxY)
			gc.position.X = float64(finalX) / float64(gc.workW)
			gc.position.Y = float64(finalY) / float64(gc.workH)
			gc.applyBounds(finalX, finalY, pixW, pixH)
			gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)
		}
	})

	gc.bodyWidget.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		if gc.isResizing {
			gc.handleResize(x, y)
		} else if gc.isDragging {
			gc.handleDrag(x, y)
		} else if gc.isCollapsed {
			gc.bodyWidget.SetCursor(walk.CursorArrow())
		} else {
			gc.updateCursor(x, y)
			idx := gc.getItemIndexAt(x, y)
			if gc.selection == nil {
				return
			}
			if idx >= 0 && idx < len(gc.items) {
				gc.selection.SetHovered(Selection{Path: gc.items[idx].Path, Card: gc.groupName})
			} else {
				gc.selection.SetHovered(Selection{})
			}
		}
	})
}

func (gc *GroupCard) checkDragStart() {
	time.Sleep(LongPressDragDelay)
	if gc.isPressed {
		gc.isDragging = true
		gc.dragNewX = gc.pixelX()
		gc.dragNewY = gc.pixelY()
	}
}

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

func (gc *GroupCard) startResize(x, y int, edge ResizeEdge) {
	gc.isResizing = true
	gc.resizeEdge = edge
	gc.resizeStartX = x
	gc.resizeStartY = y
	gc.resizeStartW = int(gc.size.Width * float64(gc.workW))
	gc.resizeStartH = int(gc.size.Height * float64(gc.workH))
	gc.resizeNewX = int(gc.position.X * float64(gc.workW))
	gc.resizeNewY = int(gc.position.Y * float64(gc.workH))
	gc.resizeNewW = gc.resizeStartW
	gc.resizeNewH = gc.resizeStartH

	win.SetCapture(gc.bodyWidget.Handle())
}

func (gc *GroupCard) handleResize(x, y int) {
	dx := x - gc.resizeStartX
	dy := y - gc.resizeStartY

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

	newW = ClampInt(newW, cardMinWidth, gc.workW)
	newH = ClampInt(newH, cardMinHeight, gc.workH)

	if newX+newW > gc.workW {
		if gc.resizeEdge == ResizeLeft || gc.resizeEdge == ResizeTopLeft || gc.resizeEdge == ResizeBottomLeft {
			newX = 0
			newW = gc.workW
		} else {
			newW = gc.workW - newX
		}
	}
	if newX < 0 {
		newW += newX
		newX = 0
		if newW < cardMinWidth {
			newW = cardMinWidth
		}
	}
	if newY+newH > gc.workH {
		if gc.resizeEdge == ResizeTop || gc.resizeEdge == ResizeTopLeft || gc.resizeEdge == ResizeTopRight {
			newY = 0
			newH = gc.workH
		} else {
			newH = gc.workH - newY
		}
	}
	if newY < 0 {
		newH += newY
		newY = 0
		if newH < cardMinHeight {
			newH = cardMinHeight
		}
	}
	newW = ClampInt(newW, cardMinWidth, gc.workW)
	newH = ClampInt(newH, cardMinHeight, gc.workH)

	gc.resizeNewX = newX
	gc.resizeNewY = newY
	gc.resizeNewW = newW
	gc.resizeNewH = newH

	if gc.onResizeOutline != nil {
		gc.onResizeOutline(gc, newX, newY, newW, newH)
	}
}

func (gc *GroupCard) endResize() {
	gc.isResizing = false

	win.ReleaseCapture()

	if gc.onResizeOutlineEnd != nil {
		gc.onResizeOutlineEnd(gc)
	}

	maxX := gc.workW - cardMinWidth
	if maxX < 0 {
		maxX = 0
	}
	maxY := gc.workH - cardMinHeight
	if maxY < 0 {
		maxY = 0
	}
	newX := ClampInt(gc.resizeNewX, 0, maxX)
	newY := ClampInt(gc.resizeNewY, 0, maxY)
	newW := ClampInt(gc.resizeNewW, cardMinWidth, gc.workW-newX)
	newH := ClampInt(gc.resizeNewH, cardMinHeight, gc.workH-newY)

	gc.size.Width = float64(newW) / float64(gc.workW)
	gc.size.Height = float64(newH) / float64(gc.workH)
	gc.position.X = float64(newX) / float64(gc.workW)
	gc.position.Y = float64(newY) / float64(gc.workH)

	gc.applyBounds(newX, newY, newW, newH)

	gc.manager.UpdateGroupSize(gc.groupName, gc.size.Width, gc.size.Height)
	gc.manager.UpdateGroupPosition(gc.groupName, gc.position.X, gc.position.Y)

	if gc.onSizeChanged != nil {
		gc.onSizeChanged(gc.groupName, gc.size.Width, gc.size.Height)
	}
	if gc.onPositionChanged != nil {
		gc.onPositionChanged(gc.groupName, gc.position.X, gc.position.Y)
	}
}

func (gc *GroupCard) pixelX() int {
	return int(gc.position.X * float64(gc.workW))
}

func (gc *GroupCard) pixelY() int {
	return int(gc.position.Y * float64(gc.workH))
}

func (gc *GroupCard) pixelW() int {
	return int(gc.size.Width * float64(gc.workW))
}

func (gc *GroupCard) pixelH() int {
	return int(gc.size.Height * float64(gc.workH))
}

func (gc *GroupCard) handleDrag(x, y int) {
	if !gc.isDragging {
		return
	}

	var screenPt win.POINT
	screenPt.X = int32(x)
	screenPt.Y = int32(y)
	win.ClientToScreen(gc.bodyWidget.Handle(), &screenPt)
	dx := int(screenPt.X) - gc.dragScreenX
	dy := int(screenPt.Y) - gc.dragScreenY

	if dx == 0 && dy == 0 {
		return
	}

	pixW := gc.pixelW()
	pixH := gc.pixelH()
	newX := ClampInt(gc.dragCardX+dx, 0, gc.workW-pixW)
	newY := ClampInt(gc.dragCardY+dy, 0, gc.workH-pixH)
	gc.dragNewX = newX
	gc.dragNewY = newY

	if gc.onCardDragOutline != nil {
		gc.onCardDragOutline(gc, gc.dragNewX, gc.dragNewY)
	}
}

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

func (gc *GroupCard) getActionButtonAt(x int) string {
	bounds := gc.bodyWidget.ClientBoundsPixels()
	btnRight := bounds.X + bounds.Width - 4
	btnLeft := btnRight - actionBtnWidth

	if x > btnLeft && x < btnRight {
		return "delete"
	}
	btnRight = btnLeft - actionBtnGap
	btnLeft = btnRight - actionBtnWidth
	if x > btnLeft && x < btnRight {
		return "color"
	}
	btnRight = btnLeft - actionBtnGap
	btnLeft = btnRight - actionBtnWidth
	if x > btnLeft && x < btnRight {
		return "rename"
	}
	btnRight = btnLeft - actionBtnGap
	btnLeft = btnRight - actionBtnWidth
	if x > btnLeft && x < btnRight {
		return "collapse"
	}
	return ""
}

func (gc *GroupCard) ScreenBounds() walk.Rectangle {
	var rect win.RECT
	win.GetWindowRect(gc.container.Handle(), &rect)
	return walk.Rectangle{
		X: int(rect.Left), Y: int(rect.Top),
		Width: int(rect.Right - rect.Left), Height: int(rect.Bottom - rect.Top),
	}
}

func (gc *GroupCard) paintBody(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	bounds := gc.bodyWidget.ClientBoundsPixels()

	if gc.isDragging {
		gc.paintDragOutline(canvas, bounds)
		return nil
	}

	gc.paintBackground(canvas, bounds)
	gc.paintHeader(canvas, bounds)

	if !gc.isCollapsed {
		gc.paintIconGrid(canvas, bounds)
	}

	if gc.isDropTarget {
		pen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0x4A, 0xA0, 0xFF))
		if err == nil {
			defer pen.Dispose()
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: bounds.Width, Y: 0})
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: bounds.Height - 1}, walk.Point{X: bounds.Width, Y: bounds.Height - 1})
			canvas.DrawLinePixels(pen, walk.Point{X: 0, Y: 0}, walk.Point{X: 0, Y: bounds.Height})
			canvas.DrawLinePixels(pen, walk.Point{X: bounds.Width - 1, Y: 0}, walk.Point{X: bounds.Width - 1, Y: bounds.Height})
		}
	}

	return nil
}

func (gc *GroupCard) paintDragOutline(canvas *walk.Canvas, bounds walk.Rectangle) {
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

func (gc *GroupCard) paintBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	if gc.onGetWallpaper != nil {
		if wp := gc.onGetWallpaper(); wp != nil {
			srcX := gc.pixelX()
			srcY := gc.pixelY()
			if srcX < 0 {
				srcX = 0
			}
			if srcY < 0 {
				srcY = 0
			}
			srcW := bounds.Width
			srcH := bounds.Height
			if srcX+srcW > gc.workW {
				srcW = gc.workW - srcX
			}
			if srcY+srcH > gc.workH {
				srcH = gc.workH - srcY
			}

			dstX := bounds.X + (gc.pixelX() - srcX)
			dstY := bounds.Y + (gc.pixelY() - srcY)
			if dstX < 0 {
				dstX = 0
			}
			if dstY < 0 {
				dstY = 0
			}
			dstW := srcW
			dstH := srcH
			if dstX+dstW > bounds.X+bounds.Width {
				dstW = bounds.X + bounds.Width - dstX
				if srcW > dstW {
					srcW = dstW
				}
			}
			if dstY+dstH > bounds.Y+bounds.Height {
				dstH = bounds.Y + bounds.Height - dstY
				if srcH > dstH {
					srcH = dstH
				}
			}

			if srcW > 0 && srcH > 0 {
				src := walk.Rectangle{X: srcX, Y: srcY, Width: srcW, Height: srcH}
				dst := walk.Rectangle{X: dstX, Y: dstY, Width: dstW, Height: dstH}
				_ = canvas.DrawBitmapPartWithOpacityPixels(wp, dst, src, 255)
			}
		}
	}

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

func (gc *GroupCard) paintHeader(canvas *walk.Canvas, bounds walk.Rectangle) {
	btnAreaW := (actionBtnWidth+actionBtnGap)*4 + 4
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
	btnFont, _ := walk.NewFont("Microsoft YaHei", 11, walk.FontBold)
	if btnFont != nil {
		defer btnFont.Dispose()
		btnY := bounds.Y + (cardHeaderHeight-actionBtnHeight)/2
		btnRight := bounds.X + bounds.Width - 4

		type btnDef struct {
			label string
			x     int
		}
		collapseLabel := "▼"
		if gc.isCollapsed {
			collapseLabel = "▲"
		}
		btns := []btnDef{
			{"×", btnRight - actionBtnWidth},
			{"色", btnRight - (actionBtnWidth+actionBtnGap)*2 + actionBtnGap},
			{"✎", btnRight - (actionBtnWidth+actionBtnGap)*3 + actionBtnGap*2},
			{collapseLabel, btnRight - (actionBtnWidth+actionBtnGap)*4 + actionBtnGap*3},
		}

		for _, b := range btns {
			btnRect := walk.Rectangle{
				X: b.x, Y: btnY,
				Width: actionBtnWidth, Height: actionBtnHeight,
			}
			if btnBrush, err := walk.NewSolidColorBrush(walk.RGB(0, 0, 0)); err == nil {
				canvas.FillRectanglePixels(btnBrush, btnRect)
				btnBrush.Dispose()
			}
			canvas.DrawTextPixels(b.label, btnFont, walk.RGB(0xFF, 0xFF, 0xFF),
				btnRect, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
		}
	}

	pen, _ := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(0xFF, 0xFF, 0xFF))
	if pen != nil {
		defer pen.Dispose()
		y := bounds.Y + cardHeaderHeight
		canvas.DrawLinePixels(pen, walk.Point{X: bounds.X + 4, Y: y}, walk.Point{X: bounds.X + bounds.Width - 4, Y: y})
	}
}

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

		var hovered, selected bool
		if gc.selection != nil {
			hovered = item.Path == gc.selection.GetHovered().Path
			selected = item.Path == gc.selection.GetSelected().Path
		}
		gc.paintIconTile(canvas, item, x, y, hovered, selected)
	}
}

func (gc *GroupCard) paintIconTile(canvas *walk.Canvas, item group.GroupItem, x, y int, hovered, selected bool) {
	EnsureTileSizeMeasured(canvas)

	lines := SplitTextToLines(item.Name, 4)
	selH := desktopIconItemHeight
	if selected {
		selH = DesktopIconLabelTop() + len(lines)*DesktopIconLineHeight() + 8
	} else if hovered {
		// 悬停只显示最多 2 行（GetIconDisplayLines），框高按实际显示行数，
		// 避免短名称（1 行）时磁贴底部留出整行空白
		hoverLines := GetIconDisplayLines(item.Name, 4)
		selH = DesktopIconLabelTop() + len(hoverLines)*DesktopIconLineHeight() + 8
	}

	if selected {
		DrawSelectionRect(canvas, walk.Rectangle{
			X: x, Y: y,
			Width: desktopIconItemWidth, Height: selH,
		})
	} else if hovered {
		DrawHoverRect(canvas, walk.Rectangle{
			X: x, Y: y,
			Width: desktopIconItemWidth, Height: selH,
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

	isEditing := gc.editingPath == item.Path
	if isEditing {
		return
	}
	font := GetIconFont()
	if font != nil {
		defer font.Dispose()
		labelTop := y + DesktopIconLabelTop()
		shadowOffsets := [4]struct{ dx, dy int }{
			{0, -1}, {-1, 0}, {1, 0}, {0, 1},
		}
		drawLabel := func(line string, lineY int) {
			textBounds := walk.Rectangle{X: x, Y: lineY, Width: desktopIconItemWidth, Height: DesktopIconLineHeight()}
			for _, off := range shadowOffsets {
				shadowBounds := walk.Rectangle{
					X: textBounds.X + off.dx, Y: textBounds.Y + off.dy,
					Width: textBounds.Width, Height: textBounds.Height,
				}
				canvas.DrawTextPixels(line, font, walk.RGB(0, 0, 0), shadowBounds, walk.TextCenter|walk.TextSingleLine)
			}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
		if selected {
			for i, line := range lines {
				lineY := labelTop + i*DesktopIconLineHeight()
				drawLabel(line, lineY)
			}
		} else {
			displayLines := GetIconDisplayLines(item.Name, 4)
			for i, line := range displayLines {
				lineY := labelTop + i*DesktopIconLineHeight()
				drawLabel(line, lineY)
			}
		}
	}
}

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

// invalidateTile 局部重绘指定索引的图标磁贴。
// 注意：卡片 bodyWidget 为 PaintNoErase 模式，无效矩形会裁剪 BeginPaint 区域，
// 因此无效高度按图标名最大行数（选中框需容纳全部文字）计算，确保选中/悬停框完整重绘。
func (gc *GroupCard) invalidateTile(idx int) {
	if idx < 0 || idx >= len(gc.items) {
		return
	}
	x, y := gc.getIconTileBounds(idx)
	lines := SplitTextToLines(gc.items[idx].Name, 4)
	tileH := DesktopIconLabelTop() + len(lines)*DesktopIconLineHeight() + 8
	if tileH < desktopIconItemHeight {
		tileH = desktopIconItemHeight
	}
	r := win.RECT{
		Left: int32(x), Top: int32(y),
		Right: int32(x + TileColWidth()), Bottom: int32(y + tileH),
	}
	win.InvalidateRect(gc.bodyWidget.Handle(), &r, false)
}

// InvalidateTileByPath 精准重绘指定 path 的图标磁贴（仅该卡片）。返回是否命中。
// 用于全局 hover/选中状态变化时，只重绘发生变化的图标，避免整卡重绘。
func (gc *GroupCard) InvalidateTileByPath(path string) bool {
	for i := range gc.items {
		if gc.items[i].Path == path {
			gc.invalidateTile(i)
			return true
		}
	}
	return false
}

func (gc *GroupCard) startCardItemEdit(idx int) {
	if idx < 0 || idx >= len(gc.items) {
		return
	}
	item := gc.items[idx]
	gc.editingPath = item.Path

	if gc.editHwnd != 0 {
		win.DestroyWindow(gc.editHwnd)
		gc.editHwnd = 0
	}

	gc.ClearSelection()

	tileX, tileY := gc.getIconTileBounds(idx)
	labelX := tileX
	labelY := tileY + DesktopIconLabelTop()
	labelW := TileWidth()
	labelH := 2 * DesktopIconLineHeight()

	hwnd := gc.bodyWidget.Handle()
	className := syscall.StringToUTF16Ptr("EDIT")
	windowText := syscall.StringToUTF16Ptr(item.Name)
	style := uintptr(win.WS_CHILD | win.WS_VISIBLE | win.ES_MULTILINE | win.ES_CENTER | win.ES_AUTOVSCROLL | win.WS_CLIPCHILDREN)
	editHwnd, _, _ := procCreateWindowExW.Call(
		uintptr(win.WS_EX_CLIENTEDGE),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowText)),
		style,
		uintptr(labelX), uintptr(labelY), uintptr(labelW), uintptr(labelH),
		uintptr(hwnd), 0, 0, 0)
	if editHwnd == 0 {
		logger.Error("startCardItemEdit: CreateWindowExW failed")
		gc.editingPath = ""
		return
	}

	editHWND := win.HWND(editHwnd)
	font := GetIconFont()
	if font != nil {
		hdc := win.GetDC(editHWND)
		if hdc != 0 {
			dpi := int(win.GetDeviceCaps(hdc, win.LOGPIXELSY))
			win.ReleaseDC(editHWND, hdc)
			if dpi <= 0 {
				dpi = 96
			}
			var lf win.LOGFONT
			lf.LfHeight = -win.MulDiv(int32(font.PointSize()), int32(dpi), 72)
			lf.LfWeight = win.FW_NORMAL
			lf.LfCharSet = win.DEFAULT_CHARSET
			lf.LfOutPrecision = win.OUT_TT_PRECIS
			lf.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
			lf.LfQuality = win.CLEARTYPE_QUALITY
			lf.LfPitchAndFamily = win.VARIABLE_PITCH | win.FF_SWISS
			family := syscall.StringToUTF16(font.Family())
			copy(lf.LfFaceName[:], family)
			hFont := win.CreateFontIndirect(&lf)
			if hFont != 0 {
				win.SendMessage(editHWND, win.WM_SETFONT, uintptr(hFont), 1)
			}
		}
		font.Dispose()
	}

	win.SendMessage(editHWND, win.EM_SETBKGNDCOLOR, 0, uintptr(win.RGB(0x30, 0x34, 0x3C)))
	win.SendMessage(editHWND, win.WM_USER+68, 0, uintptr(win.RGB(0xFF, 0xFF, 0xFF)))
	win.SendMessage(editHWND, win.EM_SETSEL, 0, ^uintptr(0))
	win.SetFocus(editHWND)
	gc.editHwnd = editHWND
	gc.setupCardItemEditSubclass(editHWND, item.Path)
}

func (gc *GroupCard) setupCardItemEditSubclass(editHwnd win.HWND, itemPath string) {
	editCB := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case win.WM_KILLFOCUS:
			gc.commitCardItemEditFromHwnd(win.HWND(hwnd), itemPath)
			win.DestroyWindow(win.HWND(hwnd))
			gc.editHwnd = 0
			gc.editingPath = ""
			gc.bodyWidget.Invalidate()
			return 0
		case win.WM_KEYDOWN:
			if wParam == win.VK_RETURN {
				gc.commitCardItemEditFromHwnd(win.HWND(hwnd), itemPath)
				win.DestroyWindow(win.HWND(hwnd))
				gc.editHwnd = 0
				gc.editingPath = ""
				gc.bodyWidget.Invalidate()
				return 0
			}
			if wParam == win.VK_ESCAPE {
				win.DestroyWindow(win.HWND(hwnd))
				gc.editHwnd = 0
				gc.editingPath = ""
				gc.bodyWidget.Invalidate()
				return 0
			}
		}
		ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})
	procSetWindowSubclass.Call(uintptr(editHwnd), editCB, 10001, 0)
}

func (gc *GroupCard) commitCardItemEditFromHwnd(editHwnd win.HWND, itemPath string) {
	textLen := win.SendMessage(editHwnd, win.WM_GETTEXTLENGTH, 0, 0)
	buf := make([]uint16, textLen+1)
	win.SendMessage(editHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
	newName := syscall.UTF16ToString(buf)
	if gc.onItemRename != nil {
		gc.onItemRename(itemPath, newName)
	}
}

func (gc *GroupCard) endCardItemEdit(save bool) {
	if gc.editingPath == "" {
		return
	}
	if gc.editHwnd != 0 {
		if save {
			textLen := win.SendMessage(gc.editHwnd, win.WM_GETTEXTLENGTH, 0, 0)
			buf := make([]uint16, textLen+1)
			win.SendMessage(gc.editHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
			newName := syscall.UTF16ToString(buf)
			if gc.onItemRename != nil {
				gc.onItemRename(gc.editingPath, newName)
			}
		}
		win.DestroyWindow(gc.editHwnd)
		gc.editHwnd = 0
	}
	gc.editingPath = ""
	gc.bodyWidget.Invalidate()
}

func (gc *GroupCard) IsEditing() bool {
	return gc.editingPath != ""
}

func (gc *GroupCard) SetOnItemRename(fn func(oldPath, newName string)) {
	gc.onItemRename = fn
}

func (gc *GroupCard) isCardItemInLabelArea(y int, idx int) bool {
	_, tileY := gc.getIconTileBounds(idx)
	labelStart := tileY + DesktopIconLabelTop()
	labelEnd := labelStart + 2*DesktopIconLineHeight()
	return y >= labelStart && y < labelEnd
}

func (gc *GroupCard) Container() *walk.Composite {
	return gc.container
}

func (gc *GroupCard) SetOnPositionChanged(fn func(name string, x, y float64)) {
	gc.onPositionChanged = fn
}

func (gc *GroupCard) SetOnSizeChanged(fn func(name string, w, h float64)) {
	gc.onSizeChanged = fn
}

func (gc *GroupCard) SetOnRename(fn func(name string)) {
	gc.onRename = fn
}

func (gc *GroupCard) SetOnColor(fn func(name string)) {
	gc.onColor = fn
}

func (gc *GroupCard) GroupColor() color.RGBA {
	return gc.groupColor
}

func (gc *GroupCard) SetGroupColor(colorStr string) {
	gc.groupColor = ParseHexColor(colorStr)
	gc.clearBgCache()
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

func (gc *GroupCard) clearBgCache() {
	if gc.bgCacheBmp != nil {
		gc.bgCacheBmp.Dispose()
		gc.bgCacheBmp = nil
	}
}

func (gc *GroupCard) SetOnDelete(fn func(name string)) {
	gc.onDelete = fn
}

func (gc *GroupCard) SetOnCollapseToggle(fn func(name string, collapsed bool)) {
	gc.onCollapseToggle = fn
}

func (gc *GroupCard) SetOnCollapseStart(fn func(card *GroupCard)) {
	gc.onCollapseStart = fn
}

func (gc *GroupCard) toggleCollapse() {
	gc.isCollapsed = !gc.isCollapsed
	if gc.isCollapsed {
		// 收缩前先收集有交集的卡片（此时仍是完整尺寸，用完整高度判断）
		if gc.onCollapseStart != nil {
			gc.onCollapseStart(gc)
		}
	}
	if !gc.isCollapsed {
		win.SetWindowPos(gc.container.Handle(), win.HWND_TOP, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	}
	gc.applyCollapsedBounds()
	if gc.onCollapseToggle != nil {
		gc.onCollapseToggle(gc.groupName, gc.isCollapsed)
	}
}

func (gc *GroupCard) applyCollapsedBounds() {
	x := gc.pixelX()
	y := gc.pixelY()
	w := gc.pixelW()
	if gc.isCollapsed {
		h := cardHeaderHeight + 4
		gc.applyBounds(x, y, w, h)
	} else {
		h := gc.pixelH()
		if h < cardMinHeight {
			h = cardMinHeight
		}
		gc.applyBounds(x, y, w, h)
	}
}

func (gc *GroupCard) refreshItems() {
	gc.items = gc.manager.GetGroupItems(gc.groupName)
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

func (gc *GroupCard) Refresh() {
	gc.refreshItems()
}

// Invalidate 触发卡片内容重绘（不重新拉取 items，用于全局 hover/选中状态变化后刷新）
func (gc *GroupCard) Invalidate() {
	if gc.bodyWidget != nil {
		gc.bodyWidget.Invalidate()
	}
}

func (gc *GroupCard) Items() []group.GroupItem { return gc.items }

func (gc *GroupCard) SetOnIconLeftClick(fn func(card *GroupCard, idx int, item group.GroupItem)) {
	gc.onIconLeftClick = fn
}

func (gc *GroupCard) SetOnIconRightClick(fn func(card *GroupCard, idx int, item group.GroupItem, screenX, screenY int)) {
	gc.onIconRightClick = fn
}

func (gc *GroupCard) applyBounds(x, y, w, h int) {
	hwndBody := gc.bodyWidget.Handle()
	hwndContainer := gc.container.Handle()
	win.SendMessage(hwndContainer, win.WM_SETREDRAW, 0, 0)
	win.SendMessage(hwndBody, win.WM_SETREDRAW, 0, 0)

	gc.container.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w + 1, Height: h + 1})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: w + 1, Height: h + 1})
	gc.container.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w, Height: h})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: w, Height: h})
	gc.clearBgCache()

	win.SendMessage(hwndContainer, win.WM_SETREDRAW, 1, 0)
	win.SendMessage(hwndBody, win.WM_SETREDRAW, 1, 0)
	win.InvalidateRect(hwndContainer, nil, false)
	win.InvalidateRect(hwndBody, nil, false)
	win.UpdateWindow(hwndBody)
	win.UpdateWindow(hwndContainer)
}

func (gc *GroupCard) SetPosition(x, y float64) {
	gc.position = config.Position{X: x, Y: y}
	w, h := gc.pixelW(), gc.pixelH()
	gc.applyBounds(gc.pixelX(), gc.pixelY(), w, h)
}

func (gc *GroupCard) SetSize(w, h float64) {
	gc.size = config.Size{Width: w, Height: h}
	pw, ph := gc.pixelW(), gc.pixelH()
	gc.applyBounds(gc.pixelX(), gc.pixelY(), pw, ph)
}

func (gc *GroupCard) ReapplyBounds() {
	w := gc.pixelW()
	h := gc.pixelH()
	if gc.isCollapsed {
		h = cardHeaderHeight + 4
	}
	x := gc.pixelX()
	y := gc.pixelY()
	gc.container.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w, Height: h})
	gc.bodyWidget.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: w, Height: h})
	gc.clearBgCache()
	gc.bodyWidget.Invalidate()
	logger.Debug("ReapplyBounds: %q bodyWidget.Invalidate called", gc.groupName)
}

func (gc *GroupCard) SetIsDropTarget(v bool) {
	if gc.isDropTarget != v {
		gc.isDropTarget = v
		gc.bodyWidget.Invalidate()
	}
}

func (gc *GroupCard) Cleanup() {}

// SelectItem 选中指定索引的图标（委托全局选中状态，未分组与分组共用）
func (gc *GroupCard) SelectItem(idx int) {
	if gc.selection == nil || idx < 0 || idx >= len(gc.items) {
		return
	}
	gc.selection.SetSelected(Selection{Path: gc.items[idx].Path, Card: gc.groupName})
}

// ClearSelection 清除全局选中状态
func (gc *GroupCard) ClearSelection() {
	if gc.selection != nil {
		gc.selection.ClearSelection()
	}
}

func (gc *GroupCard) SetOnCardBodyClick(fn func()) {
	gc.onCardBodyClick = fn
}

func (gc *GroupCard) SetOnCardClicked(fn func(card *GroupCard)) {
	gc.onCardClicked = fn
}

func (gc *GroupCard) SetOnCardDragOutline(fn func(card *GroupCard, newX, newY int)) {
	gc.onCardDragOutline = fn
}

func (gc *GroupCard) SetOnCardDragOutlineEnd(fn func(card *GroupCard)) {
	gc.onCardDragOutlineEnd = fn
}

func (gc *GroupCard) SetOnIconPress(fn func(card *GroupCard, idx int, item group.GroupItem, clientX, clientY int)) {
	gc.onIconPress = fn
}

func (gc *GroupCard) SetOnIconRelease(fn func()) {
	gc.onIconRelease = fn
}

func (gc *GroupCard) SetOnResizeOutline(fn func(card *GroupCard, newX, newY, newW, newH int)) {
	gc.onResizeOutline = fn
}

func (gc *GroupCard) SetOnResizeOutlineEnd(fn func(card *GroupCard)) {
	gc.onResizeOutlineEnd = fn
}

func (gc *GroupCard) SetOnGetWallpaper(fn func() *walk.Bitmap) {
	gc.onGetWallpaper = fn
}

// SetSelectionProvider 注入全局 hover/选中 状态提供者
func (gc *GroupCard) SetSelectionProvider(sp SelectionProvider) {
	gc.selection = sp
}

func (gc *GroupCard) GroupName() string { return gc.groupName }

func (gc *GroupCard) IsCollapsed() bool { return gc.isCollapsed }

// Overlaps 判断两张卡片（按各自当前实际显示尺寸）的矩形区域是否有交集
func (gc *GroupCard) Overlaps(other *GroupCard) bool {
	x, y, w := gc.pixelX(), gc.pixelY(), gc.pixelW()
	h := gc.pixelH()
	if gc.isCollapsed {
		h = cardHeaderHeight + 4
	}
	ox, oy, ow := other.pixelX(), other.pixelY(), other.pixelW()
	oh := other.pixelH()
	if other.isCollapsed {
		oh = cardHeaderHeight + 4
	}
	return x < ox+ow && x+w > ox && y < oy+oh && y+h > oy
}

func (gc *GroupCard) BodyWidgetHandle() win.HWND {
	return gc.bodyWidget.Handle()
}

func (gc *GroupCard) HitTestIcon(x, y int) int {
	return gc.getItemIndexAt(x, y)
}

func (gc *GroupCard) PixelW() int { return gc.pixelW() }
func (gc *GroupCard) PixelH() int { return gc.pixelH() }
func (gc *GroupCard) PixelX() int { return gc.pixelX() }
func (gc *GroupCard) PixelY() int { return gc.pixelY() }

// PixelRight 返回像素右下角 X 坐标（left + width）
func (gc *GroupCard) PixelRight() int { return gc.pixelX() + gc.pixelW() }

// PixelBottom 返回像素右下角 Y 坐标（top + height）
func (gc *GroupCard) PixelBottom() int { return gc.pixelY() + gc.pixelH() }

func (gc *GroupCard) SetDragNewPos(x, y int) {
	gc.dragNewX = x
	gc.dragNewY = y
}

// DragPosX 返回最新拖拽位置 X
func (gc *GroupCard) DragPosX() int { return gc.dragNewX }

// DragPosY 返回最新拖拽位置 Y
func (gc *GroupCard) DragPosY() int { return gc.dragNewY }

// ResizeNewX 返回最新缩放位置 X
func (gc *GroupCard) ResizeNewX() int { return gc.resizeNewX }

// ResizeNewY 返回最新缩放位置 Y
func (gc *GroupCard) ResizeNewY() int { return gc.resizeNewY }

// ResizeNewW 返回最新缩放宽度
func (gc *GroupCard) ResizeNewW() int { return gc.resizeNewW }

// ResizeNewH 返回最新缩放高度
func (gc *GroupCard) ResizeNewH() int { return gc.resizeNewH }

// SetResizeNewPos 设置缩放吸附后的位置和尺寸
func (gc *GroupCard) SetResizeNewPos(x, y, w, h int) {
	gc.resizeNewX = x
	gc.resizeNewY = y
	gc.resizeNewW = w
	gc.resizeNewH = h
}
