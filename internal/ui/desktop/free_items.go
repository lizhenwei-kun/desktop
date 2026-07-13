package desktop

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/group"
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

// findItemByPath 通过路径在所有项目中查找项目信息
func (dm *DesktopMode) findItemByPath(path string) *group.ItemInfo {
	allItems := dm.Manager.GetAllItems()
	for _, item := range allItems {
		if item.Path == path {
			return &item
		}
	}
	return nil
}

// handleDesktopMouseDown 桌面左键按下
func (dm *DesktopMode) handleDesktopMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}

	// 如果当前正在编辑标题，先结束编辑（保存修改）
	if dm.EditingPath != "" {
		dm.endItemEdit(true)
		// 如果点击在编辑框区域内，让编辑框处理
		if dm.EditHwnd != 0 {
			var rect win.RECT
			win.GetWindowRect(dm.EditHwnd, &rect)
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
		dm.clearSelectedItem()
		dm.addNewCard()
		return
	}

	// 检测未分组图标点击
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, i)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {

			// 如果已选中且点击在标签区域 → 进入编辑模式
			if dm.SelectedPath == item.Path && dm.isInLabelArea(y, iy) {
				dm.startItemEdit(item.Path)
				return
			}

			// 双击检测
			if dm.lastClickPath == item.Path && !dm.lastClickTime.IsZero() &&
				time.Since(dm.lastClickTime) < 500*time.Millisecond {
				// 双击→执行程序
				dm.Executor.Execute(item.Path)
				dm.lastClickTime = time.Time{}
				return
			}

			// 单击选中（全局唯一）
			dm.selectItem(item.Path)
			dm.lastClickTime = time.Now()
			dm.lastClickPath = item.Path

			// 记录按下，延迟后启动拖拽
			dm.dragPressed = true
			dm.dragHoldStart(item.Path, x, y)
			return
		}
	}

	// 检测卡片内图标点击（作为卡片自身 MouseDown 的补充/备选）
	if dm.isPointInAnyCard(x, y) {
		return
	}

	// 点击空白处清除选中
	dm.clearSelectedItem()
}

// isPointInAnyCard 判断客户区坐标是否在任意卡片区域内
func (dm *DesktopMode) isPointInAnyCard(cx, cy int) bool {
	var pt win.POINT
	pt.X = int32(cx)
	pt.Y = int32(cy)
	win.ClientToScreen(dm.BodyWidget.Handle(), &pt)
	sx := int(pt.X)
	sy := int(pt.Y)
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if sx >= sb.X && sx <= sb.X+sb.Width && sy >= sb.Y && sy <= sb.Y+sb.Height {
			// 找到被点击的卡片，将屏幕坐标转为卡片 bodyWidget 客户区坐标
			var bodyPt win.POINT
			bodyPt.X = int32(sx)
			bodyPt.Y = int32(sy)
			win.ScreenToClient(card.BodyWidgetHandle(), &bodyPt)
			bodyX := int(bodyPt.X)
			bodyY := int(bodyPt.Y)
			// 如果在卡片主体区域（非标题栏），尝试查找并选中图标
			if bodyY >= ui.CardHeaderHeight {
				itemIdx := card.HitTestIcon(bodyX, bodyY)
				if itemIdx >= 0 {
					items := card.Items()
					if itemIdx < len(items) {
						// 清除其他卡片选中
						for _, c2 := range dm.Cards {
							if c2 != card {
								c2.ClearSelection()
							}
						}
						card.SelectItem(itemIdx)
						dm.selectItem(items[itemIdx].Path)
					}
				} else {
					// 点击卡片空白区域 → 清除选中
					card.ClearSelection()
					dm.clearSelectedItem()
				}
			}
			return true
		}
	}
	return false
}

// isInLabelArea 判断 y 坐标是否在图标磁贴的标签区域内
func (dm *DesktopMode) isInLabelArea(y, tileY int) bool {
	labelStart := tileY + ui.DesktopIconLabelTop
	labelEnd := labelStart + 2*ui.DesktopIconLineHeight
	return y >= labelStart && y < labelEnd
}

// selectItem 设置选中的项目路径（全局唯一）
func (dm *DesktopMode) selectItem(itemPath string) {
	if dm.SelectedPath != itemPath {
		// 清除所有卡片的选中
		for _, c := range dm.Cards {
			c.ClearSelection()
		}
		dm.SelectedPath = itemPath
		dm.BodyWidget.Invalidate()
	}
}

// clearSelectedItem 清除全局选中状态
func (dm *DesktopMode) clearSelectedItem() {
	if dm.EditingPath != "" {
		dm.endItemEdit(false)
	}
	// 清除所有卡片的选中
	for _, c := range dm.Cards {
		c.ClearSelection()
	}
	if dm.SelectedPath != "" {
		dm.SelectedPath = ""
		dm.BodyWidget.Invalidate()
	}
}

