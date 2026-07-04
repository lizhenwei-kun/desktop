package desktop

import (
	"github.com/lxn/win"

	"desktop_go/internal/config"
	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

func (dm *DesktopMode) addNewCard() {
	name, ok := ui.ShowInputDialog(dm.MainWindow, "新建分组", "请输入分组名称：", "")
	if !ok || name == "" {
		return
	}
	dm.Manager.CreateGroup(name, "#30343CBD")
	dm.refreshCards()
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
	card.SetOnIconDragStart(dm.onCardIconDragStart)
	card.SetOnIconDragMove(dm.onCardIconDragMove)
	card.SetOnIconDragEnd(dm.onCardIconDragEnd)
	card.SetOnMouseMove(func() {
		for _, c := range dm.Cards {
			if c != card {
				c.ClearHover()
			}
		}
	})
	card.SetOnCardDragOutline(dm.onCardDragOutline)
	card.SetOnCardDragOutlineEnd(dm.onCardDragOutlineEnd)
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
			dm.BodyWidget.Invalidate()
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
			dm.BodyWidget.Invalidate()
		}
	})
	card.SetOnRefresh(func() {
		dm.refreshDesktop()
	})
	card.SetOnResizeOutline(dm.onCardResizeOutline)
	card.SetOnResizeOutlineEnd(dm.onCardResizeOutlineEnd)
}

func (dm *DesktopMode) refreshCards() {
	for _, card := range dm.Cards {
		card.Cleanup()
		card.Container().Dispose()
	}
	dm.Cards = nil
	dm.createGroupCards()
	dm.BodyWidget.Invalidate()
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
