package desktop

import (
	"syscall"

	"github.com/lxn/win"

	"desktop_go/internal/ui"
)

const rclickSubclassID = 2

var (
	comctl32DLL              = syscall.NewLazyDLL("comctl32.dll")
	procSetWindowSubclass    = comctl32DLL.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32DLL.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32DLL.NewProc("DefSubclassProc")
)

func (dm *DesktopMode) installRightClickHandler() {
	hwnd := dm.BodyWidget.Handle()
	if hwnd == 0 {
		return
	}
	dm.RClickCB = syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
		if msg == win.WM_RBUTTONDOWN {
			x := int(win.GET_X_LPARAM(lParam))
			y := int(win.GET_Y_LPARAM(lParam))
			var pt win.POINT
			pt.X = int32(x)
			pt.Y = int32(y)
			win.ClientToScreen(win.HWND(hwnd), &pt)
			screenX := int(pt.X)
			screenY := int(pt.Y)
			items := dm.Manager.GetUngroupedItems()
			hitIcon := false
			for i, item := range items {
				ix, iy := dm.getFreeItemPixelPos(item.Path, i)
				if x >= ix && x <= ix+ui.TileWidth() &&
					y >= iy && y <= iy+ui.TileHeight() {
					hitIcon = true
					dm.ShowIconContextMenu(dm.MainWindow.Handle(), dm.Manager, dm.Executor, item, screenX, screenY)
					break
				}
			}
			if !hitIcon {
				dm.ShowDesktopContextMenu(dm.MainWindow.Handle(), screenX, screenY)
			}
			return 0
		}
		ret, _, _ := procDefSubclassProc.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	})
	procSetWindowSubclass.Call(
		uintptr(hwnd),
		dm.RClickCB,
		rclickSubclassID,
		0,
	)
}

func (dm *DesktopMode) uninstallRightClickHandler() {
	if dm.RClickCB == 0 {
		return
	}
	hwnd := dm.BodyWidget.Handle()
	if hwnd == 0 {
		return
	}
	procRemoveWindowSubclass.Call(
		uintptr(hwnd),
		dm.RClickCB,
		rclickSubclassID,
	)
}
