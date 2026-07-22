# 对话框 (Dialogs)

## 元信息

- **文件**: `internal/ui/dialogs.go`
- **包**: `ui`
- **依赖**: `github.com/lxn/walk`, `github.com/lxn/walk/declarative`, `github.com/lxn/win`, `syscall`, `unsafe`
- **外部 DLL**: `comdlg32.dll` (ChooseColorW)

## API

### ShowInputDialog

```go
func ShowInputDialog(owner walk.Form, title, label, defaultValue string) (string, bool)
```

**UI 布局**:
```
Dialog (300×120)
├── Label (提示文字)
├── LineEdit (输入框，默认值)
└── Composite (HBox)
    ├── HSpacer
    ├── PushButton "确定" → Accept
    └── PushButton "取消" → Cancel
```

**返回值**: `(输入文本, 是否确认)`

### ShowConfirmDialog

```go
func ShowConfirmDialog(owner walk.Form, title, message string) bool
```

- 基于 `walk.MsgBox(owner, title, message, MsgBoxYesNo|MsgBoxIconQuestion)`
- 返回用户是否点击"是"

### ShowColorDialog

```go
func ShowColorDialog(owner walk.Form, title string, presetColors []string) (string, bool)
```

**实现方式**: 调用 Windows 系统 `ChooseColorW` API（comdlg32.dll）

**内部机制**:
- 使用 `CHOOSECOLORW` 结构体，通过 `syscall.NewLazyDLL("comdlg32.dll")` 加载
- 设置 `CC_ENABLEHOOK` 标志，通过 `CCHookProc` 钩子处理 `WM_INITDIALOG` 消息
- 在钩子中获取对话框尺寸和桌面尺寸，调用 `SetWindowPos` 将对话框居中于屏幕中央
- `hwndOwner` 传入父窗口句柄（来自 `owner.Handle()`），使对话框模态于父窗口
- 自定义颜色数组（16 个 COLORREF）初始为白色
- 取 `presetColors[0]` 作为初始选中颜色（`CC_RGBINIT`）
- 标志：`CC_RGBINIT | CC_FULLOPEN | CC_ANYCOLOR | CC_ENABLEHOOK`

**返回值**: `(#RRGGBBFF 十六进制字符串, 是否确认)`

**居中逻辑**:
```go
GetWindowRect(hDlg, &dlgRect)                // 对话框屏幕坐标
GetWindowRect(GetDesktopWindow(), &desktopRect) // 桌面屏幕坐标
x = (desktopRect.Right - dlgW) / 2
y = (desktopRect.Bottom - dlgH) / 2
SetWindowPos(hDlg, NULL, x, y, SWP_NOSIZE|SWP_NOZORDER)
```

## 预设颜色

| 名称 | 色值 |
|------|------|
| 深紫 | #342333B8 |
| 淡紫 | #A783BEB8 |
| 青绿 | #24A892B8 |
| 蓝色 | #276BA6B8 |
| 红橙 | #C54834B8 |
| 墨绿 | #1F6F5FB8 |
| 深灰 | #30343CBD |

## 检查清单

- [ ] 输入对话框正确返回输入文本
- [ ] 确认对话框正确反映用户选择
- [ ] 颜色对话框调用系统 ChooseColorW，居中于屏幕中央
- [ ] 颜色对话框选中后返回 #RRGGBBFF 格式字符串
- [ ] 用户取消时返回空字符串和 false
