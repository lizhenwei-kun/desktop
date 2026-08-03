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
		card.SetSelectionProvider(dm)
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
		dm.selectItem(item.Path)
	})
	card.SetOnIconRightClick(func(_ *ui.GroupCard, _ int, item group.GroupItem, screenX, screenY int) {
		showIconContextMenuReal(dm.MainWindow.Handle(), dm.Executor, item, screenX, screenY)
	})
	card.SetOnCardBodyClick(func() {
		dm.clearSelectedItem()
	})
	card.SetOnCardClicked(func(c *ui.GroupCard) {
		dm.bringCardToFrontIfOverlap(c)
	})

	card.SetOnCardDragOutline(func(c *ui.GroupCard, newX, newY int) {
		dm.CardDragOutline.OnCardDragOutlineEx(c, newX, newY, dm.Cards)
	})
	card.SetOnCardDragOutlineEnd(func(card *ui.GroupCard) {
		// 用卡片最新的拖拽位置作为吸附基准（DragOutlineX/Y 因 500ms 检测间隔可能滞后）
		snapX, snapY := dm.CardDragOutline.SnapPosition(card, dm.Cards, card.DragPosX(), card.DragPosY())
		card.SetDragNewPos(snapX, snapY)
		dm.CardDragOutline.OnCardDragOutlineEnd(card)
		cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
		logger.Debug("dragEnd: card=%q snap=(%d,%d) pos=(%d,%d,%dx%d) totalCards=%d",
			card.GroupName(), snapX, snapY, cx, cy, cw, ch, len(dm.Cards))
		dm.redrawCardsOverlappingWith(card)
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
		// 以卡片当前颜色（仅 RGB）作为初始选中色，其余为预设色
		preset := append([]string{ui.ColorToHexRGB(card.GroupColor())}, ui.PresetColors...)
		colorStr, ok := ui.ShowColorDialog(dm.MainWindow, "修改颜色", preset)
		if ok && colorStr != "" {
			// 透明度统一采用新建卡片默认色的 Alpha，忽略对话框返回的透明度
			c := ui.ParseHexColor(colorStr)
			c.A = ui.DefaultCardColorAlpha
			colorStr = ui.ColorToHex(c)
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
	card.SetOnResizeOutline(func(c *ui.GroupCard, newX, newY, newW, newH int) {
		dm.ResizeOutlineState.OnCardResizeOutlineEx(c, newX, newY, newW, newH, dm.Cards)
	})
	card.SetOnResizeOutlineEnd(func(card *ui.GroupCard) {
		// 缩放释放时右下角吸附（endResize 回调后应用 resizeNewX/Y/W/H）
		sx, sy, sw, sh := dm.ResizeOutlineState.SnapResizePosition(card, dm.Cards,
			card.ResizeNewX(), card.ResizeNewY(), card.ResizeNewW(), card.ResizeNewH())
		card.SetResizeNewPos(sx, sy, sw, sh)
		dm.ResizeOutlineState.OnCardResizeOutlineEnd(card)
		cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
		logger.Debug("resizeEnd: card=%q snap=(%d,%d,%dx%d) pos=(%d,%d,%dx%d) totalCards=%d",
			card.GroupName(), sx, sy, sw, sh, cx, cy, cw, ch, len(dm.Cards))
		dm.redrawCardsOverlappingWith(card)
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
	// 收缩前收集与当前卡片有交集的其它卡片到重绘队列
	card.SetOnCollapseStart(func(c *ui.GroupCard) {
		dm.collectOverlappingCards(c)
	})
	card.SetOnCollapseToggle(func(name string, collapsed bool) {
		logger.Debug("collapseToggle: name=%q collapsed=%v", name, collapsed)
		dm.Manager.UpdateGroupCollapsed(name, collapsed)
		if collapsed {
			// 收缩结束后统一重绘之前收集的、原本被覆盖的卡片
			dm.flushRedrawQueue()
		}
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

// bringCardToFrontIfOverlap 当点击的卡片与其它卡片存在交集时，将当前卡片置顶（z 序最上）
func (dm *DesktopMode) bringCardToFrontIfOverlap(card *ui.GroupCard) {
	for _, c := range dm.Cards {
		if c == card {
			continue
		}
		if card.Overlaps(c) {
			win.SetWindowPos(card.Container().Handle(), win.HWND_TOP, 0, 0, 0, 0,
				win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
			return
		}
	}
}

// redrawCardsOverlappingWith 重绘与指定卡片有交集的其它卡片
func (dm *DesktopMode) redrawCardsOverlappingWith(card *ui.GroupCard) {
	cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
	if card.IsCollapsed() {
		ch = ui.CardHeaderHeight + 4
	}
	logger.Debug("redrawCardsOverlappingWith: card=%q bounds=(%d,%d,%dx%d), checking %d other cards",
		card.GroupName(), cx, cy, cw, ch, len(dm.Cards)-1)
	for _, c := range dm.Cards {
		if c == card {
			continue
		}
		ox, oy, ow, oh := c.PixelX(), c.PixelY(), c.PixelW(), c.PixelH()
		if c.IsCollapsed() {
			oh = ui.CardHeaderHeight + 4
		}
		overlap := card.Overlaps(c)
		logger.Debug("redrawCardsOverlappingWith: %q(%d,%d,%dx%d) vs %q(%d,%d,%dx%d) overlap=%v",
			card.GroupName(), cx, cy, cw, ch, c.GroupName(), ox, oy, ow, oh, overlap)
		if overlap {
			win.InvalidateRect(c.BodyWidgetHandle(), nil, false)
		}
	}
}

// collectOverlappingCards 收缩前调用：按当前卡片的完整尺寸判断，
// 把与它有交集的其它卡片收集到重绘队列，待收缩结束后统一重绘。
func (dm *DesktopMode) collectOverlappingCards(card *ui.GroupCard) {
	// 每次收集前先清空队列，避免上一次流程异常退出时残留旧卡片被重复重绘
	dm.redrawQueue = dm.redrawQueue[:0]
	cx, cy, cw, ch := card.PixelX(), card.PixelY(), card.PixelW(), card.PixelH()
	logger.Debug("collectOverlappingCards: card=%q fullBounds=(%d,%d,%dx%d), checking %d other cards",
		card.GroupName(), cx, cy, cw, ch, len(dm.Cards)-1)
	for _, c := range dm.Cards {
		if c == card {
			continue
		}
		ox, oy, ow, oh := c.PixelX(), c.PixelY(), c.PixelW(), c.PixelH()
		if c.IsCollapsed() {
			oh = ui.CardHeaderHeight + 4
		}
		overlap := cx < ox+ow && cx+cw > ox && cy < oy+oh && cy+ch > oy
		logger.Debug("collectOverlappingCards: %q(%d,%d,%dx%d) vs %q(%d,%d,%dx%d) overlap=%v",
			card.GroupName(), cx, cy, cw, ch, c.GroupName(), ox, oy, ow, oh, overlap)
		if overlap {
			dm.redrawQueue = append(dm.redrawQueue, c)
		}
	}
}

// flushRedrawQueue 统一重绘队列中收集的卡片，然后清空队列。
func (dm *DesktopMode) flushRedrawQueue() {
	for _, c := range dm.redrawQueue {
		win.InvalidateRect(c.BodyWidgetHandle(), nil, false)
	}
	dm.redrawQueue = dm.redrawQueue[:0]
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
