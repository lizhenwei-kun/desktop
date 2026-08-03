package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

// setupHotkeys 设置快捷键
func (dm *DesktopMode) setupHotkeys() {
	dm.MainWindow.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyF5 {
			dm.refreshDesktop()
			return
		}
		if key == walk.KeyF6 {
			// 检查 Alt 是否按下
			if win.GetKeyState(int32(win.VK_MENU)) < 0 {
				dm.exitDesktopMode()
			}
			return
		}

		// Ctrl+C 复制
		if key == walk.KeyC && walk.ControlDown() {
			if dm.Selected.Path != "" {
				ui.CopyFileToClipboard(dm.Selected.Path)
			}
			return
		}

		// Ctrl+X 剪切
		if key == walk.KeyX && walk.ControlDown() {
			if dm.Selected.Path != "" {
				ui.CutFileToClipboard(dm.Selected.Path)
			}
			return
		}

		// Ctrl+V 粘贴
		if key == walk.KeyV && walk.ControlDown() {
			ui.PasteFromClipboard(0, 0)
			dm.InvalidateBody()
			return
		}

		// Delete 删除到回收站
		if key == walk.KeyDelete {
			if dm.Selected.Path != "" {
				ui.DeleteFileToRecycleBin(dm.Selected.Path)
				dm.Manager.RemoveItem(dm.Selected.Path)
				dm.Selected = ui.Selection{}
				dm.InvalidateBody()
			}
			return
		}
	})
}
