package desktop

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// Win32 函数
var procCreateWindowExW = syscall.NewLazyDLL("user32.dll").NewProc("CreateWindowExW")

const (
	freeGridLeft = 20
	freeGridTop  = 60
)

func freeCellW() int { return ui.TileWidth() + ui.DesktopIconGap }
func freeCellH() int { return ui.TileHeight() + ui.DesktopIconGap }

func gridToPixel(col, row int) (int, int) {
	return freeGridLeft + col*freeCellW(), freeGridTop + row*freeCellH()
}

func pixelToGrid(px, py int) (int, int) {
	col := (px - freeGridLeft + freeCellW()/2) / freeCellW()
	row := (py - freeGridTop + freeCellH()/2) / freeCellH()
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	return col, row
}

// posToGrid 保留在 DesktopMode，grid helpers 被多处调用
func (dm *DesktopMode) posToGrid(pos config.Position) (int, int) {
	px := int(pos.X * float64(dm.WorkW))
	py := int(pos.Y * float64(dm.WorkH))
	return pixelToGrid(px, py)
}

// gridToRel 保留在 DesktopMode
func (dm *DesktopMode) gridToRel(col, row int) config.Position {
	px, py := gridToPixel(col, row)
	return config.Position{
		X: float64(px) / float64(dm.WorkW),
		Y: float64(py) / float64(dm.WorkH),
	}
}

// getOccupiedCells 保留在 DesktopMode
func (dm *DesktopMode) getOccupiedCells(exceptPath string) map[[2]int]bool {
	items := dm.Manager.GetUngroupedItems()
	cells := make(map[[2]int]bool)
	for _, item := range items {
		if item.Path == exceptPath {
			continue
		}
		pos := dm.Manager.GetFreeItemPosition(item.Path)
		col, row := dm.posToGrid(pos)
		if col < 0 || row < 0 {
			continue
		}
		cell := [2]int{col, row}
		if !cells[cell] {
			cells[cell] = true
		}
	}
	return cells
}

// getFreeItemPixelPos 保留在 DesktopMode
func (dm *DesktopMode) getFreeItemPixelPos(path string, fallbackIdx int) (int, int) {
	pos := dm.Manager.GetFreeItemPosition(path)
	if pos.X < 0 || pos.Y < 0 {
		bounds := dm.BodyWidget.ClientBoundsPixels()
		if bounds.Width < 100 || bounds.Height < 100 {
			maxRow := dm.WorkH / freeCellH()
			if maxRow < 1 {
				maxRow = 1
			}
			col := fallbackIdx / maxRow
			row := fallbackIdx % maxRow
			return gridToPixel(col, row)
		}
		col, row := dm.findFreeGridCell("", 0, fallbackIdx)
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(path, relPos)
		return gridToPixel(col, row)
	}
	col, row := dm.posToGrid(pos)
	return gridToPixel(col, row)
}

// findFreeGridCell 保留在 DesktopMode
func (dm *DesktopMode) findFreeGridCell(exceptPath string, wantCol, wantRow int) (int, int) {
	occupied := dm.getOccupiedCells(exceptPath)
	bounds := dm.BodyWidget.ClientBoundsPixels()
	maxCol := bounds.Width / freeCellW()
	if maxCol < 1 {
		maxCol = 1
	}
	maxRow := bounds.Height / freeCellH()
	if maxRow < 1 {
		maxRow = 1
	}
	for attempt := 0; attempt < 500; attempt++ {
		cell := [2]int{wantCol, wantRow}
		if !occupied[cell] {
			return wantCol, wantRow
		}
		wantRow++
		if wantRow >= maxRow {
			wantRow = 0
			wantCol++
		}
		if wantCol >= maxCol {
			wantCol = 0
		}
	}
	return wantCol, wantRow
}

// handleDesktopMouseDown 桌面左键按下
func (dm *DesktopMode) handleDesktopMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}

	// 如果当前正在编辑标题，先结束编辑（保存修改）
	if dm.EditingFreeIdx >= 0 {
		dm.endFreeItemEdit(true)
		// 如果点击在编辑框区域内，让编辑框处理
		if dm.FreeEditHwnd != 0 {
			var rect win.RECT
			win.GetWindowRect(dm.FreeEditHwnd, &rect)
			var pt win.POINT
			pt.X = rect.Left
			pt.Y = rect.Top
			win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
			editX := int(pt.X)
			editY := int(pt.Y)
			editW := int(rect.Right - rect.Left)
			editH := int(rect.Bottom - rect.Top)
			if x >= editX && x <= editX+editW && y >= editY && y <= editY+editH {
				return
			}
		}
	}

	bounds := dm.BodyWidget.ClientBoundsPixels()
	btnRect := walk.Rectangle{X: bounds.Width - 140, Y: 10, Width: 120, Height: 30}
	if x >= btnRect.X && x <= btnRect.X+btnRect.Width &&
		y >= btnRect.Y && y <= btnRect.Y+btnRect.Height {
		dm.clearSelection()
		dm.addNewCard()
		return
	}

	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {

			// 如果已选中且点击在标签区域 → 进入编辑模式
			if dm.SelectedFreeIdx == i && dm.isInLabelArea(y, iy) {
				dm.startFreeItemEdit(i)
				return
			}

			// 单击选中
			dm.setSelection(i)

			dm.FreeItemDragPressed = true
			dm.FreeItemDragIdx = i
			dm.FreeItemDragItem = item
			dm.FreeItemDragStartX = x
			dm.FreeItemDragStartY = y
			dm.FreeItemDragStartTime = time.Now()
			go dm.checkFreeItemDragStart()
			return
		}
	}

	// 点击空白处清除选中
	dm.clearSelection()
}

