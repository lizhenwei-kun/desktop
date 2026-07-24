package desktop

import (
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/group"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

func (dm *DesktopMode) addNewCard() {
	name, ok := ui.ShowInputDialog(dm.MainWindow, "新建分组", "请输入分组名称：", "")
	if !ok || name == "" {
		return
	}
	dm.Manager.CreateGroup(name, "#30343CBD")
	pos := dm.findFreeGroupPosition()
	dm.Manager.UpdateGroupPosition(name, pos.X, pos.Y)
	dm.refreshCards()
}

// findFreeGroupPosition 查找不与现有卡片重叠的可用位置（相对坐标）
// 卡片默认大小约 0.156w x 0.288h，以网格方式排列避免重叠
func (dm *DesktopMode) findFreeGroupPosition() config.Position {
	groups := dm.Manager.GetGroups()
	const (
		cardW = 0.17 // 卡片宽度 + 间距
		cardH = 0.31 // 卡片高度 + 间距
		cols  = 5
		baseX = 0.05
		baseY = 0.05
	)
	for idx := 0; idx < 200; idx++ {
		col := idx % cols
		row := idx / cols
		x := baseX + float64(col)*cardW
		y := baseY + float64(row)*cardH
		overlap := false
		for _, g := range groups {
			dx := g.Position.X - x
			if dx < 0 {
				dx = -dx
			}
			dy := g.Position.Y - y
			if dy < 0 {
				dy = -dy
			}
			if dx < 0.14 && dy < 0.26 {
				overlap = true
				break
			}
		}
		if !overlap {
			return config.Position{X: x, Y: y}
		}
	}
	return config.Position{X: 0.1, Y: 0.1}
}

func (dm *DesktopMode) createGroupCards() {
	groups := dm.Manager.GetGroups()
	logger.Debug("createGroupCards: %d groups", len(groups))
	for i, grp := range groups {
		card, err := ui.NewGroupCard(dm.MainWindow, grp, dm.Manager, dm.Executor, dm.MainWindow, dm.WorkW, dm.WorkH)
		if err != nil {
			logger.Debug("createGroupCards: card[%d] %q error: %v", i, grp.Name, err)
			continue
		}
		b := card.Container().BoundsPixels()
		logger.Debug("createGroupCards: card[%d] %q bounds=(%d,%d,%dx%d) visible=%v handle=%v",
			i, grp.Name, b.X, b.Y, b.Width, b.Height, card.Container().Visible(), card.Container().Handle())
		dm.setupCardActions(card, grp)
		dm.Cards = append(dm.Cards, card)
	}
}

func (dm *DesktopMode) setupCardActions(card *ui.GroupCard, grp config.Group) {
	card.SetOnPositionChanged(func(name string, x, y float64) {
		dm.Manager.UpdateGroupPosition(name, x, y)
		// 位置变了，需要重绘桌面以刷新卡片下方的壁纸背景
		dm.InvalidateBody()
	})
	card.SetOnSizeChanged(func(name string, w, h float64) {
		dm.Manager.UpdateGroupSize(name, w, h)
		// 尺寸变了，需要重绘桌面以刷新卡片下方的壁纸背景
		dm.InvalidateBody()
	})
	card.SetOnIconLeftClick(func(c *ui.GroupCard, idx int, item group.GroupItem) {
		// 清除其他所有卡片的选中
		for _, c2 := range dm.Cards {
			if c2 != c {
				c2.ClearSelection()
			}
		}
		c.SelectItem(idx)
		dm.selectItem(item.Path)
	})
	card.SetOnIconRightClick(func(_ *ui.GroupCard, _ int, item group.GroupItem, screenX, screenY int) {
		// UI 主线程实时 COM 获取图标右键菜单
		showIconContextMenuReal(dm.MainWindow.Handle(), dm.Executor, item, screenX, screenY)
	})
	card.SetOnCardBodyClick(func() {
		card.ClearSelection()
		dm.clearSelectedItem()
	})
	card.SetOnCardDragOutline(dm.CardDragOutline.OnCardDragOutline)
	card.SetOnCardDragOutlineEnd(func(card *ui.GroupCard) {
		dm.CardDragOutline.OnCardDragOutlineEnd(card)
		// 刷新桌面，清除卡片原位置残留
		dm.InvalidateBody()
	})
	card.SetOnRename(func(name string) {
		newName, ok := ui.ShowInputDialog(dm.MainWindow, "重命名分组", "请输入新名称：", name)
		if ok && newName != "" && newName != name {
			dm.Manager.RenameGroup(name, newName)
			dm.refreshCards()
		}
	})
	card.SetOnColor(func(name string) {
		colorStr, ok := ui.ShowColorDialog(dm.MainWindow, "修改颜色", ui.PresetColors)
		if ok && colorStr != "" {
			dm.Manager.UpdateGroupColor(name, colorStr)
			card.SetGroupColor(colorStr)
			dm.InvalidateBody()
		}
	})
	card.SetOnDelete(func(name string) {
		if ui.ShowConfirmDialog(dm.MainWindow, "删除分组", "确定要删除分组「"+name+"」吗？\n分组内的项目将移回桌面。") {
			dm.Manager.DeleteGroup(name)
			for i, c := range dm.Cards {
				if c.GroupName() == name {
					c.Cleanup()
					c.Container().Dispose()
					dm.Cards = append(dm.Cards[:i], dm.Cards[i+1:]...)
					break
				}
			}
			dm.Refresh()
		}
	})
	card.SetOnResizeOutline(dm.ResizeOutlineState.OnCardResizeOutline)
	card.SetOnResizeOutlineEnd(dm.ResizeOutlineState.OnCardResizeOutlineEnd)

	// 提供桌面壁纸位图，卡片背景从真实壁纸合成
	card.SetOnGetWallpaper(dm.WallpaperState.getBitmap)

	// 图标按下回调（通知 DesktopMode 通过 UnifiedDragState 统一管理拖拽）
	card.SetOnIconPress(dm.handleCardIconPress)

	// 图标释放回调（通知 DesktopMode 取消拖拽，防止点击变拖拽）
	card.SetOnIconRelease(func() {
		dm.DragPressed = false
		dm.SourceCard = nil
		dm.SourceItemIdx = -1
	})
}

var refreshCardsCount int

// refreshCards 刷新所有卡片。
// 关键：尽量复用现有卡片控件（就地 Refresh + ReapplyBounds），
// 不要在每次刷新时销毁重建——Dispose 会让卡片窗口瞬间消失、再重建导致可见闪烁。
// 仅当分组集合（增删分组）发生变化时才销毁/新建对应卡片。
func (dm *DesktopMode) refreshCards() {
	refreshCardsCount++
	logger.Debug("refreshCards #%d: groups=%d cards=%d", refreshCardsCount, len(dm.Manager.GetGroups()), len(dm.Cards))

	groups := dm.Manager.GetGroups()

	// 冻结所有卡片和桌面重绘，所有操作完成后再一次性刷新
	for _, card := range dm.Cards {
		win.SendMessage(card.Container().Handle(), win.WM_SETREDRAW, 0, 0)
		win.SendMessage(card.BodyWidgetHandle(), win.WM_SETREDRAW, 0, 0)
	}
	win.SendMessage(dm.BodyWidget.Handle(), win.WM_SETREDRAW, 0, 0)

	// 当前分组名集合
	groupNames := make(map[string]bool, len(groups))
	for _, g := range groups {
		groupNames[g.Name] = true
	}

	// 1. 复用仍存在的卡片，移除已删除的卡片
	kept := make([]*ui.GroupCard, 0, len(dm.Cards))
	for _, card := range dm.Cards {
		name := card.GroupName()
		if groupNames[name] {
			// 分组仍存在：就地刷新内容（不重设位置，reapplyCardPositions 统一处理）
			card.Refresh()
			kept = append(kept, card)
		} else {
			// 分组已被删除：销毁卡片
			card.Cleanup()
			card.Container().Dispose()
		}
	}
	dm.Cards = kept

	// 2. 为新增的分组创建卡片
	for _, grp := range groups {
		exists := false
		for _, card := range dm.Cards {
			if card.GroupName() == grp.Name {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		card, err := ui.NewGroupCard(dm.MainWindow, grp, dm.Manager, dm.Executor, dm.MainWindow, dm.WorkW, dm.WorkH)
		if err != nil {
			logger.Debug("refreshCards: new card %q error: %v", grp.Name, err)
			continue
		}
		dm.setupCardActions(card, grp)
		dm.Cards = append(dm.Cards, card)
	}

	dm.reapplyCardPositions()

	// 解冻所有窗口，让系统自然处理挂起的重绘请求。
	// 不要在此处额外调用 InvalidateRect/UpdateWindow，因为：
	// 1. ReapplyBounds 中的 SetBoundsPixels + Invalidate 已在冻结期间标记了脏区域
	// 2. 额外的 UpdateWindow 强制同步重绘会阻塞 UI 线程，且多次强制重绘导致闪烁
	// 3. WM_SETREDRAW 1 恢复后系统会自动处理所有挂起的无效区域
	logger.Debug("refreshCards #%d: unfreeze (no force redraw)", refreshCardsCount)
	for _, card := range dm.Cards {
		win.SendMessage(card.Container().Handle(), win.WM_SETREDRAW, 1, 0)
		win.SendMessage(card.BodyWidgetHandle(), win.WM_SETREDRAW, 1, 0)
	}
	win.SendMessage(dm.BodyWidget.Handle(), win.WM_SETREDRAW, 1, 0)
}

// reapplyCardPositions 重新应用所有卡片的绝对定位，并确保卡片 Z-order 在 bodyWidget 上方
func (dm *DesktopMode) reapplyCardPositions() {
	for i, card := range dm.Cards {
		card.ReapplyBounds()
		// 确保卡片在 Z-order 顶部（在 bodyWidget 上方）
		win.SetWindowPos(card.Container().Handle(), win.HWND_TOP, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		if i == 0 {
			b := card.Container().BoundsPixels()
			parentHwnd := win.GetParent(card.Container().Handle())
			logger.Debug("reapplyCardPositions: card[0] bounds=(%d,%d,%dx%d), visible=%v, hwnd=%v, parent=%v, containerHwnd=%v",
				b.X, b.Y, b.Width, b.Height, card.Container().Visible(),
				card.Container().Handle(), parentHwnd, dm.Container.Handle())
		}
	}
}
