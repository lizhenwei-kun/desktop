package ui

// DesktopAPI 桌面操作接口定义
type DesktopAPI interface {
	// GetScreenSize 获取屏幕分辨率
	GetScreenSize() (width, height int)
	// GetVirtualScreenSize 获取虚拟屏幕尺寸（多显示器）
	GetVirtualScreenSize() (width, height int)
	// GetWorkAreaRect 获取工作区矩形（排除任务栏）
	GetWorkAreaRect() (left, top, right, bottom int)
	// SetWindowBorderless 移除窗口边框
	SetWindowBorderless(hwnd uintptr)
	// MoveWindow 移动/调整窗口
	MoveWindow(hwnd uintptr, x, y, w, h int)
	// HideTaskbar 隐藏任务栏
	HideTaskbar()
	// ShowTaskbar 显示任务栏
	ShowTaskbar()
}

// DesktopItem 表示桌面项
type DesktopItem struct {
	Path      string // 文件完整路径
	Name      string // 显示名称
	GroupName string // 所属分组名（空表示未分组）
	IsDir     bool   // 是否为文件夹
}
