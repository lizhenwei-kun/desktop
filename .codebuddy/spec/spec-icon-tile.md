# 图标磁贴 (Icon Tile)

## 元信息

- **文件**: `internal/ui/draggable_icon.go`
- **包**: `ui`
- **依赖**: `walk`, `win`

## 常量

| 常量 | 值 | 说明 |
|------|-----|------|
| desktopIconItemWidth | 74 | 磁贴宽度 |
| desktopIconItemHeight | 96 | 磁贴高度 |
| desktopIconSize | 48 | 图标尺寸 |
| desktopIconTop | 4 | 图标顶部偏移 |
| desktopIconLabelTop | 56 | 文字顶部偏移 |
| desktopIconLineHeight | 17 | 行高 |
| desktopIconTextSize | 13 | 字号 |
| longPressDragDelay | 3s | 长按触发拖拽延迟 |

## 核心类型

```go
type DraggableIcon struct {
    widget       *walk.CustomWidget
    filePath     string
    displayName  string
    iconImg      image.Image
    isPressed    bool
    isDragging   bool
    pressTime    time.Time
    onDoubleClick func()
    onDragStart  func(filePath string)
    onDragEnd    func(filePath string, x, y int)
    groupName    string
}
```

## 绘制流程

```
paint(canvas, bounds)
├── drawIcon(canvas, bounds)
│   ├── iconImg → *image.RGBA
│   ├── walk.NewBitmapFromImage
│   ├── 居中绘制 48×48 图标
│   └── canvas.DrawBitmapWithOpacityPixels
└── drawLabel(canvas, bounds)
    ├── Font: Microsoft YaHei, 13pt, Bold
    ├── splitTextToLines(displayName, 8 chars/line)
    ├── 最多2行，第2行省略号截断
    ├── 白色文字 + 黑色阴影 (偏移1px)
    └── canvas.DrawTextPixels
```

## 交互

| 操作 | 行为 |
|------|------|
| 鼠标按下 | isPressed=true, 启动长按检测协程 |
| 长按3秒 | isDragging=true, 光标变 SizeAll |
| 鼠标释放 | isPressed=false, isDragging=false |
| 双击 | 触发 onDoubleClick → executor.Execute |

## 文字拆分

```
splitTextToLines(text, maxRunesPerLine=8)
├── rune 计数（支持中文）
├── 一行能容纳 → 返回单行
└── 多行 → 每 8 个 rune 切分一次
```

## 检查清单

- [ ] 图标正确绘制，居中 48×48
- [ ] 文字白色+阴影效果清晰
- [ ] 中文文件名正确拆行
- [ ] 长按3秒拖拽不误触
