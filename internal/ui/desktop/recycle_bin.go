package desktop

import (
	"unsafe"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// RecycleBinState 回收站状态
type RecycleBinState struct {
	NonEmpty    bool // 回收站是否非空
	lastNonEmpty bool // 前一次检测结果，用于判断状态变化
}

// recycleBinCheckInterval 回收站检测间隔（毫秒）
const recycleBinCheckInterval = 5000

// initRecycleBinMonitor 初始化回收站状态定时监测
// 每 2 秒检测回收站是否非空，状态变化时通知 UI 主线程更新图标
func (dm *DesktopMode) initRecycleBinMonitor() {
	// 先做一次初始检测
	dm.doRecycleBinCheck()

	// 添加定时器，每 2 秒检测一次
	dm.Work.AddTimer(recycleBinCheckInterval, func() {
		dm.doRecycleBinCheck()
	})
}

// doRecycleBinCheck 检测回收站状态，如果与上次不同则通知 UI 更新
// 注意：在 strand goroutine 中调用，通过局部变量检测，最后 Post 到 UI 主线程更新状态
func (dm *DesktopMode) doRecycleBinCheck() {
	nonEmpty := dm.queryRecycleBinNonEmpty()

	// 通过 dm.Post 投递到 UI 主线程做状态比较和更新，避免跨线程访问 RecycleBinState
	dm.Post(func() {
		if nonEmpty == dm.RecycleBinState.lastNonEmpty {
			return // 状态无变化，跳过
		}

		dm.RecycleBinState.lastNonEmpty = nonEmpty
		dm.RecycleBinState.NonEmpty = nonEmpty

		logger.Debug("recycleBin: nonEmpty=%v", nonEmpty)
		dm.updateRecycleBinIcon()
	})
}

// queryRecycleBinNonEmpty 查询回收站是否非空
func (dm *DesktopMode) queryRecycleBinNonEmpty() bool {
	var state ui.SHQUERYRBINFO
	state.CbSize = uint32(unsafe.Sizeof(state))
	ui.ProcSHQueryRecycleBinW.Call(0, uintptr(unsafe.Pointer(&state)))
	return state.II64Size > 0 || state.II64NumItems > 0
}

// updateRecycleBinIcon 更新回收站图标
// 清除缓存的回收站图标 bitmap，下次绘制时自动重新提取
func (dm *DesktopMode) updateRecycleBinIcon() {
	shellPath := "shell:RecycleBinFolder"

	// 清除 bitmap 缓存，下次绘制时使用新图标
	ui.GlobalIconBmpCache.Remove(shellPath)

	logger.Debug("recycleBin: nonEmpty=%v, icon cache cleared", dm.RecycleBinState.NonEmpty)

	// 重绘桌面（窗口不可见时自动跳过）
	dm.InvalidateBody()
}
