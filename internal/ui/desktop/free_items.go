package desktop

import (
	"image"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/safego"
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
	_ULW_ALPHA = 0x00000002
)

func freeCellW() int { return ui.TileWidth() + ui.DesktopIconGap() }
func freeCellH() int { return ui.TileHeight() + ui.DesktopIconGap() }

func gridToPixel(col, row int) (int, int) {
	return ui.FreeGridLeft() + col*freeCellW(), ui.FreeGridTop() + row*freeCellH()
}

// pixelToGrid 将像素坐标转换为网格（列, 行），不会做越界裁剪
func pixelToGrid(px, py int) (int, int) {
	left := ui.FreeGridLeft()
	top := ui.FreeGridTop()
	col := (px - left + freeCellW()/2) / freeCellW()
	row := (py - top + freeCellH()/2) / freeCellH()
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	return col, row
}

// maxGridRows 返回当前工作区可容纳的最大行数（列优先布局：先填满一列再换下一列）
func (dm *DesktopMode) maxGridRows() int {
	bounds := dm.BodyWidget.ClientBoundsPixels()
	h := bounds.Height
	if h < 100 {
		h = dm.WorkH
	}
	maxRow := (h - ui.FreeGridTop()) / freeCellH()
	if maxRow < 1 {
		maxRow = 1
	}
	return maxRow
}

// maxGridCols 返回当前工作区可容纳的最大列数
func (dm *DesktopMode) maxGridCols() int {
	bounds := dm.BodyWidget.ClientBoundsPixels()
	w := bounds.Width
	if w < 100 {
		w = dm.WorkW
	}
	maxCol := (w - ui.FreeGridLeft()) / freeCellW()
	if maxCol < 1 {
		maxCol = 1
	}
	return maxCol
}

// indexToGrid 将列优先的网格索引转换为 (列, 行)
// 布局顺序：从上到下填满一列，再从左到右换下一列
func (dm *DesktopMode) indexToGrid(idx int) (int, int) {
	if idx < 0 {
		return 0, 0
	}
	maxRow := dm.maxGridRows()
	col := idx / maxRow
	row := idx % maxRow
	return col, row
}

// gridToIndex 将 (列, 行) 转换为列优先的网格索引
func (dm *DesktopMode) gridToIndex(col, row int) int {
	maxRow := dm.maxGridRows()
	return col*maxRow + row
}

// getOccupiedIndices 返回所有已分配网格索引的集合（排除 exceptPath）
func (dm *DesktopMode) getOccupiedIndices(exceptPath string) map[int]bool {
	items := dm.Manager.GetUngroupedItems()
	occupied := make(map[int]bool, len(items))
	for _, item := range items {
		if item.Path == exceptPath {
			continue
		}
		idx := dm.Manager.GetFreeItemIndex(item.Path)
		if idx < 0 {
			continue
		}
		occupied[idx] = true
	}
	return occupied
}

// getFreeItemPixelPos 返回未分组项的像素坐标
// 若该项尚未分配索引（-1），则自动分配一个空闲格子并持久化
// fallbackIdx 用于在尺寸未就绪时的兜底分配
func (dm *DesktopMode) getFreeItemPixelPos(path string, fallbackIdx int) (int, int) {
	idx := dm.Manager.GetFreeItemIndex(path)
	if idx < 0 {
		bounds := dm.BodyWidget.ClientBoundsPixels()
		if bounds.Width < 100 || bounds.Height < 100 {
			// 尺寸未就绪，用 fallbackIdx 兜底（列优先）
			maxRow := dm.WorkH / freeCellH()
			if maxRow < 1 {
				maxRow = 1
			}
			col := fallbackIdx / maxRow
			row := fallbackIdx % maxRow
			return gridToPixel(col, row)
		}
		idx = dm.findFreeIndex("", fallbackIdx)
		dm.Manager.SetFreeItemIndex(path, idx)
	}
	col, row := dm.indexToGrid(idx)
	return gridToPixel(col, row)
}

