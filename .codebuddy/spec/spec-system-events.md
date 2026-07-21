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

## 已知问题

### 1. refreshDesktop() 触发 ReloadDesktopItems 导致桌面项分组变更

**问题描述**：`refreshDesktop()` 调用 `ReloadDesktopItems()`，而 `ReloadDesktopItems` 会扫描桌面目录并添加新文件。
在旧逻辑中，新文件通过 `groupForPath` 被自动分配到默认分组（如 `.lnk` → "快捷方式"），
导致这些文件从未分组图标区域"消失"，用户以为图标丢失。

**触发路径**：

```
系统事件回调 / healthcheck 父窗口变更
  └── refreshDesktop()
        └── ReloadDesktopItems()
              └── 新文件 → groupForPath → 分配到默认分组
                    └── 不再显示为未分组图标
```

**修复**：新文件默认保持未分组（`gName=""`），用户自行拖入分组。
见 `internal/group/manager.go` `ReloadDesktopItems()` 第 673-678 行。

**影响范围**：
- 系统事件回调（电源恢复、显示变更）触发 `refreshDesktop()` 时
- healthcheck 检测到父窗口变更触发 `ReapplyCardPositionsAndRefresh()` 时
- 程序启动 `NewRunner()` 首次调用 `ReloadDesktopItems()` 时

### 2. healthcheck 频繁 re-embedding

**问题描述**：`FindShellWorkerW()` 中 `SendMessage 0x052C` 会触发 WorkerW 重建，
导致每次 healthcheck 都检测到 `parent != shellWorkerW`，触发 `SetAsDesktopChild` 重新嵌入。
日志显示 8 小时内执行了约 960 次 re-embedding。

**影响**：WorkerW 频繁重建可能导致 Explorer 不稳定。

**建议优化**：
- 检测到 parent 变更后，比较 WorkerW 句柄是否真的变化了（缓存上次的 shellWorkerW）
- 如果 WorkerW 没有变化但 parent 变了，可能是 0x052C 消息本身导致了重建，应减少检测频率或增加去抖
