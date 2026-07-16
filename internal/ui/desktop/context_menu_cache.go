package desktop

import (
	"time"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

// contextMenuCacheInterval 右键菜单缓存刷新间隔（秒）
const contextMenuCacheInterval = 30

// initContextMenuCache 初始化右键菜单缓存定时更新
// 每 30 秒从注册表读取桌面和文件图标的 Shell 菜单项，并通过 COM IContextMenu
// 获取图标右键菜单（含注册表项 + COM 扩展处理器，如 7-Zip、VS Code 等），
// 缓存到 ContextMenuState 中，避免每次右键时才去读取。
// 所有数据读取在 strand goroutine 中异步执行，不阻塞 UI 主线程。
func (dm *DesktopMode) initContextMenuCache() {
	// 初始加载通过 Post 投递到 strand goroutine 执行，避免阻塞 UI 主线程
	dm.Work.Post(func() {
		dm.refreshContextMenuCacheAsync()
	})

	// 添加定时器，每 30 秒刷新一次
	dm.Work.AddTimer(contextMenuCacheInterval*1000, func() {
		dm.refreshContextMenuCacheAsync()
	})
}

// refreshContextMenuCacheAsync 异步刷新右键菜单缓存（在 strand goroutine 中调用）
// 先读取数据，再投递到 UI 主线程赋值，避免跨线程访问 ContextMenuState
func (dm *DesktopMode) refreshContextMenuCacheAsync() {
	// 在 strand goroutine 中执行数据读取（注册表/COM 调用可能阻塞）
	desktopItems := ui.ReadDesktopRegistryMenu()
	fileItems := ui.ReadFileRegistryMenu("")
	iconItems, iconOK := ui.QueryIconMenuItems()

	// 投递到 UI 主线程赋值
	dm.Post(func() {
		if desktopItems != nil {
			dm.ContextMenuState.CachedDesktopRegItems = desktopItems
			dm.ContextMenuState.CachedDesktopRegCmdStart = ui.MaxCmdIDDynamic
			logger.Debug("contextMenuCache: refreshed %d desktop registry items", len(desktopItems))
		}

		if fileItems != nil {
			dm.ContextMenuState.CachedFileRegItems = fileItems
			dm.ContextMenuState.CachedFileRegCmdStart = 0x4000
			logger.Debug("contextMenuCache: refreshed %d file registry items", len(fileItems))
		}

		if iconOK && len(iconItems) > 0 {
			dm.ContextMenuState.CachedIconMenuItems = iconItems
			dm.ContextMenuState.CachedIconMenuCmdStart = 0x5000
			logger.Debug("contextMenuCache: refreshed %d icon COM menu items", len(iconItems))
		}

		dm.ContextMenuState.registryCacheTime = time.Now()
	})
}
