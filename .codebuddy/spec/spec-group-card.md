# 分组卡片 (Group Card)

## 元信息

- **文件**: `internal/ui/group_card.go`
- **包**: `ui`
- **依赖**: `walk`, `config`, `group`

## 核心类型

```go
type GroupCard struct {
    container  *walk.Composite    // 容器（无 Layout，用于绝对定位）
    headerBar  *walk.Composite    // （预留）
    bodyWidget *walk.CustomWidget // 自定义绘制区域
    groupName  string
    groupColor color.RGBA
    position   config.Position    // 相对坐标 0~1
    size       config.Size        // 相对尺寸 0~1
    items      []group.GroupItem  // 分组内的项目
    icons      []*DraggableIcon   // （预留）
    manager    *group.Manager
    executor   *ProgramExecutor
    owner      walk.Form
    workW, workH int             // 工作区像素尺寸

    // 拖拽状态
    isDragging    bool
    dragStartX, dragStartY    int
    dragStartTime time.Time

    // 缩放状态
    isResizing   bool
    resizeEdge   ResizeEdge
    resizeStartX, resizeStartY int
    resizeStartW, resizeStartH int

    // 回调
    onPositionChanged func(name string, x, y float64)
    onSizeChanged     func(name string, w, h float64)
    onRefresh         func()
}

type ResizeEdge int // None, Left, Right, Top, Bottom, TopLeft, TopRight, BottomLeft, BottomRight
```

## 常量

| 常量 | 值 | 说明 |
|------|-----|------|
| cardMinWidth | 220 | 卡片最小像素宽度 |
| cardMinHeight | 160 | 卡片最小像素高度 |
| cardHeaderHeight | 30 | 标题栏高度 |
| resizeHandleSize | 8 | 缩放热区大小 |

## 交互规格

| 操作 | 触发条件 | 行为 |
|------|----------|------|
| 拖拽卡片 | 长按标题栏 3秒 | 更新 position + container/body 位置 + manager.UpdateGroupPosition |
| 缩放卡片 | 拖动 8 方向边缘热区 | 更新 size + container/body 尺寸 + 实时回调 |
| 缩放结束 | 鼠标释放 | 持久化 size 和 position |

## 缩放方向检测

```
getResizeEdge(x, y)
├── x < 8 && y < 8 → TopLeft
├── x > w-8 && y < 8 → TopRight
├── x < 8 && y > h-8 → BottomLeft
├── x > w-8 && y > h-8 → BottomRight
├── x < 8 → Left
├── x > w-8 → Right
├── y < 8 → Top
├── y > h-8 → Bottom
└── None
```

## 光标更新

| 边缘 | 光标 |
|------|------|
| 左/右 | SizeWE (↔) |
| 上/下 | SizeNS (↕) |
| 左上/右下 | SizeNWSE (↘) |
| 右上/左下 | SizeNESW (↗) |
| 普通 | Arrow |

## API

| 方法 | 功能 |
|------|------|
| `Container() *walk.Composite` | 返回容器 |
| `SetOnPositionChanged(fn)` | 位置变更回调 |
| `SetOnSizeChanged(fn)` | 尺寸变更回调 |
| `Refresh()` | 刷新项目列表 |
| `SetPosition(x, y)` | 设置位置（相对坐标） |
| `SetSize(w, h)` | 设置尺寸（相对坐标） |
| `ReapplyBounds()` | 重新应用位置和尺寸 |

## 检查清单

- [ ] 长按3秒触发拖拽，不误触
- [ ] 8 方向缩放正确
- [ ] 缩放/拖拽不超出工作区
- [ ] 最小尺寸 220×160 限制生效
- [ ] 结束操作后持久化配置
