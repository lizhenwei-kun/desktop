# 桌面右键菜单 (Context Menu)

## 元信息

- **文件**: `internal/ui/context_menu.go`
- **包**: `ui`
- **依赖**: `walk`, `win`, `group`, `logger`, `syscall`
- **修改文件**: `internal/ui/desktop_mode.go` (DesktopMode 结构体新增字段 + handleDesktopMouseDown 右键处理)

## 功能概述

实现与 Windows 系统桌面一致的右键菜单系统，包含三类菜单：

| 菜单类型 | 显示时机 | 实现函数 |
|---------|---------|---------|
| 桌面背景菜单 | 右键点击桌面空白处 | `ShowDesktopContextMenu` |
| 图标菜单 | 右键点击未分组桌面图标 | `ShowIconContextMenu` |
| 卡片菜单 | 右键点击分组卡片 | *待实现* |

## DesktopMode 新增字段

```go
type DesktopMode struct {
    // ... 原有字段 ...

    // 右键菜单状态
    isAutoArrange      bool
    isAlignToGrid      bool
    isShowDesktopIcons bool
    sortBy             int

    // 注册表菜单缓存
    cachedDesktopRegItems    []registryShellItem
    cachedDesktopRegCmdStart int
    cachedFileRegItems       []registryShellItem
    cachedFileRegCmdStart    int
}
```

## 桌面背景菜单结构

```
┌─ 查看(&V) ─────────────────┐
│  大图标  ●                  │
│  中图标                      │
│  小图标                      │
│  ─────────────────────────  │
│  ☐ 自动排列图标               │
│  ☑ 将图标与网格对齐           │
│  ─────────────────────────  │
│  ☑ 显示桌面图标               │
├─ 排序方式(&O) ──────────────┤
│  名称  ●                    │
│  大小                        │
│  项目类型                     │
│  修改日期                     │
├─────────────────────────────┤
│  刷新(&E)                    │
├─────────────────────────────┤
│  粘贴(&P)                    │
│  粘贴快捷方式(&S)             │
├─────────────────────────────┤
│  新建(&W)                    │
│  ├─ 文件夹(&F)               │
│  ├─ 快捷方式(&S)              │
│  ├─ ───────────────────     │
│  ├─ 文本文档(&T)              │
│  └─ 位图图像(&B)              │
├─── 注册表扩展菜单项 ──────────┤  ← 来自 HKCR\Directory\Background\shell
├─────────────────────────────┤
│  显示设置(&D)                │
│  个性化(&R)                  │
└─────────────────────────────┘
```

## 图标菜单结构

```
┌─ 打开(&O) ──────────────────┐
│  打开文件所在位置(&I)         │  ← 仅对 .lnk/目录显示
├─────────────────────────────┤
│  发送到(&N)                  │
│  ├─ 桌面快捷方式              │
│  ├─ 邮件收件人                │
│  ├─ ───────────────────     │
│  └─ 来自 SendTo 文件夹的项... │  ← 动态读取
├─────────────────────────────┤
│  剪切(&T)                    │
│  复制(&C)                    │
│  删除(&D)                    │
│  重命名(&M)                  │
├─── 注册表扩展菜单项 ──────────┤  ← 来自 HKCR\*\shell + .ext + AllFSObjects
├─────────────────────────────┤
│  属性(&R)                    │
└─────────────────────────────┘
```

## 核心数据结构

### 注册表菜单项

```go
type registryShellItem struct {
    verb    string // 动词名（如 "open", "print"）
    name    string // 显示名称
    command string // 执行命令（含 %1 占位符）
    isDir   bool   // 是否文件夹路径
    cmdID   int    // 动态分配的命令 ID
}
```

### 命令 ID 分配

| 范围 | 用途 | 说明 |
|------|------|------|
| `0x1001–0x1052` | 内置桌面菜单命令 | 查看、排序、刷新、粘贴、新建、系统设置 |
| `0x2001–0x2031` | 内置图标菜单命令 | 打开、剪切、复制、删除、重命名、属性 |
| `0x3000+` | 桌面注册表动态命令 | 从 `Directory\Background\shell` 读取 |
| `0x4000+` | 文件注册表动态命令 | 从 `*\shell` + `.ext` + 其他路径读取 |
| `0x5000+` | 发送到动态命令 | 从 SendTo 文件夹读取 |

## 注册表 Shell 菜单读取

### 桌面背景菜单读取路径

```
HKCU\Software\Classes\Directory\Background\shell
  └─ 回退：HKCR\Directory\Background\shell
```

### 文件图标菜单读取路径（按优先级）

```
1. HKCU\Software\Classes\*\shell
    └─ 回退：HKCR\*\shell
2. HKCU\Software\Classes\.ext\shell  （扩展名特定）
    └─ 回退：HKCR\.ext\shell
3. 如果是文件夹：
   HKCU\Software\Classes\Directory\shell
    └─ 回退：HKCR\Directory\shell
4. HKCU\Software\Classes\AllFilesystemObjects\shell
    └─ 回退：HKCR\AllFilesystemObjects\shell
```

