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
	})
	card.SetOnSizeChanged(func(name string, w, h float64) {
		dm.Manager.UpdateGroupSize(name, w, h)
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
	card.SetOnCardDragOutlineEnd(dm.CardDragOutline.OnCardDragOutlineEnd)
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

	// 图标按下回调（通知 DesktopMode 通过 UnifiedDragState 统一管理拖拽）
	card.SetOnIconPress(dm.handleCardIconPress)

	// 图标释放回调（通知 DesktopMode 取消拖拽，防止点击变拖拽）
	card.SetOnIconRelease(func() {
		dm.DragPressed = false
		dm.SourceCard = nil
		dm.SourceItemIdx = -1
	})
}

func (dm *DesktopMode) refreshCards() {
	for _, card := range dm.Cards {
		card.Cleanup()
		card.Container().Dispose()
	}
	dm.Cards = nil
	dm.createGroupCards()
	dm.reapplyCardPositions()
	dm.InvalidateBody()
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