// startItemEdit 开始编辑图标的标题
func (dm *DesktopMode) startItemEdit(itemPath string) {
	dm.EditingPath = itemPath
	items := dm.Manager.GetUngroupedItems()
	var foundItem *group.GroupItem
	var foundIdx int
	for i, item := range items {
		if item.Path == itemPath {
			foundItem = &item
			foundIdx = i
			break
		}
	}
	if foundItem == nil {
		dm.EditingPath = ""
		return
	}

	// 如果已有编辑框，先销毁
	if dm.EditHwnd != 0 {
		win.DestroyWindow(dm.EditHwnd)
		dm.EditHwnd = 0
	}

	// 获取标签区域坐标（与文字渲染位置精确一致）
	ix, iy := dm.getFreeItemPixelPos(foundItem.Path, foundIdx)
	labelX := ix
	labelY := iy + ui.DesktopIconLabelTop
	labelW := ui.TileWidth()
	labelH := 2 * ui.DesktopIconLineHeight

	// 使用原生 Win32 EDIT 控件，直接作为 bodyWidget 的子窗口（避开 Walk 布局）
	hwnd := dm.BodyWidget.Handle()
	className := syscall.StringToUTF16Ptr("EDIT")
	windowText := syscall.StringToUTF16Ptr(foundItem.Name)
	style := uintptr(win.WS_CHILD | win.WS_VISIBLE | win.WS_BORDER | win.ES_LEFT | win.ES_AUTOHSCROLL)
	editHwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowText)),
		style,
		uintptr(labelX), uintptr(labelY), uintptr(labelW), uintptr(labelH),
		uintptr(hwnd), 0, 0, 0)
	if editHwnd == 0 {
		logger.Error("startItemEdit: CreateWindowExW failed")
		dm.EditingPath = ""
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

	dm.EditHwnd = editHWND

	// 子类化编辑框捕获事件
	dm.setupItemEditSubclass(editHWND, itemPath)
}

// setupItemEditSubclass 子类化编辑框，捕获失去焦点和按键事件
func (dm *DesktopMode) setupItemEditSubclass(editHwnd win.HWND, itemPath string) {
	editCB := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case win.WM_KILLFOCUS:
			dm.Post(func() {
				dm.commitItemEditFromHwnd(win.HWND(hwnd), itemPath)
				win.DestroyWindow(win.HWND(hwnd))
				dm.EditHwnd = 0
				dm.EditingPath = ""
				dm.BodyWidget.Invalidate()
			})
			return 0
		case win.WM_KEYDOWN:
			if wParam == win.VK_RETURN {
				dm.Post(func() {
					dm.commitItemEditFromHwnd(win.HWND(hwnd), itemPath)
					win.DestroyWindow(win.HWND(hwnd))
					dm.EditHwnd = 0
					dm.EditingPath = ""
					dm.BodyWidget.Invalidate()
				})
				return 0
			}
			if wParam == win.VK_ESCAPE {
				dm.Post(func() {
					win.DestroyWindow(win.HWND(hwnd))
					dm.EditHwnd = 0
					dm.EditingPath = ""
					dm.BodyWidget.Invalidate()
				})
				return 0
			}
		}
		ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})
	procSetWindowSubclass.Call(uintptr(editHwnd), editCB, 10001, 0)
}

// commitItemEditFromHwnd 从编辑框读取文字并提交
func (dm *DesktopMode) commitItemEditFromHwnd(editHwnd win.HWND, itemPath string) {
	textLen := win.SendMessage(editHwnd, win.WM_GETTEXTLENGTH, 0, 0)
	buf := make([]uint16, textLen+1)
	win.SendMessage(editHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
	newName := syscall.UTF16ToString(buf)
	dm.commitItemRename(newName, itemPath)
}

// endItemEdit 结束编辑（save=true 时保存修改）
func (dm *DesktopMode) endItemEdit(save bool) {
	if dm.EditingPath == "" {
		return
	}
	if dm.EditHwnd != 0 {
		if save {
			textLen := win.SendMessage(dm.EditHwnd, win.WM_GETTEXTLENGTH, 0, 0)
			buf := make([]uint16, textLen+1)
			win.SendMessage(dm.EditHwnd, win.WM_GETTEXT, uintptr(textLen+1), uintptr(unsafe.Pointer(&buf[0])))
			newName := syscall.UTF16ToString(buf)
			dm.commitItemRename(newName, dm.EditingPath)
		}
		win.DestroyWindow(dm.EditHwnd)
		dm.EditHwnd = 0
	}
	dm.EditingPath = ""
	dm.BodyWidget.Invalidate()
}

// commitItemRename 提交图标重命名
func (dm *DesktopMode) commitItemRename(newName string, itemPath string) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return
	}

	// 查找当前名称
	items := dm.Manager.GetUngroupedItems()
	var currentName string
	for _, item := range items {
		if item.Path == itemPath {
			currentName = item.Name
			break
		}
	}
	if currentName == "" {
		return
	}
	if newName == currentName {
		return
	}

	oldPath := itemPath
	newPath, err := dm.Manager.RenameItem(oldPath, newName)
	if err != nil {
		logger.Warn("commitItemRename: failed to rename %q -> %q: %v", oldPath, newName, err)
		return
	}

	// 更新图标缓存
	ui.GlobalIconBmpCache.Remove(oldPath)
	if newPath != "" {
		ui.GlobalIconBmpCache.GetOrLoad(newPath)
	}

	logger.Info("renamed: %q -> %q", oldPath, newPath)
}