// findFreeIndex 从 wantIdx 开始查找一个未被占用的网格索引（列优先顺序）
func (dm *DesktopMode) findFreeIndex(exceptPath string, wantIdx int) int {
	occupied := dm.getOccupiedIndices(exceptPath)
	maxRow := dm.maxGridRows()
	maxCol := dm.maxGridCols()
	total := maxCol * maxRow
	if wantIdx < 0 {
		wantIdx = 0
	}
	for attempt := 0; attempt < total+10; attempt++ {
		if !occupied[wantIdx] {
			return wantIdx
		}
		wantIdx++
		// 超出网格范围则回到 0 重新查找
		if wantIdx >= total {
			wantIdx = 0
		}
	}
	return wantIdx
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

			if dm.Selected.Path == item.Path && dm.isInLabelArea(y, iy) {
				dm.startItemEdit(item.Path)
				return
			}

			// 双击检测
			if dm.LastClickPath == item.Path && !dm.LastClickTime.IsZero() &&
				time.Since(dm.LastClickTime) < 500*time.Millisecond {
				logger.Debug("handleDesktopMouseDown: DOUBLE-CLICK detected, path=%q", item.Path)
				dm.Executor.Execute(item.Path)
				dm.LastClickTime = time.Time{}
				logger.Debug("handleDesktopMouseDown: Execute returned, returning")
				return
			}

			// 单击选中（全局唯一，未分组与分组共用）
			dm.selectItem(ui.Selection{Path: item.Path})
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
						dm.selectItem(ui.Selection{Path: items[itemIdx].Path, Card: card.GroupName()})
					}
				} else {
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
	labelStart := tileY + ui.DesktopIconLabelTop()
	labelEnd := labelStart + 2*ui.DesktopIconLineHeight()
	return y >= labelStart && y < labelEnd
}

// --- SelectionProvider 实现（未分组与分组图标共用全局 hover/选中状态）---

// invalidateSelection 精准重绘指定 selection 对应的图标磁贴（分组卡片内 或 未分组）。
// 未分组图标由桌面 BodyWidget 绘制（PaintBuffered，局部重绘安全）；
// 分组图标由对应卡片的 bodyWidget 绘制（局部 tile 重绘）。
// 若 Card 为空则按未分组处理；卡片内索引通过 InvalidateTileByPath 实时反查，避免过期。
func (dm *DesktopMode) invalidateSelection(sel ui.Selection) {
	if sel.Path == "" {
		return
	}
	// 分组卡片内图标：只重绘该卡片对应 tile
	if sel.Card != "" {
		for _, card := range dm.Cards {
			if card.GroupName() == sel.Card {
				card.InvalidateTileByPath(sel.Path)
				return
			}
		}
		// 卡片不存在（已删除分组等），回退到全卡片查找
		for _, card := range dm.Cards {
			if card.InvalidateTileByPath(sel.Path) {
				return
			}
		}
		return
	}
	// 未分组图标：只重绘桌面对应磁贴。
	// 重绘高度始终用图标全部文字行数（选中框最大高度），而不是当前 hover/选中状态，
	// 因为清除选中时 dm.Selected 可能已被改写，若按当前状态算高度会把选中框的
	// 扩展文字区域残留漏掉。用最大高度保证选中框边线被完整清除。
	items := dm.Manager.GetUngroupedItems()
	for i, item := range items {
		if item.Path != sel.Path {
			continue
		}
		px, py := dm.getFreeItemPixelPos(item.Path, i)
		lines := ui.SplitTextToLines(item.Name, 4)
		h := ui.DesktopIconLabelTop() + len(lines)*ui.DesktopIconLineHeight()
		if h < ui.TileHeight() {
			h = ui.TileHeight()
		}
		rect := win.RECT{
			Left: int32(px), Top: int32(py),
			Right: int32(px + ui.TileWidth()), Bottom: int32(py + h),
		}
		win.InvalidateRect(dm.BodyWidget.Handle(), &rect, false)
		return
	}
}

// GetSelected 返回全局选中状态
func (dm *DesktopMode) GetSelected() ui.Selection { return dm.Selected }

// SetSelected 设置全局选中状态（仅重绘发生变化的两个图标：旧框清除 + 新框绘制）
func (dm *DesktopMode) SetSelected(sel ui.Selection) {
	if dm.Selected == sel {
		return
	}
	old := dm.Selected
	dm.Selected = sel
	dm.invalidateSelection(old)
	dm.invalidateSelection(sel)
}

// GetHovered 返回全局悬停状态
func (dm *DesktopMode) GetHovered() ui.Selection { return dm.Hovered }

// SetHovered 设置全局悬停状态（仅重绘发生变化的两个图标：旧框清除 + 新框绘制）
func (dm *DesktopMode) SetHovered(sel ui.Selection) {
	if dm.Hovered == sel {
		return
	}
	old := dm.Hovered
	dm.Hovered = sel
	dm.invalidateSelection(old)
	dm.invalidateSelection(sel)
}