### 注册表项解析规则

```
verb\
    (Default) = 显示名称（当 MUIVerb 不存在时使用）
    MUIVerb   = 显示名称（支持 @path.dll,-resID 格式）
    Extended  = 存在时表示需要 Shift 键才显示
    command\
        (Default) = 执行命令行（%1=文件路径，%V=目录路径）
```

### MUIVerb 本地化字符串解析

格式: `@C:\Windows\System32\shell32.dll,-21770`

处理流程:
1. 去掉 `@` 前缀
2. 按 `,` 分割为 DLL 路径和资源 ID
3. `LoadLibraryW` 加载 DLL
4. `LoadStringW` 读取资源字符串
5. `FreeLibrary` 释放 DLL

## Win32 API 封装

### 菜单操作

| 函数 | 对应 Win32 API | 说明 |
|------|---------------|------|
| `createPopupMenu` | `CreatePopupMenu` | 创建弹出菜单 |
| `destroyMenu` | `DestroyMenu` | 销毁菜单 |
| `appendMenu` | `AppendMenuW` | 添加菜单项 |
| `appendMenuSeparator` | `AppendMenuW(MF_SEPARATOR)` | 添加分隔线 |
| `trackPopupMenu` | `TrackPopupMenu` | 显示弹出菜单并返回命令 ID |
| `checkMenuItem` | `CheckMenuItem` | 设置勾选状态 |
| `checkMenuRadioItem` | `CheckMenuRadioItem` | 设置单选勾选 |
| `getMenuItemCount` | `GetMenuItemCount` | 获取菜单项数量 |

### 注册表操作

| 函数 | 对应 Win32 API | 说明 |
|------|---------------|------|
| `regOpenKey` | `RegOpenKeyExW` | 打开注册表键 |
| `regCloseKey` | `RegCloseKey` | 关闭注册表键 |
| `regEnumSubKeys` | `RegEnumKeyExW` | 枚举子键 |
| `regQueryStringValue` | `RegQueryValueExW` | 查询字符串值 |

### 剪贴板操作

| 函数 | 对应 Win32 API | 说明 |
|------|---------------|------|
| `openClipboard` | `OpenClipboard` | 打开剪贴板 |
| `closeClipboard` | `CloseClipboard` | 关闭剪贴板 |
| `getClipboardData` | `GetClipboardData` | 获取剪贴板数据 |
| `registerClipboardFormat` | `RegisterClipboardFormatW` | 注册剪贴板格式 |
| `dragQueryFileCount` | `DragQueryFileW` | 查询拖拽文件数 |
| `dragQueryFile` | `DragQueryFileW` | 查询拖拽文件路径 |
| `dragFinish` | `DragFinish` | 释放拖拽内存 |

## 交互流程

### 右键点击桌面

```
handleDesktopMouseDown(x, y, RightButton)
├── ScreenToClient → 转为屏幕坐标
├── 遍历未分组图标:
│   ├── 点击在图标上 → ShowIconContextMenu
│   └── 未点击图标 → ShowDesktopContextMenu
│
├── ShowDesktopContextMenu(hwnd, x, y)
│   ├── CreatePopupMenu
│   ├── 构建内置菜单（查看/排序/刷新/粘贴/新建/显示设置/个性化）
│   ├── readDesktopRegistryMenu() → 添加注册表项
│   ├── TrackPopupMenu(TPM_RETURNCMD)
│   └── handleContextMenuCommand(cmd)
│       ├── cmd < 0x3000 → 内置命令
│       └── cmd >= 0x3000 → executeRegistryCommand()
│
├── ShowIconContextMenu(hwnd, mgr, executor, item, x, y)
│   ├── ComInitThread() (COM 初始化)
│   ├── SHParseDisplayName → 获取 pidl
│   ├── SHBindToParent → 获取 IShellFolder + pidlChild
│   ├── GetUIObjectOf(IID_IContextMenu) → 获取 IContextMenu
│   ├── IContextMenu::QueryContextMenu → 填充 Shell 扩展菜单
│   ├── TrackPopupMenu(TPM_RETURNCMD)
│   └── IContextMenu::InvokeCommand
│       ├── lpDirectory = NULL (0)  ← 关键：必须为 NULL
│       ├── lpVerb = cmd - 1
│       └── nShow = SW_SHOWNORMAL
```

## 执行命令逻辑

### 注册表命令执行

```go
func ExecuteRegistryCommand(cmdLine string, filePath string) {
    // 1. 替换占位符: %1, %L, %V → filePath
    // 2. removeUnresolvedPlaceholders() 移除残留的 %X 占位符
    // 3. 使用 cmd /c 执行整条命令行（正确处理引号、空格路径）
}
```

**重要规则**：
- 必须使用 `cmd /c <cmdLine>` 执行，**不能**用 `strings.Fields` 拆分命令行（会破坏引号包裹的带空格路径）
- 桌面背景菜单调用时，`filePath` 参数应传入 `GetDesktopPath()`（桌面路径），不能传空字符串
- 文件图标菜单调用时，`filePath` 参数应传入文件/文件夹的完整路径