// isInLabelArea 判断 y 坐标是否在图标磁贴的标签区域内
func (dm *DesktopMode) isInLabelArea(y, tileY int) bool {
	labelStart := tileY + ui.DesktopIconLabelTop
	labelEnd := labelStart + 2*ui.DesktopIconLineHeight
	return y >= labelStart && y < labelEnd
}

// setSelection 设置选中的未分组图标索引
func (dm *DesktopMode) setSelection(idx int) {
	if dm.SelectedFreeIdx != idx {
		dm.SelectedFreeIdx = idx
		dm.BodyWidget.Invalidate()
	}
}

// clearSelection 清除选中状态
func (dm *DesktopMode) clearSelection() {
	if dm.EditingFreeIdx >= 0 {
		dm.endFreeItemEdit(false)
	}
	if dm.SelectedFreeIdx != -1 {
		dm.SelectedFreeIdx = -1
		dm.BodyWidget.Invalidate()
	}
}

// startFreeItemEdit 开始编辑未分组图标的标题
func (dm *DesktopMode) startFreeItemEdit(idx int) {
	dm.EditingFreeIdx = idx
	items := dm.Manager.GetUngroupedItems()
	if idx < 0 || idx >= len(items) {
		dm.EditingFreeIdx = -1
		return
	}
	item := items[idx]

	// 如果已有编辑框，先销毁
	if dm.FreeEditHwnd != 0 {
		win.DestroyWindow(dm.FreeEditHwnd)
		dm.FreeEditHwnd = 0
	}

	// 获取标签区域坐标（与文字渲染位置精确一致）
	ix, iy := dm.getFreeItemPixelPos(item.Path, idx)
	labelX := ix
	labelY := iy + ui.DesktopIconLabelTop
	labelW := ui.TileWidth()
	labelH := 2 * ui.DesktopIconLineHeight

	// 使用原生 Win32 EDIT 控件，直接作为 bodyWidget 的子窗口（避开 Walk 布局）
	hwnd := dm.BodyWidget.Handle()
	className := syscall.StringToUTF16Ptr("EDIT")
	windowText := syscall.StringToUTF16Ptr(item.Name)
	style := uintptr(win.WS_CHILD | win.WS_VISIBLE | win.WS_BORDER | win.ES_LEFT | win.ES_AUTOHSCROLL)
	editHwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowText)),
		style,
		uintptr(labelX), uintptr(labelY), uintptr(labelW), uintptr(labelH),
		uintptr(hwnd), 0, 0, 0)
	if editHwnd == 0 {
		logger.Error("startFreeItemEdit: CreateWindowExW failed")
		dm.EditingFreeIdx = -1
		return
	}

	editHWND := win.HWND(editHwnd)

	// 设置字体
	font := ui.GetIconFont()
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

	// 设置背景色
	win.SendMessage(editHWND, win.EM_SETBKGNDCOLOR, 0, uintptr(win.RGB(0x30, 0x34, 0x3C)))

	// 选中全部文字
	win.SendMessage(editHWND, win.EM_SETSEL, 0, ^uintptr(0))
	win.SetFocus(editHWND)

	dm.FreeEditHwnd = editHWND

	// 子类化编辑框捕获事件
	dm.setupFreeEditSubclass(editHWND, idx)
}

// setupFreeEditSubclass 子类化编辑框，捕获失去焦点和按键事件
func (dm *DesktopMode) setupFreeEditSubclass(editHwnd win.HWND, idx int) {
	editCB := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case win.WM_KILLFOCUS:
			dm.Post(func() {
				dm.commitFreeItemEditFromHwnd(win.HWND(hwnd), idx)
				win.DestroyWindow(win.HWND(hwnd))
				dm.FreeEditHwnd = 0
				dm.EditingFreeIdx = -1
				dm.BodyWidget.Invalidate()
			})
			return 0
		case win.WM_KEYDOWN:
			if wParam == win.VK_RETURN {
				dm.Post(func() {
					dm.commitFreeItemEditFromHwnd(win.HWND(hwnd), idx)
					win.DestroyWindow(win.HWND(hwnd))
					dm.FreeEditHwnd = 0
					dm.EditingFreeIdx = -1
					dm.BodyWidget.Invalidate()
				})
				return 0
			}
			if wParam == win.VK_ESCAPE {
				dm.Post(func() {
					win.DestroyWindow(win.HWND(hwnd))
					dm.FreeEditHwnd = 0
					dm.EditingFreeIdx = -1
					dm.BodyWidget.Invalidate()
				})
				return 0
			}
		}
		ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})
	procSetWindowSubclass.Call(uintptr(editHwnd), editCB, 10001, uintptr(idx))
}