// ClearSelection 清除全局选中状态（仅重绘原选中图标）
func (dm *DesktopMode) ClearSelection() {
	if dm.Selected.Path == "" {
		return
	}
	old := dm.Selected
	dm.Selected = ui.Selection{}
	dm.invalidateSelection(old)
}

// selectItem 设置选中的项目（全局唯一）
func (dm *DesktopMode) selectItem(sel ui.Selection) {
	dm.SetSelected(sel)
}

// clearSelectedItem 清除全局选中状态
func (dm *DesktopMode) clearSelectedItem() {
	if dm.EditingPath != "" {
		dm.endItemEdit(false)
	}
	dm.ClearSelection()
}

// startItemEdit 开始编辑图标的标题
func (dm *DesktopMode) startItemEdit(itemPath string) {
	// 清除选中状态，避免编辑模式下残留选中框
	dm.Selected = ui.Selection{}
	dm.InvalidateBody()

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
	labelY := iy + ui.DesktopIconLabelTop()
	labelW := ui.TileWidth()
	labelH := 2 * ui.DesktopIconLineHeight()

	hwnd := dm.BodyWidget.Handle()
	className := syscall.StringToUTF16Ptr("EDIT")
	windowText := syscall.StringToUTF16Ptr(foundItem.Name)
	// 使用 ES_MULTILINE + ES_CENTER 使编辑框内文字与绘制文字位置一致：
	//   - 去掉 WS_BORDER：避免边框导致客户区偏移
	//   - ES_MULTILINE：支持多行显示，文字从顶部开始，与绘制对齐
	//   - ES_CENTER：文字居中，与 DrawTextPixels 的 TextCenter 一致
	//   - ES_AUTOHSCROLL → ES_AUTOVSCROLL：多行模式下垂直滚动
	//   - WS_CLIPCHILDREN：防止闪烁
	style := uintptr(win.WS_CHILD | win.WS_VISIBLE | win.ES_MULTILINE | win.ES_CENTER | win.ES_AUTOVSCROLL | win.WS_CLIPCHILDREN)
	// WS_EX_CLIENTEDGE 提供柔和的内嵌边框，替代 WS_BORDER
	editHwnd, _, _ := procCreateWindowExW.Call(
		uintptr(win.WS_EX_CLIENTEDGE),
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

	// EM_SETTEXTCOLOR = WM_USER + 68 (richedit 消息，对普通 EDIT 也有效)
	win.SendMessage(editHWND, win.EM_SETBKGNDCOLOR, 0, uintptr(win.RGB(0x30, 0x34, 0x3C)))
	win.SendMessage(editHWND, win.WM_USER+68, 0, uintptr(win.RGB(0xFF, 0xFF, 0xFF)))
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
				dm.InvalidateBody()
			})
			return 0
		case win.WM_KEYDOWN:
			if wParam == win.VK_RETURN {
				dm.Post(func() {
					dm.commitItemEditFromHwnd(win.HWND(hwnd), itemPath)
					win.DestroyWindow(win.HWND(hwnd))
					dm.EditHwnd = 0
					dm.EditingPath = ""
					dm.InvalidateBody()
				})
				return 0
			}
			if wParam == win.VK_ESCAPE {
				dm.Post(func() {
					win.DestroyWindow(win.HWND(hwnd))
					dm.EditHwnd = 0
					dm.EditingPath = ""
					dm.InvalidateBody()
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
	dm.InvalidateBody()
}

// commitItemRename 提交图标重命名
func (dm *DesktopMode) commitItemRename(newName string, itemPath string) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return
	}

	// 从所有项目（分组内 + 未分组）中查找当前名称，
	// 卡片内的图标属于分组项，必须用 GetAllItems 才能找到
	item := dm.findItemByPath(itemPath)
	if item == nil {
		return
	}
	currentName := item.Name
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

	safego.Go("startIconDrag", func() {
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
	})
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
	tileH := ui.DesktopIconLabelTop() + len(lines)*ui.DesktopIconLineHeight() + 4
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

	// 尺寸变化或首次绘制时重建 DIB 缓存
	if dm.ghostDibBmp == 0 || dm.ghostDibW != tileW || dm.ghostDibH != tileH {
		// 释放旧缓存
		dm.disposeGhostDib()

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
		iconX := (tileW - ui.DesktopIconSize()) / 2
		iconY := ui.DesktopIconTop()
		canvas.DrawBitmapWithOpacityPixels(dm.GhostBmp,
			walk.Rectangle{X: iconX, Y: iconY, Width: ui.DesktopIconSize(), Height: ui.DesktopIconSize()}, 255)

		// 绘制文字
		font := ui.GetIconFont()
		if font != nil {
			defer font.Dispose()
			lines := ui.SplitTextToLines(dm.DragItemName, 4)
			drawIconLabel(canvas, font, lines, 0, ui.DesktopIconLabelTop(), tileW)
		}

		img, err := bmp.ToImage()
		if err != nil || img == nil {
			return
		}

		// 创建 DIB 并缓存
		dm.createGhostDib(tileW, tileH, img)
	}

	if dm.ghostDibBmp == 0 {
		return
	}

	// 用缓存的 DIB 直接更新图层
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

	hOld := win.SelectObject(hdcMem, win.HGDIOBJ(dm.ghostDibBmp))
	if hOld == 0 {
		return
	}
	defer win.SelectObject(hdcMem, hOld)

	size := win.SIZE{CX: int32(tileW), CY: int32(tileH)}
	ptSrc := win.POINT{X: 0, Y: 0}
	blend := win.BLENDFUNCTION{
		BlendOp:             0,
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

// createGhostDib 创建并缓存 DIB，将 walk.Bitmap 像素预乘 alpha 后写入
func (dm *DesktopMode) createGhostDib(tileW, tileH int, img *image.RGBA) {
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
	bi.BmiHeader.BiHeight = -int32(tileH)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = win.BI_RGB

	var bits unsafe.Pointer
	hBmp := win.CreateDIBSection(hdcMem, &bi.BmiHeader, win.DIB_RGB_COLORS, &bits, 0, 0)
	if hBmp == 0 {
		return
	}

	// 像素预乘 alpha
	pixels := (*[1 << 24]byte)(bits)
	imgW := img.Bounds().Dx()
	imgH := img.Bounds().Dy()
	n := 0
	for y := 0; y < tileH; y++ {
		inTextArea := y >= ui.DesktopIconLabelTop()
		for x := 0; x < tileW; x++ {
			if x >= imgW || y >= imgH {
				pixels[n+0] = 0
				pixels[n+1] = 0
				pixels[n+2] = 0
				pixels[n+3] = 0
			} else {
				c := img.RGBAAt(x, y)
				a := c.A
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

	dm.ghostDibBmp = hBmp
	dm.ghostDibW = tileW
	dm.ghostDibH = tileH
	dm.ghostDibBits = bits
}

// disposeGhostDib 释放缓存的 DIB
func (dm *DesktopMode) disposeGhostDib() {
	if dm.ghostDibBmp != 0 {
		win.DeleteObject(win.HGDIOBJ(dm.ghostDibBmp))
		dm.ghostDibBmp = 0
	}
	dm.ghostDibBits = nil
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
	dm.disposeGhostDib()
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
	defer dm.InvalidateBody()

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
		wantIdx := dm.gridToIndex(wantCol, wantRow)
		idx := dm.findFreeIndex(dm.DragItemPath, wantIdx)
		dm.Manager.SetFreeItemIndex(dm.DragItemPath, idx)
		if sourceCard != nil {
			sourceCard.Refresh()
		}
	}

	// 若拖放的图标恰是当前选中项，移动位置后需同步更新其所在卡片/索引
	dm.refreshSelectedPosition()

	// 清理拖拽状态
	dm.clearDragState()
}

// refreshSelectedPosition 重新定位全局选中图标所在卡片（空 = 未分组）。
// 图标被拖拽移动（跨卡片/换位置）后调用，保证 Selected.Card 与最新位置一致。
func (dm *DesktopMode) refreshSelectedPosition() {
	if dm.Selected.Path == "" {
		return
	}
	path := dm.Selected.Path
	// 查分组卡片
	for _, card := range dm.Cards {
		items := card.Items()
		for _, item := range items {
			if item.Path == path {
				dm.Selected.Card = card.GroupName()
				return
			}
		}
	}
	// 查未分组
	if item := dm.findUngroupedByPath(path); item != nil {
		dm.Selected.Card = ""
		return
	}
	// 图标已不存在（被删除等）→ 清空选中
	dm.Selected = ui.Selection{}
}

// findUngroupedByPath 在未分组中查找指定 path 的项目，返回 nil 表示不存在
func (dm *DesktopMode) findUngroupedByPath(path string) *group.GroupItem {
	for _, item := range dm.Manager.GetUngroupedItems() {
		if item.Path == path {
			return &item
		}
	}
	return nil
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