// checkFreeItemHover 悬停检测
func (dm *DesktopMode) checkFreeItemHover(x, y int) bool {
	items := dm.Manager.GetUngroupedItems()
	newHoveredPath := ""
	for _, item := range items {
		ix, iy := dm.getFreeItemPixelPos(item.Path, 0)
		if x >= ix && x <= ix+ui.TileWidth() &&
			y >= iy && y <= iy+ui.TileHeight() {
			newHoveredPath = item.Path
			break
		}
	}
	if newHoveredPath != dm.HoveredPath {
		dm.HoveredPath = newHoveredPath
		return true
	}
	return false
}

// dragHoldStart 记录按下状态，延迟后启动拖拽（仅当鼠标仍在按下时）
func (dm *DesktopMode) dragHoldStart(itemPath string, x, y int) {
	dm.DragItemPath = itemPath
	dm.DragItemName = ""
	dm.DragScreenX = x
	dm.DragScreenY = y
	dm.LastMoveTime = time.Now()
	go func() {
		defer recoverGoroutine("dragHoldStart")
		time.Sleep(ui.IconDragDelay)
		dm.Post(func() {
			if !dm.dragPressed || dm.DragActive {
				return
			}
			// 获取实时光标位置
			var screenPt, clientPt win.POINT
			win.GetCursorPos(&screenPt)
			clientPt = screenPt
			win.ScreenToClient(dm.BodyWidget.Handle(), &clientPt)

			// 查找项目信息
			item := dm.findItemByPath(itemPath)
			name := ""
			srcGroup := ""
			if item != nil {
				name = item.Name
				srcGroup = item.GroupName
			}

			dm.DragActive = true
			dm.DragItemName = name
			dm.DragSourceGroup = srcGroup
			dm.DragMouseX = int(clientPt.X)
			dm.DragMouseY = int(clientPt.Y)
			dm.DragScreenX = int(screenPt.X)
			dm.DragScreenY = int(screenPt.Y)
			dm.loadDragGhost(itemPath)
			dm.LastMoveTime = time.Now()
			dm.BodyWidget.Invalidate()
			win.SetCapture(dm.BodyWidget.Handle())
		})
	}()
}

// loadDragGhost 加载拖拽 ghost 图标 bitmap
func (dm *DesktopMode) loadDragGhost(path string) {
	dm.disposeDragGhost()
	dm.GhostBmp = ui.GlobalIconBmpCache.GetOrLoad(path)
}

// disposeDragGhost 释放 ghost 图标 bitmap
func (dm *DesktopMode) disposeDragGhost() {
	dm.GhostBmp = nil
}

// updateDropTarget 更新拖拽目标（用于拖拽中的高亮指示）
func (dm *DesktopMode) updateDropTarget(screenX, screenY int) {
	// 拖拽过程中：检查是否在卡片上
	var onCard bool
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			onCard = true
			break
		}
	}
	_ = onCard
}

// LoadGhostBmp 加载拖拽 ghost 图像
func (dm *DesktopMode) LoadGhostBmp(filePath string) {
	dm.DisposeGhostBmp()
	dm.GhostBmp = ui.GlobalIconBmpCache.GetOrLoad(filePath)
}

// DisposeGhostBmp 释放拖拽 ghost 图像
func (dm *DesktopMode) DisposeGhostBmp() {
	dm.GhostBmp = nil
}

// handleFreeItemDrop 未分组图标拖拽释放
func (dm *DesktopMode) handleFreeItemDrop(screenX, screenY int) {
	dm.DragActive = false
	dm.DisposeGhostBmp()
	defer dm.BodyWidget.Invalidate()

	// 检查是否拖放到卡片上
	var targetCard *ui.GroupCard
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			targetCard = card
			break
		}
	}

	if targetCard != nil {
		dm.Manager.MoveItemToGroup(dm.DragItemPath, targetCard.GroupName())
		targetCard.Refresh()
	} else {
		var pt win.POINT
		pt.X = int32(screenX)
		pt.Y = int32(screenY)
		win.ScreenToClient(dm.BodyWidget.Handle(), &pt)
		px := int(pt.X) - ui.TileWidth()/2
		py := int(pt.Y) - ui.TileHeight()/2
		wantCol, wantRow := pixelToGrid(px, py)
		col, row := dm.findFreeGridCell(dm.DragItemPath, wantCol, wantRow)
		relPos := dm.gridToRel(col, row)
		dm.Manager.SetFreeItemPosition(dm.DragItemPath, relPos)
	}

	// 清理拖拽状态
	dm.DragItemPath = ""
	dm.DragItemName = ""
	dm.DragSourceGroup = ""
}
