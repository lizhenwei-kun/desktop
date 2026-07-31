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

func (dm *DesktopMode) findFreeGroupPosition() config.Position {
	groups := dm.Manager.GetGroups()
	const (
		cardW = 0.17
		cardH = 0.31
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
		dm.InvalidateBody()
	})
	card.SetOnSizeChanged(func(name string, w, h float64) {
		dm.Manager.UpdateGroupSize(name, w, h)
		dm.InvalidateBody()
	})
	card.SetOnIconLeftClick(func(c *ui.GroupCard, idx int, item group.GroupItem) {
		for _, c2 := range dm.Cards {
			if c2 != c {
				c2.ClearSelection()
			}
		}
		c.SelectItem(idx)
		dm.selectItem(item.Path)
	})
	card.SetOnIconRightClick(func(_ *ui.GroupCard, _ int, item group.GroupItem, screenX, screenY int) {
		showIconContextMenuReal(dm.MainWindow.Handle(), dm.Executor, item, screenX, screenY)
	})
	card.SetOnCardBodyClick(func() {
		card.ClearSelection()
		dm.clearSelectedItem()
	})

	card.SetOnCardDragOutline(func(c *ui.GroupCard, newX, newY int) {
		dm.CardDragOutline.OnCardDragOutlineEx(c, newX, newY, dm.Cards)
	})
	card.SetOnCardDragOutlineEnd(func(card *ui.GroupCard) {
		// 用拖拽中的实际位置作为吸附基准（不是原位置）
		dragX := dm.CardDragOutline.DragOutlineX
		dragY := dm.CardDragOutline.DragOutlineY
		snapX, snapY := dm.CardDragOutline.SnapPosition(card, dm.Cards, dragX, dragY)
		card.SetDragNewPos(snapX, snapY)
		dm.CardDragOutline.OnCardDragOutlineEnd(card)
		cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
		logger.Debug("dragEnd: card=%q snap=(%d,%d) pos=(%d,%d,%dx%d) totalCards=%d",
			card.GroupName(), snapX, snapY, cx, cy, cw, ch, len(dm.Cards))
		for _, c := range dm.Cards {
			if c == card {
				continue
			}
			ox, oy, ow, oh := c.PixelX(), c.PixelY(), c.PixelW(), c.PixelH()
			if cx < ox+ow && cx+cw > ox && cy < oy+oh && cy+ch > oy {
				win.InvalidateRect(c.BodyWidgetHandle(), nil, false)
			}
		}
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
	card.SetOnResizeOutlineEnd(func(card *ui.GroupCard) {
		dm.ResizeOutlineState.OnCardResizeOutlineEnd(card)
		cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
		logger.Debug("resizeEnd: card=%q pos=(%d,%d,%dx%d) totalCards=%d", card.GroupName(), cx, cy, cw, ch, len(dm.Cards))
		for _, c := range dm.Cards {
			if c == card {
				continue
			}
			ox, oy, ow, oh := c.PixelX(), c.PixelY(), c.PixelW(), c.PixelH()
			if cx < ox+ow && cx+cw > ox && cy < oy+oh && cy+ch > oy {
				win.InvalidateRect(c.BodyWidgetHandle(), nil, false)
			}
		}
		dm.InvalidateBody()
	})

	card.SetOnGetWallpaper(dm.WallpaperState.getBitmap)
	card.SetOnIconPress(dm.handleCardIconPress)
	card.SetOnIconRelease(func() {
		dm.DragPressed = false
		dm.SourceCard = nil
		dm.SourceItemIdx = -1
	})
	card.SetOnItemRename(func(oldPath, newName string) {
		dm.commitItemRename(newName, oldPath)
	})
	card.SetOnCollapseToggle(func(name string, collapsed bool) {
		dm.Manager.UpdateGroupCollapsed(name, collapsed)
		dm.InvalidateBody()
	})
}

var refreshCardsCount int

func (dm *DesktopMode) refreshCards() {
	refreshCardsCount++
	logger.Debug("refreshCards #%d: groups=%d cards=%d", refreshCardsCount, len(dm.Manager.GetGroups()), len(dm.Cards))
	groups := dm.Manager.GetGroups()
	for _, card := range dm.Cards {
		win.SendMessage(card.Container().Handle(), win.WM_SETREDRAW, 0, 0)
		win.SendMessage(card.BodyWidgetHandle(), win.WM_SETREDRAW, 0, 0)
	}
	win.SendMessage(dm.BodyWidget.Handle(), win.WM_SETREDRAW, 0, 0)
	groupNames := make(map[string]bool, len(groups))
	for _, g := range groups {
		groupNames[g.Name] = true
	}
	kept := make([]*ui.GroupCard, 0, len(dm.Cards))
	for _, card := range dm.Cards {
		if groupNames[card.GroupName()] {
			card.Refresh()
			kept = append(kept, card)
		} else {
			card.Cleanup()
			card.Container().Dispose()
		}
	}
	dm.Cards = kept
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
	logger.Debug("refreshCards #%d: unfreeze (no force redraw)", refreshCardsCount)
	for _, card := range dm.Cards {
		win.SendMessage(card.Container().Handle(), win.WM_SETREDRAW, 1, 0)
		win.SendMessage(card.BodyWidgetHandle(), win.WM_SETREDRAW, 1, 0)
	}
	win.SendMessage(dm.BodyWidget.Handle(), win.WM_SETREDRAW, 1, 0)
	dm.InvalidateBody()
}

func (dm *DesktopMode) reapplyCardPositions() {
	for i, card := range dm.Cards {
		card.ReapplyBounds()
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