### Shell COM 图标右键菜单（IContextMenu）

`ShowIconContextMenu` 使用 Shell COM 接口 `IContextMenu::InvokeCommand` 执行菜单命令。

**CMINVOKECOMMANDINFO.lpDirectory 规则**：
- **必须设为 NULL (0)**，让 Shell 扩展自行根据 pidl 确定目标路径和工作目录
- 不能传入文件/文件夹路径本身，否则某些 Shell 扩展（如 VS Code）会将其误用为工作目录导致"目录名称无效"错误
- 系统桌面的实现也是传 NULL，由 Shell 内部处理

### 文件操作

| 命令 | 实现方式 |
|------|---------|
| 打开文件 | `executor.Execute(path)` → `cmd start` / 直接启动 exe |
| 打开位置 | `explorer /select,path` |
| 剪切到剪贴板 | `OpenClipboard + GlobalAlloc + SetClipboardData(CF_UNICODETEXT)` |
| 复制到剪贴板 | 同上 |
| 删除到回收站 | PowerShell `Shell.Application.NameSpace(0).ParseName().InvokeVerb('delete')` |
| 重命名 | `ShowInputDialog → os.Rename` |
| 显示属性 | `ShellExecuteW("properties", path)` |
| 创建快捷方式 | PowerShell `WScript.Shell.CreateShortcut` |
| 发送到 | 复制文件/创建快捷方式到目标路径 |

## 交互事件总表

| 操作 | 触发 | 行为 |
|------|------|------|
| 右键桌面空白处 | `handleDesktopMouseDown(Right)` | `ShowDesktopContextMenu` |
| 右键桌面图标 | `handleDesktopMouseDown(Right)` + 命中图标 | `ShowIconContextMenu` |
| 查看 → 大/中/小图标 | 菜单命令 | 调 `desktopIconItemWidth/Height` + 强制重测 |
| 查看 → 自动排列 | 菜单命令 | 切换 `isAutoArrange`，重新网格排列 |
| 查看 → 对齐网格 | 菜单命令 | 切换 `isAlignToGrid` |
| 查看 → 显示桌面图标 | 菜单命令 | 切换 `isShowDesktopIcons`，刷新显示 |
| 排序方式 | 菜单命令 | 切换 `sortBy`，刷新显示 |
| 刷新 | 菜单命令 | `loadWallpaper` + `ReloadDesktopItems` + 重绘 |
| 粘贴 | 菜单命令 | 从剪贴板读取文件列表并复制到桌面 |
| 新建 | 菜单命令 | 在桌面创建对应类型文件 |
| 显示设置/个性化 | 菜单命令 | `start ms-settings:display/personalization` |
| 打开图标 | 菜单命令 | 执行文件关联程序 |
| 剪切/复制/删除/重命名 | 菜单命令 | 对应文件操作 |
| 属性 | 菜单命令 | 打开 Windows 属性对话框 |
| 第三方菜单项 | 菜单命令 | 执行注册表命令（替换 %1 为文件路径） |

## 已知限制

| 限制 | 说明 | 改进方向 |
|------|------|---------|
| ~~不支持 shellex COM 处理器~~ | 已通过 `IContextMenu::QueryContextMenu` 实现 | ✅ 已解决 |
| 不支持级联子菜单 | 注册表 `subcommands` 暂不处理 | 可递归读取子菜单 |
| 排序方式仅影响显示 | 未真正排序 manage r数据 | 扩展 Manager 添加排序支持 |
| 桌面图标显示/隐藏 | 通过 bodyWidget 是否绘制控制 | 可支持 walk container 整体隐藏 |
| 不支持 Extended 动词 | 按住 Shift 的扩展菜单暂不显示 | 可通过 GetKeyState(VK_SHIFT) 检测 |
| SendTo 解析 | 仅读取文件夹，未解析 .lnk 目标 | 可解析每个 .lnk 的真实目标目录 |

## 检查清单

- [ ] 右键桌面空白处显示完整菜单
- [ ] 右键桌面图标显示图标菜单
- [ ] 查看子菜单单选勾选正常
- [ ] 自动排列/对齐网格状态切换正确
- [ ] 粘贴文件到桌面正常工作
- [ ] 新建文件夹/文本文档/位图正确创建
- [ ] 新建文件自动处理名称冲突（自动编号）
- [ ] 显示设置/个性化打开系统面板
- [ ] 图标菜单打开文件正常工作
- [ ] 图标菜单剪切/复制/删除/重命名正常工作
- [ ] 删除文件正确移入回收站
- [ ] 属性对话框正常打开
- [ ] 注册表菜单项正确读取
- [ ] 注册表命令正确执行（%1 替换）
- [ ] MUIVerb 本地化字符串正确解析
- [ ] 菜单不超出屏幕边界
- [ ] 菜单弹出时不会干扰其他交互
- [ ] 发送到子菜单正确列出 SendTo 文件夹内容
