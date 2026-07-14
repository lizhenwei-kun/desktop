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
var (
	user32DLL                = syscall.NewLazyDLL("user32.dll")
	procCreateWindowExW      = user32DLL.NewProc("CreateWindowExW")
	procDestroyWindow        = user32DLL.NewProc("DestroyWindow")
	procUpdateLayeredWindow  = user32DLL.NewProc("UpdateLayeredWindow")
	procLoadCursorW          = user32DLL.NewProc("LoadCursorW")
	procDrawTextW            = user32DLL.NewProc("DrawTextW")
	procFillRect             = user32DLL.NewProc("FillRect")
	procGetDC                = user32DLL.NewProc("GetDC")
	procReleaseDC            = user32DLL.NewProc("ReleaseDC")
	gdi32DLL                 = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC   = gdi32DLL.NewProc("CreateCompatibleDC")
	procDeleteDC             = gdi32DLL.NewProc("DeleteDC")
	procCreateCompatibleBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procCreateSolidBrush     = gdi32DLL.NewProc("CreateSolidBrush")
	procSelectObject         = gdi32DLL.NewProc("SelectObject")
	procDeleteObject         = gdi32DLL.NewProc("DeleteObject")
)

const (
	freeGridLeft = 20
	freeGridTop  = 60
	_ULW_ALPHA   = 0x00000002
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

// ============================================================
// 统一图标拖拽入口（未分组 + 分组内图标共用）
// ============================================================

// handleDesktopMouseDown 桌面左键按下（未分组图标点击 + 拖拽检测）
func (dm *DesktopMode) handleDesktopMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}

	// 如果当前正在编辑标题，先结束编辑（保存修改）
	if dm.EditingPath != "" {
		dm.endItemEdit(true)
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

			if dm.SelectedPath == item.Path && dm.isInLabelArea(y, iy) {
				dm.startItemEdit(item.Path)
				return
			}

			// 双击检测
			if dm.LastClickPath == item.Path && !dm.LastClickTime.IsZero() &&
				time.Since(dm.LastClickTime) < 500*time.Millisecond {
				dm.Executor.Execute(item.Path)
				dm.LastClickTime = time.Time{}
				return
			}

			// 单击选中（同时清除所有卡片选中，保证全局唯一）
			for _, c := range dm.Cards {
				c.ClearSelection()
			}
			dm.selectItem(item.Path)
			dm.LastClickTime = time.Now()
			dm.LastClickPath = item.Path

			// 统一拖拽启动（未分组来源）
			dm.DragPressed = true
			dm.startIconDrag("", item.Path, item.Name, x, y)
			return
		}
	}

	// 检测卡片内图标点击
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
			var bodyPt win.POINT
			bodyPt.X = int32(sx)
			bodyPt.Y = int32(sy)
			win.ScreenToClient(card.BodyWidgetHandle(), &bodyPt)
			bodyX := int(bodyPt.X)
			bodyY := int(bodyPt.Y)
			if bodyY >= ui.CardHeaderHeight {
				itemIdx := card.HitTestIcon(bodyX, bodyY)
				if itemIdx >= 0 {
					items := card.Items()
					if itemIdx < len(items) {
						for _, c2 := range dm.Cards {
							if c2 != card {
								c2.ClearSelection()
							}
						}
						card.SelectItem(itemIdx)
						dm.selectItem(items[itemIdx].Path)
					}
				} else {
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

// selectItem 设置选中的项目路径（全局唯一，不操作卡片选中状态由调用方管理）
func (dm *DesktopMode) selectItem(itemPath string) {
	if dm.SelectedPath != itemPath {
		dm.SelectedPath = itemPath
		dm.BodyWidget.Invalidate()
	}
}

// clearSelectedItem 清除全局选中状态
func (dm *DesktopMode) clearSelectedItem() {
	if dm.EditingPath != "" {
		dm.endItemEdit(false)
	}
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

	if dm.EditHwnd != 0 {
		win.DestroyWindow(dm.EditHwnd)
		dm.EditHwnd = 0
	}

	ix, iy := dm.getFreeItemPixelPos(foundItem.Path, foundIdx)
	labelX := ix
	labelY := iy + ui.DesktopIconLabelTop
	labelW := ui.TileWidth()
	labelH := 2 * ui.DesktopIconLineHeight

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

	win.SendMessage(editHWND, win.EM_SETBKGNDCOLOR, 0, uintptr(win.RGB(0x30, 0x34, 0x3C)))
	win.SendMessage(editHWND, win.EM_SETSEL, 0, ^uintptr(0))
	win.SetFocus(editHWND)
	dm.EditHwnd = editHWND
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

// ============================================================
// 统一拖拽启动（未分组 + 分组内图标共用入口）
// ============================================================

// startIconDrag 记录按下状态，延迟后启动拖拽（未分组 + 分组内共用）
// 从按下到实际拖拽激活之间由 DragPressed 控制
func (dm *DesktopMode) startIconDrag(sourceGroup, itemPath, itemName string, clientX, clientY int) {
	dm.DragItemPath = itemPath
	dm.DragItemName = itemName
	dm.DragSourceGroup = sourceGroup
	dm.DragScreenX = clientX
	dm.DragScreenY = clientY
	dm.LastMoveTime = time.Now()

	go func() {
		defer recoverGoroutine("startIconDrag")
		time.Sleep(ui.IconDragDelay)
		dm.Post(func() {
			if !dm.DragPressed || dm.DragActive {
				return
			}
			var screenPt, clientPt win.POINT
			win.GetCursorPos(&screenPt)
			clientPt = screenPt
			win.ScreenToClient(dm.BodyWidget.Handle(), &clientPt)

			// 如果来源分组为空（未分组），查找项目信息填充名称和分组
			if dm.DragSourceGroup == "" {
				item := dm.findItemByPath(itemPath)
				if item != nil {
					dm.DragItemName = item.Name
					dm.DragSourceGroup = item.GroupName
				}
			}

			dm.DragActive = true
			dm.DragMouseX = int(clientPt.X)
			dm.DragMouseY = int(clientPt.Y)
			dm.DragScreenX = int(screenPt.X)
			dm.DragScreenY = int(screenPt.Y)
			dm.loadDragGhost(itemPath)
			dm.LastMoveTime = time.Now()
			// 创建幽灵窗口（自动绘制内容并显示）
			dm.createGhostWindow()
			win.SetCapture(dm.BodyWidget.Handle())
		})
	}()
}

// handleCardIconPress 卡片内图标按下回调（由 GroupCard.onIconPress 直接回调）
// 设置来源信息后调用 startIconDrag，由 UnifiedDragState 统一管理延迟检测
func (dm *DesktopMode) handleCardIconPress(card *ui.GroupCard, idx int, item group.GroupItem, clientX, clientY int) {
	dm.SourceCard = card
	dm.SourceItemIdx = idx
	dm.SourceItem = item
	dm.DragPressed = true
	// 将卡片内客户区坐标转为屏幕坐标
	var screenPt win.POINT
	screenPt.X = int32(clientX)
	screenPt.Y = int32(clientY)
	win.ClientToScreen(card.BodyWidgetHandle(), &screenPt)
	dm.startIconDrag(card.GroupName(), item.Path, item.Name, int(screenPt.X), int(screenPt.Y))
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

// ghostWindowSize 返回幽灵窗口的宽度和高度（按选中状态计算，包含图标和全部文字行）
func (dm *DesktopMode) ghostWindowSize() (int, int) {
	tileW := ui.TileWidth()
	displayName := dm.DragItemName
	lines := ui.SplitTextToLines(displayName, 4)
	tileH := ui.DesktopIconLabelTop + len(lines)*ui.DesktopIconLineHeight + 4
	if tileH < ui.TileHeight() {
		tileH = ui.TileHeight()
	}
	return tileW, tileH
}

// createGhostWindow 创建幽灵窗口（裸 POPUP 窗口 + UpdateLayeredWindow 绘制）
func (dm *DesktopMode) createGhostWindow() {
	if dm.GhostHwnd != 0 {
		return
	}
	hInst := win.GetModuleHandle(nil)

	// 注册窗口类
	className := syscall.StringToUTF16Ptr("DesktopGoGhost")
	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(ghostWndProc)
	wc.HInstance = hInst
	wc.HbrBackground = win.HBRUSH(win.GetStockObject(5)) // HOLLOW_BRUSH
	wc.LpszClassName = className
	win.RegisterClassEx(&wc)

	exStyle := uint32(win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT | win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	style := uint32(win.WS_POPUP)

	ghostW, ghostH := dm.ghostWindowSize()
	ghostX := dm.DragScreenX - ghostW/2
	ghostY := dm.DragScreenY - ghostH/2

	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(style),
		uintptr(ghostX), uintptr(ghostY), uintptr(ghostW), uintptr(ghostH),
		0, 0, uintptr(hInst), 0)
	if hwnd == 0 {
		return
	}
	dm.GhostHwnd = win.HWND(hwnd)

	win.ShowWindow(win.HWND(hwnd), win.SW_SHOWNA)
	dm.paintGhostIcon()
}

// destroyGhostWindow 销毁幽灵窗口
func (dm *DesktopMode) destroyGhostWindow() {
	if dm.GhostHwnd != 0 {
		procDestroyWindow.Call(uintptr(dm.GhostHwnd))
		dm.GhostHwnd = 0
	}
}

// moveGhostWindow 移动幽灵窗口到指定屏幕坐标
func (dm *DesktopMode) moveGhostWindow(screenX, screenY int) {
	if dm.GhostHwnd == 0 {
		return
	}
	ghostW, ghostH := dm.ghostWindowSize()
	ghostX := screenX - ghostW/2
	ghostY := screenY - ghostH/2
	win.SetWindowPos(dm.GhostHwnd, 0, int32(ghostX), int32(ghostY), 0, 0,
		win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
}

// paintGhostIcon 用 UpdateLayeredWindow 在幽灵窗口上绘制半透明图标和文字
func (dm *DesktopMode) paintGhostIcon() {
	if dm.GhostHwnd == 0 || dm.GhostBmp == nil {
		return
	}

	tileW, tileH := dm.ghostWindowSize()

	bmp, err := walk.NewBitmapWithTransparentPixelsForDPI(walk.Size{Width: tileW, Height: tileH}, dm.MainWindow.DPI())
	if err != nil || bmp == nil {
		return
	}
	defer bmp.Dispose()

	canvas, err := walk.NewCanvasFromImage(bmp)
	if err != nil || canvas == nil {
		return
	}
	defer canvas.Dispose()

	// 绘制图标（居中）
	iconX := (tileW - ui.DesktopIconSize) / 2
	iconY := ui.DesktopIconTop
	canvas.DrawBitmapWithOpacityPixels(dm.GhostBmp,
		walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize, Height: ui.DesktopIconSize}, 255)

	// 绘制文字（选中状态：显示所有行）
	font := ui.GetIconFont()
	if font != nil {
		defer font.Dispose()
		lines := ui.SplitTextToLines(dm.DragItemName, 4)
		labelTop := ui.DesktopIconLabelTop
		for i, line := range lines {
			lineY := labelTop + i*ui.DesktopIconLineHeight
			textBounds := walk.Rectangle{X: 0, Y: lineY, Width: tileW, Height: ui.DesktopIconLineHeight}
			canvas.DrawTextPixels(line, font, walk.RGB(0xFF, 0xFF, 0xFF), textBounds, walk.TextCenter|walk.TextSingleLine)
		}
	}

	img, err := bmp.ToImage()
	if err != nil || img == nil {
		return
	}

	hdcScreen := win.GetDC(0)
	if hdcScreen == 0 {
		return
	}
	defer win.ReleaseDC(0, hdcScreen)

	hdcMem := win.CreateCompatibleDC(hdcScreen)
	if hdcMem == 0 {
		return
	}
	defer win.DeleteDC(hdcMem)

	var bi win.BITMAPINFO
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth = int32(tileW)
	bi.BmiHeader.BiHeight = -int32(tileH) // 顶向下
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	hBmp := win.CreateDIBSection(hdcMem, &bi.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if hBmp == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(hBmp))

	hOld := win.SelectObject(hdcMem, win.HGDIOBJ(hBmp))
	if hOld == 0 {
		return
	}
	defer win.SelectObject(hdcMem, hOld)

	// 将 walk.Bitmap 像素复制到 DIB，并预乘 alpha（UpdateLayeredWindow 要求 premultiplied alpha）
	// GDI 在透明 DIB 上绘制文字时 alpha 可能为 0，对文字区域（y >= DesktopIconLabelTop）的非黑色像素强制不透明
	pixels := (*[1 << 24]byte)(bits)
	imgW := img.Bounds().Dx()
	imgH := img.Bounds().Dy()
	n := 0
	for y := 0; y < tileH; y++ {
		inTextArea := y >= ui.DesktopIconLabelTop
		for x := 0; x < tileW; x++ {
			if x >= imgW || y >= imgH {
				pixels[n+0] = 0
				pixels[n+1] = 0
				pixels[n+2] = 0
				pixels[n+3] = 0
			} else {
				c := img.RGBAAt(x, y)
				a := c.A
				// 文字区域：非黑色但 alpha 为 0 的像素，是被 GDI 错误标记的的文字，强制不透明
				if inTextArea && a == 0 && (c.R != 0 || c.G != 0 || c.B != 0) {
					a = 255
				}
				pixels[n+0] = byte(uint16(c.B) * uint16(a) / 255)
				pixels[n+1] = byte(uint16(c.G) * uint16(a) / 255)
				pixels[n+2] = byte(uint16(c.R) * uint16(a) / 255)
				pixels[n+3] = a
			}
			n += 4
		}
	}

	size := win.SIZE{CX: int32(tileW), CY: int32(tileH)}
	ptSrc := win.POINT{X: 0, Y: 0}
	blend := win.BLENDFUNCTION{
		BlendOp:             0, // AC_SRC_OVER
		BlendFlags:          0,
		SourceConstantAlpha: 200,
		AlphaFormat:         win.AC_SRC_ALPHA,
	}

	procUpdateLayeredWindow.Call(
		uintptr(dm.GhostHwnd),
		0,
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(hdcMem),
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		_ULW_ALPHA)
}

// ghostWndProc 幽灵窗口消息处理
func ghostWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(win.HWND(hwnd), msg, wParam, lParam)
}

// updateDropTarget 更新拖拽目标（用于拖拽中的高亮指示）
func (dm *DesktopMode) updateDropTarget(screenX, screenY int) {
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		onCard := screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height
		card.SetIsDropTarget(onCard)
	}
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

// ============================================================
// 统一拖拽释放（未分组 + 分组内图标共用）
// ============================================================

// handleIconDrop 统一图标拖拽释放
func (dm *DesktopMode) handleIconDrop(screenX, screenY int) {
	dm.DragActive = false
	dm.DisposeGhostBmp()
	// 销毁幽灵窗口
	dm.destroyGhostWindow()
	defer dm.BodyWidget.Invalidate()

	// 清除所有卡片的高亮
	for _, c := range dm.Cards {
		c.SetIsDropTarget(false)
	}

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

	sourceCard := dm.SourceCard

	if targetCard != nil && sourceCard != nil && targetCard == sourceCard {
		// 同一分组内：拖拽排序
		var bodyPt win.POINT
		bodyPt.X = int32(screenX)
		bodyPt.Y = int32(screenY)
		win.ScreenToClient(sourceCard.BodyWidgetHandle(), &bodyPt)
		newIdx := sourceCard.HitTestIcon(int(bodyPt.X), int(bodyPt.Y))
		if newIdx < 0 {
			items := sourceCard.Items()
			newIdx = len(items) - 1
		}
		dm.Manager.MoveItemWithinGroup(sourceCard.GroupName(), dm.DragItemPath, newIdx)
		sourceCard.Refresh()
	} else if targetCard != nil && sourceCard != nil && targetCard != sourceCard {
		// 从一个分组拖到另一个分组
		dm.Manager.MoveItemToGroup(dm.DragItemPath, targetCard.GroupName())
		targetCard.Refresh()
		sourceCard.Refresh()
	} else if targetCard != nil && sourceCard == nil {
		// 从未分组拖到分组
		dm.Manager.MoveItemToGroup(dm.DragItemPath, targetCard.GroupName())
		targetCard.Refresh()
	} else {
		// 拖到桌面空白区域
		if sourceCard != nil {
			// 从分组拖出到桌面
			dm.Manager.MoveItemToDesktop(dm.DragItemPath)
		}
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
		if sourceCard != nil {
			sourceCard.Refresh()
		}
	}

	// 清理拖拽状态
	dm.clearDragState()
}

// clearDragState 清除统一拖拽状态
func (dm *DesktopMode) clearDragState() {
	dm.DragItemPath = ""
	dm.DragItemName = ""
	dm.DragSourceGroup = ""
	dm.DragPressed = false
	dm.SourceCard = nil
	dm.SourceItemIdx = -1
	dm.SourceItem = group.GroupItem{}
	dm.destroyGhostWindow()
	win.ReleaseCapture()
}
