# 进程间拖放 (Cross-Process Drag & Drop)

## 元信息

- **文件**: `internal/ui/drag_drop.go`
- **包**: `ui`
- **依赖**: `walk`, `win`, `syscall`, `unsafe`, `group`, `logger`, `ole32`, `shell32`

## 功能概述

实现与 Windows 系统一致的进程间文件拖放功能，支持两种方向：

| 方向 | 场景 | 技术方案 |
|------|------|---------|
| 外部 → 应用 (Drop) | 从桌面/资源管理器拖文件到本应用的桌面或卡片 | `DragAcceptFiles` + `WM_DROPFILES` |
| 应用 → 外部 (Drag) | 从本应用拖图标到桌面/资源管理器/其他程序 | OLE `DoDragDrop` + `IDataObject` |

## 外部拖入 (External Drop)

### 技术方案

使用 Windows Shell 拖放协议 (`WM_DROPFILES`)：
1. 在 `BodyWidget` 窗口上调用 `DragAcceptFiles(hwnd, TRUE)` 注册为拖放目标
2. 子类化 `BodyWidget` 窗口，拦截 `WM_DROPFILES` 消息
3. 从 `WM_DROPFILES` 的 `wParam` 中获取 `HDROP` 句柄
4. 使用 `DragQueryFileW` 枚举被拖拽的文件列表
5. 使用 `DragFinish` 释放资源
6. 将文件添加到当前鼠标位置的卡片或桌面

### WM_DROPFILES 消息处理流程

```
BodyWidget 子类化回调收到 WM_DROPFILES
├── HDROP hDrop = (HDROP)wParam
├── count = DragQueryFileW(hDrop, 0xFFFFFFFF, nil, 0)
├── for i = 0; i < count; i++:
│   ├── DragQueryFileW(hDrop, i, buf, MAX_PATH)
│   └── files[i] = UTF16ToString(buf)
├── DragFinish(hDrop)
├── 确定鼠标释放位置 (GetMessagePos 或 DM_DROPFILES 中的鼠标坐标)
├── 判断目标区域:
│   ├── 在卡片上 → 添加到该卡片分组
│   └── 在桌面空白 → 添加到未分组桌面
└── 刷新 UI
```

### 关键 API

| API | 用途 | DLL |
|-----|------|-----|
| `DragAcceptFiles(hwnd, TRUE/FALSE)` | 注册/注销窗口接收拖放 | shell32.dll |
| `DragQueryFileW(hDrop, i, buf, size)` | 获取文件数量或文件路径 | shell32.dll |
| `DragFinish(hDrop)` | 释放拖放资源 | shell32.dll |

### 窗口子类化

由于 walk 框架不直接暴露 `WM_DROPFILES` 事件，需要通过 `SetWindowSubclass` 对 `BodyWidget` 窗口进行子类化来拦截消息。

**子类化回调处理**：
```go
func dropFilesSubclassProc(hwnd, msg, wParam, lParam, uIDSubclass, dwRefData uintptr) uintptr {
    if msg == WM_DROPFILES {
        handleDropFiles(win.HDROP(wParam))
        return 0  // 消息已处理
    }
    ret, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
    return ret
}
```

## 应用拖出 (External Drag)

### 技术方案

使用 OLE (COM) 拖放协议 `DoDragDrop`：
1. 当用户将图标拖拽到应用窗口外部时，启动 OLE 拖放
2. 实现 `IDataObject` COM 接口，提供 `CF_HDROP` 格式的数据
3. 调用 `Ole32!DoDragDrop` 开始拖放操作
4. 拖放完成后（成功/取消），清理资源

### 拖出触发条件

在现有 `handleIconDrop` / `dragDrop` 逻辑中增加检测：
- 当图标拖拽到 `BodyWidget` 客户区外部时（鼠标超出窗口边界）
- 此时不应执行内部拖放逻辑，而是启动 OLE 拖出

**检测方式**：在 `MouseMove` 中判断屏幕坐标是否仍在窗口客户区内。

### IDataObject 实现

