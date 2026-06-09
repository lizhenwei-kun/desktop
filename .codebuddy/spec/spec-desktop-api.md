# 桌面 API (Desktop API)

## 元信息

- **文件**: `internal/desktop/windows.go`, `internal/ui/desktop_api.go`
- **包**: `desktop`, `ui`

## API 接口定义

```go
type DesktopAPI interface {
    GetScreenSize() (width, height int)
    GetVirtualScreenSize() (width, height int)
    GetWorkAreaRect() (left, top, right, bottom int)
    SetWindowBorderless(hwnd uintptr)
    MoveWindow(hwnd uintptr, x, y, w, h int)
    HideTaskbar()
    ShowTaskbar()
}

type DesktopItem struct {
    Path      string
    Name      string
    GroupName string // 所属分组名（空=未分组）
    IsDir     bool
}
```

## WindowsAPI 结构体

**文件**: `internal/desktop/windows.go`

| 方法 | Win32 API | 用途 |
|------|-----------|------|
| `GetScreenSize()` | `GetSystemMetrics(SM_CXSCREEN/CYSCREEN)` | 屏幕分辨率 |
| `GetVirtualScreenSize()` | `GetSystemMetrics(SM_X/ YVIRTUALSCREEN)` | 多显示器虚拟屏幕 |
| `GetWorkAreaRect()` | `SystemParametersInfo(SPI_GETWORKAREA)` | 排除任务栏的工作区 |
| `FindWorkerW()` | `FindWindow/FindWindowEx` | 查找桌面 WorkerW |
| `SetAsDesktopChild(hwnd)` | `SetParent(hwnd, workerW)` | 设为桌面子窗口 |
| `SetWindowBorderless(hwnd)` | 移除 `WS_CAPTION\|WS_THICKFRAME\|WS_BORDER` | 去除所有边框 |
| `RemoveWindowMenu(hwnd)` | `SetMenu(hwnd, NULL)` | 移除菜单栏 |
| `DisableMinimize(hwnd)` | 移除 `WS_MINIMIZEBOX` | 禁用最小化 |
| `SetWindowBottom(hwnd)` | `SetWindowPos(hwnd, HWND_BOTTOM)` | Z序沉底 |
| `SetBorderlessAndPosition(hwnd,...)` | 组合操作 | 去边框+定位 |
| `SetWindowPosNoRedraw(hwnd,...)` | `SWP_NOCOPYBITS\|SWP_NOREDRAW` | 不重绘的尺寸变化 |
| `IsForegroundWindow(hwnd)` | `GetForegroundWindow()` | 检查是否为前台窗口 |
| `ForceShowAndRaise(hwnd)` | `ShowWindow+SetForegroundWindow` | 强制显示并置顶 |
| `HideTaskbar()` / `ShowTaskbar()` | `FindWindow("Shell_TrayWnd") + ShowWindow` | 任务栏显隐 |

## 布局修复协议

去边框后 walk 的 VBox 布局不自动填充新客户区的修复步骤：

1. `SetWindowPosNoRedraw(hwnd, x, y, w+1, h+1)` — 触发 `WM_WINDOWPOSCHANGED`
2. `SetWindowPosNoRedraw(hwnd, x, y, w, h)` — 恢复正确尺寸
3. 手动 `container.SetBoundsPixels(0, 0, workW, fullH)` — 强制 container 铺满
4. 手动 `bodyWidget.SetBoundsPixels(0, 0, workW, fullH)` — 强制 body 铺满
5. 重新 `reapplyCardPositions()` — 恢复卡片绝对定位

## 检查清单

- [ ] 去边框后无白边残留
- [ ] 工作区尺寸正确排除任务栏
- [ ] Z序沉底后不被意外提升
- [ ] 去边框+移除菜单后布局正确填充