// commitFreeItemEditFromHwnd 从编辑框读取文字并提交
func (dm *DesktopMode) commitFreeItemEditFromHwnd(editHwnd win.HWND, idx int) {
	textLen := win.SendMessage(editHwnd, win.WM_GETTEXTLENGTH, 0, 0)
	buf := make([]uint16, textLen+1)
	win.SendMessage(editHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
	newName := syscall.UTF16ToString(buf)
	dm.commitFreeItemRename(newName, idx)
}

// endFreeItemEdit 结束编辑（save=true 时保存修改）
func (dm *DesktopMode) endFreeItemEdit(save bool) {
	if dm.EditingFreeIdx < 0 {
		return
	}
	if dm.FreeEditHwnd != 0 {
		if save {
			textLen := win.SendMessage(dm.FreeEditHwnd, win.WM_GETTEXTLENGTH, 0, 0)
			buf := make([]uint16, textLen+1)
			win.SendMessage(dm.FreeEditHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
			newName := syscall.UTF16ToString(buf)
			dm.commitFreeItemRename(newName, dm.EditingFreeIdx)
		}
		win.DestroyWindow(dm.FreeEditHwnd)
		dm.FreeEditHwnd = 0
	}
	dm.EditingFreeIdx = -1
	dm.BodyWidget.Invalidate()
}

// commitFreeItemRename 提交未分组图标重命名
func (dm *DesktopMode) commitFreeItemRename(newName string, idx int) {
	items := dm.Manager.GetUngroupedItems()
	if idx < 0 || idx >= len(items) {
		return
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return
	}
	item := items[idx]
	if newName == item.Name {
		return
	}

	oldPath := item.Path
	newPath, err := dm.Manager.RenameItem(oldPath, newName)
	if err != nil {
		logger.Warn("commitFreeItemRename: failed to rename %q -> %q: %v", oldPath, newName, err)
		return
	}

	// 更新图标缓存
	ui.GlobalIconBmpCache.Remove(oldPath)
	if newPath != "" {
		ui.GlobalIconBmpCache.GetOrLoad(newPath)
	}

	logger.Info("renamed: %q -> %q", oldPath, newPath)
}

// checkFreeItemHover 调用 HoverState
func (dm *DesktopMode) checkFreeItemHover(x, y int) bool {
	return dm.HoverState.CheckFreeItemHover(x, y, dm.Manager.GetUngroupedItems(), dm.getFreeItemPixelPos)
}

// checkFreeItemDragStart 长按延迟后启动未分组图标拖拽
func (dm *DesktopMode) checkFreeItemDragStart() {
	defer recoverGoroutine("checkFreeItemDragStart")
	time.Sleep(ui.IconDragDelay)
	dm.Post(func() {
		if !dm.FreeItemDragPressed || dm.FreeItemDragActive {
			return
		}
		dm.FreeItemDragActive = true
		var screenPt, clientPt win.POINT
		win.GetCursorPos(&screenPt)
		clientPt = screenPt
		win.ScreenToClient(dm.BodyWidget.Handle(), &clientPt)
		dm.FreeItemDragMouseX = int(clientPt.X)
		dm.FreeItemDragMouseY = int(clientPt.Y)
		dm.IconDragState.LoadGhostBmp(dm.FreeItemDragItem.Path)
		dm.LastDragMoveTime = time.Now()
		dm.BodyWidget.Invalidate()
		win.SetCapture(dm.BodyWidget.Handle())
		dm.IconDragState.ActivateFromFreeDrag(dm.FreeItemDragItem, int(screenPt.X), int(screenPt.Y))
	})
}

// handleFreeItemDrop 未分组图标拖拽释放
func (dm *DesktopMode) handleFreeItemDrop(screenX, screenY int) {
	dm.IconDragActive = false
	dm.IconDragState.DisposeGhostBmp()
	defer dm.BodyWidget.Invalidate()
	targetCard := dm.IconDragState.FindCardAtPoint(screenX, screenY)
	if targetCard != nil {
		dm.Manager.MoveItemToGroup(dm.FreeItemDragItem.Path, targetCard.GroupName())
		targetCard.Refresh()
	} else {
		var pt win.POINT
		pt.X = int32(screenX)
		pt.Y = int32(screenY)
		win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
		px := int(pt.X) - ui.TileWidth()/2
		py := int(pt.Y) - ui.TileHeight()/2
		wantCol, wantRow := pixelToGrid(px, py)
		col, row := dm.findFreeGridCell(dm.FreeItemDragItem.Path, wantCol, wantRow)
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(dm.FreeItemDragItem.Path, relPos)
	}
	dm.IconDragState.ClearDropState()
}