```go
type dropSourceDataObject struct {
    refCount uint32
    files    []string
    hGlobal  win.HGLOBAL
}
```

实现 `IDataObject` 的以下方法：
- `QueryInterface` → 支持 `IID_IDataObject`
- `AddRef` / `Release` → 引用计数
- `GetData` → 返回 `CF_HDROP` 格式的文件列表
- `EnumFormatEtc` → 返回支持的格式枚举
- 其他方法返回 `E_NOTIMPL`

### CF_HDROP 数据结构

```
DROPFILES 结构体 (在全局内存中):
├── pFiles (DWORD) = offset 到文件列表的偏移
├── pt (POINT) = 拖放位置（屏幕坐标）
├── fNC (BOOL) = 是否客户区坐标
├── fWide (BOOL) = 是否 Unicode
├── 文件列表 (以 \0 分隔，双 \0 结尾)
```

## DesktopMode 新增字段

```go
type DesktopMode struct {
    // ... 原有字段 ...

    // 外部拖放状态
    DropFilesHooked      bool            // 是否已注册 DragAcceptFiles
    DropFilesHwnd        win.HWND        // 被子类化的窗口句柄
    IsExternalDragging   bool            // 是否正在对外拖出
    ExternalDragPath     string          // 当前对外拖出的文件路径
    ExternalDropActive   bool            // 是否正在接收外部拖入（DragEnter 状态）
    ExternalDropFiles    []string        // 外部拖入的文件列表
    ExternalDropPos      win.POINT       // 外部拖入的鼠标位置
}
```

## DesktopSetup 修改

在 `Setup()` 中增加：
```go
// 在创建 BodyWidget 之后，注册拖放
dm.registerExternalDropTarget()
```

在 `exitDesktopMode()` 中增加：
```go
// 注销拖放
dm.unregisterExternalDropTarget()
```

## 交互流程

### 从桌面拖文件到应用

```
1. 用户从桌面/资源管理器选择文件
2. 鼠标拖入 BodyWidget 客户区
3. BodyWidget 子类化收到 WM_DROPFILES
4. 解析文件列表
5. 获取鼠标释放位置
6. 判断目标区域（卡片/桌面）
7. 添加到对应位置
8. 刷新 UI
```

### 从应用拖图标到桌面

```
1. 用户在应用内开始拖拽图标（现有内部拖拽逻辑）
2. 鼠标拖出 BodyWidget 客户区边界
3. 进入"对外拖出"模式
4. 销毁内部幽灵窗口
5. 创建 IDataObject（含 CF_HDROP 格式文件列表）
6. 调用 DoDragDrop 开始 OLE 拖放
7. 释放到外部（桌面/资源管理器）→ 文件复制/移动
8. 取消拖放 → 清理状态
9. 完成后清理 COM 资源
```

## 已知限制

| 限制 | 说明 | 改进方向 |
|------|------|---------|
| 只支持文件拖放 | 不支持文本/图片等其他格式 | 扩展 IDataObject 支持更多格式 |
| OLE 拖出需 COM 初始化 | 需要在 UI 线程初始化 COM | 确保 `OleInitialize` 已调用 |
| 拖出操作是复制 | 从应用拖到桌面是复制文件，不是移动 | 可检测 Ctrl/Shift 键状态 |
| 拖入不支持文件夹递归 | 仅处理顶级文件 | 可选递归扫描文件夹 |

## 检查清单

- [ ] 从桌面/资源管理器拖文件到桌面空白区域，文件正确添加到未分组
- [ ] 从桌面/资源管理器拖文件到分组卡片，文件正确添加到该分组
- [ ] 拖入时鼠标位置正确映射到客户区坐标
- [ ] 多文件同时拖入正常工作
- [ ] 从应用拖图标到桌面，在桌面生成文件（复制/快捷方式）
- [ ] 从应用拖图标到资源管理器，文件正确复制到目标目录
- [ ] 拖出取消时正确清理状态
- [ ] 退出应用时正确注销拖放注册
- [ ] 多次进出拖放不会导致内存泄漏
