package desktop

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// setupHotkeys 设置快捷键
func (dm *DesktopMode) setupHotkeys() {
	// Alt+F6 退出全屏模式
	dm.MainWindow.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyF6 {
			// 检查 Alt 是否按下
			if win.GetKeyState(int32(win.VK_MENU)) < 0 {
				dm.exitDesktopMode()
			}
		}
	})
}
