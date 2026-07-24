# 桌面模式 (Desktop Mode)

## 元信息

- **文件**: `internal/ui/desktop_mode.go`
- **包**: `ui`
- **依赖**: `walk`, `win`, `config`, `desktop`, `group`, `logger`

## 全局原则

### 1. 能用 Windows API 的尽量用

所有功能优先使用 Windows 原生 API（`user32.dll`, `gdi32.dll`, `shell32.dll` 等），避免在 Go 层重复实现已有系统功能。例如：
- 窗口操作：`SetWindowPos`, `MoveWindow`, `SetParent`
- 壁纸路径：`SystemParametersInfoW`
- 图标提取：`SHGetImageList`, `SHGetFileInfoW`
- 右键菜单：COM `IContextMenu`

## 核心类型

```go
type DesktopMode struct {
    mainWindow   *walk.MainWindow
    container    *walk.Composite    // 无布局容器，用于卡片绝对定位
    manager      *group.Manager
    executor     *ProgramExecutor
    winAPI       *desktop.WindowsAPI
    lifecycle    *LifecycleManager
    cards        []*GroupCard
    bodyWidget   *walk.CustomWidget // 主绘制区域（背景+壁纸+工具栏）
    screenW      int
    screenH      int
    workX, workY int
    workW, workH int
    wallpaperBmp *walk.Bitmap
}
```

## UI 组件层次

```
MainWindow (VBox)
└── Composite (container, VBox, margins=0)
    └── CustomWidget (bodyWidget, stretch=1)
        ├── paintBackground → 深色 #1A1A2E
        ├── paintWallpaper → 系统壁纸
        ├── paintToolbar → "+ 添加卡片" 按钮
        └── paintFreeItems → 未分组的桌面图标
    ├── GroupCard × N (绝对定位，非 VBox 管理)
    │   └── CustomWidget (bodyWidget)
    │       ├── paintBackground → 半透明颜色
    │       ├── paintHeader → 标题 + 分隔线
    │       └── paintIconGrid → 图标磁贴网格
```

## 完整流程

```
Setup()
├── 设置主窗口尺寸为工作区
├── 设置背景颜色 #1A1A2E
├── loadWallpaper()
├── 创建 container (VBox, margins=0)
├── 创建 bodyWidget (CustomWidget, stretch=1)
├── container.SizeChanged → 50ms后 reapplyCardPositions
├── setupHotkeys (Alt+F6 退出)
├── bodyWidget.MouseDown → handleMouseDown
├── createGroupCards()
└── go delayedSetup()

delayedSetup()
├── 等待300ms
├── Synchronize:
│   ├── HideDesktopIcons (隐藏系统桌面图标)
│   ├── RemoveWindowMenu
│   ├── SetBorderlessAndPosition
│   ├── DisableMinimize
│   ├── InstallMinimizeBlock (子类化拦截最小化)
│   └── SetWindowBottom
├── 等待100ms
├── Synchronize: SetWindowPosNoRedraw(w+1, h+1)
├── 等待50ms
├── Synchronize:
│   ├── SetWindowPosNoRedraw(w, h)
│   └── SetWindowBottom
├── 等待100ms
├── Synchronize:
│   ├── container.SetBoundsPixels 铺满客户区
│   ├── bodyWidget.SetBoundsPixels 铺满客户区
│   ├── reapplyCardPositions
│   ├── bodyWidget.Invalidate
│   ├── lifecycle.MarkReady()
│   └── go maintainBottomZOrder()
```

## Z 序守护

- 每 500ms 轮询一次
- 检测 IsIconic → SW_RESTORE + SetWindowBottom
- 检测 !IsWindowVisible → SW_SHOWNA + SetWindowBottom
- 检测 IsForegroundWindow → SetWindowBottom 推底
- 用户打开其他程序时不做任何操作

## 绘制机制（⚠️ CustomWidget 的 paint 回调必须每次都绘制）

`bodyWidget` 是 `walk.NewCustomWidgetPixels(container, 0, paintDesktop)`。

**关键注意事项（双击打开程序后未分组图标消失的根因）**：

- `paintDesktop` 的 paint 回调执行**前**，walk 框架已经擦除了客户区（背景被清空）。
- 因此 **不能**因为"非脏"就 `return nil` 跳过绘制——跳过会导致整个桌面（壁纸 + 未分组图标 + 卡片）被清空，下次 WM_PAINT（如双击打开程序使本窗口失去焦点触发重绘）时内容消失。
- `paintDesktop` 必须**每次 WM_PAINT 都执行全量绘制**（背景→壁纸→工具栏→图标），即使没有数据变更。
- 原 `paintDirty` 脏标记曾用于控制"是否跳过绘制"，该逻辑会导致图标消失，已移除。`paintDirty` 现在仅作标记字段，不再控制跳过。
- 背景跳动问题由壁纸 1:1 物理像素加载 + 加载并发锁解决，去掉跳过绘制不会复现跳动。
- `refreshDesktopPending` 的 debounce 仍保留，用于防止连续刷新时的并发 race（壁纸加载 goroutine 与 UI 重绘）。

## 桌面图标遮挡方案

- `HideDesktopIcons()`: 隐藏包含 SHELLDLL_DefView 的父窗口，防止系统图标显示在应用窗口上层
- `ShowDesktopIcons()`: 退出时恢复系统桌面图标

## Win+D 防护方案

- 使用 `SetWindowSubclass` (comctl32.dll) 对本窗口子类化
- 子类化回调拦截 `WM_SYSCOMMAND` + `SC_MINIMIZE`，返回 0 忽略最小化
- 仅影响本窗口，不影响其他程序对 Win+D 的正常响应
- 退出时 `RemoveWindowSubclass` 恢复

## 交互事件

| 操作 | 触发 | 行为 |
|------|------|------|
| 点击 "+ 添加卡片" | 鼠标按下 | 弹出输入对话框 → CreateGroup → refreshCards |
| 点击未分组图标 | 鼠标按下 | 执行程序/打开文件 |
| Alt+F6 | 键盘 | exitDesktopMode → lifecycle关闭 → Close |

## 退出清理

```
exitDesktopMode()
├── lifecycle.MarkClosing()
├── lifecycle.ExecuteCleanups()
├── RemoveMinimizeBlock (卸载子类化)
├── ShowDesktopIcons (恢复系统桌面图标)
├── ShowTaskbar
└── mainWindow.Close()
```

## 检查清单

- [ ] 去边框后客户区正确铺满，无白边
- [ ] 壁纸直接按工作区尺寸 1:1 加载（见 spec-wallpaper），绘制无缩放无跳动
- [ ] `paintDesktop` 每次 WM_PAINT 都执行全量绘制，**禁止**非脏时跳过（否则图标消失）
- [ ] 隐藏→显示时先设 bounds + SetPaintDirty，再 MoveWindow
- [ ] Z序守护不会干扰用户打开其他程序
- [ ] Alt+F6 正确退出桌面模式
- [ ] 添加/删除分组后 UI 正确刷新
- [ ] 系统桌面图标被隐藏，不会遮挡应用
- [ ] Win+D 不会最小化本窗口，不影响其他程序
