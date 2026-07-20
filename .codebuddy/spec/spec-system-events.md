# 系统事件恢复与自检 (System Events & Health Check)

## 元信息

- **文件**: `internal/desktop/windows.go`, `internal/ui/desktop/desktop_setup.go`, `internal/app/runner.go`
- **包**: `desktop`, `desktop`(ui), `app`

## 概述

长时间不操作系统后可能出现的异常：
1. 系统睡眠/休眠恢复后，窗口在 WorkerW 中的状态失效（其他应用打不开、桌面图标不显示）
2. 显示器关闭/开启后绘制中断
3. 窗口父窗口被意外改变

解决方案：监听系统事件 + 定时自检恢复。

## 系统事件监听

### 常量定义

**文件**: `internal/desktop/windows.go`

| 常量 | 值 | 用途 |
|------|-----|------|
| `WM_POWERBROADCAST` | 0x0218 | 系统电源状态变更（睡眠/恢复） |
| `PBT_APMRESUMEAUTOMATIC` | 0x0012 | 系统从睡眠自动恢复 |
| `PBT_APMRESUMESUSPEND` | 0x0007 | 系统从休眠恢复 |
| `WM_DISPLAYCHANGE` | 0x007E | 显示分辨率/状态变更 |

### 回调注册

```go
// SetOnSystemEvent 设置系统事件回调（电源恢复、显示变更等触发）
func (api *WindowsAPI) SetOnSystemEvent(fn func())
```

### 子类化扩展

`subclassProc` 在原有拦截 `WM_SYSCOMMAND` + `SC_MINIMIZE` 的基础上，增加：

```
WM_POWERBROADCAST → wParam==PBT_APMRESUMEAUTOMATIC/PBT_APMRESUMESUSPEND
    └── systemEventCallback() → dm.Post → dm.refreshDesktop()

WM_DISPLAYCHANGE → 显示变更
    └── systemEventCallback() → dm.Post → dm.refreshDesktop()
```

### 注册时机

在 `delayedSetup()` 中窗口嵌入桌面完成后注册：

```go
dm.WinAPI.SetOnSystemEvent(func() {
    dm.Post(func() {
        logger.Debug("system event: refreshing desktop")
        dm.refreshDesktop()
    })
})
```

## 定时自检恢复

### 实现位置

**文件**: `internal/app/runner.go` — `startTimer()`

### 自检周期

每 **30 秒**执行一次（仅桌面模式）。

### 自检项

| 检测项 | 检测方式 | 恢复动作 |
|--------|---------|---------|
| 窗口不可见 | `IsWindowVisible(hwnd)` 返回 false | `SetVisible(true)` + `MoveWindow` + `ReapplyCardPositionsAndRefresh()` |
| 父窗口变更 | `GetParent(hwnd)` 不等于 `FindShellWorkerW()` | `SetAsDesktopChild` 重新嵌入 + `MoveWindow` + `ReapplyCardPositionsAndRefresh()` |

## 0x052C 消息优化

`FindShellWorkerW()` 中向 Progman 发送 `0x052C` 消息的超时从 **1000ms** 降至 **500ms**，减少系统卡死风险。

## 关键方法

| 方法 | 文件 | 用途 |
|------|------|------|
| `refreshDesktop()` | `desktop_menu.go` | 整体刷新：同步桌面项 + 加载壁纸 + 重建卡片 + 预加载图标缓存 |
| `ReapplyCardPositionsAndRefresh()` | `desktop_paint.go` | 强制完全重绘：重新应用卡片位置 + InvalidateRect + SendWMSize + UpdateWindow |
| `SetOnSystemEvent(fn)` | `windows.go` | 注册系统事件回调 |
| `IsWindowVisible(hwnd)` | `windows.go` | 检测窗口可见性 |
| `FindShellWorkerW()` | `windows.go` | 查找当前桌面 WorkerW 句柄 |

## 检查清单

- [ ] 系统睡眠/休眠恢复后桌面自动刷新
- [ ] 显示分辨率变更后自动调整布局
- [ ] 定时自检发现窗口不可见时自动恢复
- [ ] 定时自检发现父窗口变更时重新嵌入
- [ ] 0x052C 消息超时合理，不会导致卡死
