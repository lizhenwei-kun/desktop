# 对话框 (Dialogs)

## 元信息

- **文件**: `internal/ui/dialogs.go`
- **包**: `ui`
- **依赖**: `github.com/lxn/walk`, `github.com/lxn/walk/declarative`

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

**UI 布局**:
```
Dialog (360×160)
├── Label "选择颜色："
├── Composite (Flow layout)
│   ├── PushButton "■" × N（预设颜色）
│   ├── LineEdit（自定义颜色输入）
│   └── PushButton "自定义" → Accept
└── Composite (HBox)
    └── PushButton "取消" → Cancel
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
- [ ] 颜色对话框预设颜色可点击选择
- [ ] 自定义颜色可输入 #RRGGBB 或 #RRGGBBAA
