# 图标悬停高亮框 (Icon Hover Highlight)

## 元信息

- **文件**: `internal/ui/helpers.go`（绘制函数）、`internal/ui/group_card.go`（卡片内悬停）、`internal/ui/desktop_mode.go`（自由图标悬停）
- **包**: `ui`
- **依赖**: `walk`, `image`, `image/color`

## 绘制定位

### 函数签名

```go
func drawHoverRect(canvas *walk.Canvas, bounds walk.Rectangle)
```

### 最终参数（调试迭代后）

| 部分 | RGBA | Alpha | 透明度 |
|------|------|-------|--------|
| 填充 | `(0xE8, 0xF4, 0xFF, 0x0D)` 淡冰蓝 | 13 | ≈ 95% |
| 边框 | `(0x4A, 0xA0, 0xFF, 0x0D)` 亮蓝 | 13 | ≈ 95% |

### 实现说明

使用单张 `*image.RGBA` 位图，填充和边框各像素独立写入 alpha，最后用 `DrawBitmapWithOpacityPixels(bmp, bounds, 255)` 绘制，preserve 像素级透明度。边框宽度 1px（最外层一圈像素）。

## 两种悬停场景

### 场景 A：卡片（GroupCard）内图标

- **字段**: `GroupCard.hoveredItemIdx int`（-1 表示无悬停）
- **初始值**: `-1`（在 `NewGroupCard` 中显式设置，避免 int 零值 0 导致第一个图标默认高亮）
- **检测**: `bodyWidget.MouseMove` 中调用 `getItemIndexAt(x, y)` 获取当前光标下的图标索引
- **触发重绘**: 仅当 `idx != gc.hoveredItemIdx` 时才调用 `gc.bodyWidget.Invalidate()`，避免频繁无效重绘
- **绘制**: `paintIconGrid` 遍历时传入 `i == gc.hoveredItemIdx`，在 `paintIconTile` 中先于图标和文字绘制 `drawHoverRect`

#### 悬停检测代码（group_card.go）

```
MouseMove:
├── isResizing → handleResize
├── isDragging → handleDrag
└── else → updateCursor
           ├── idx = getItemIndexAt(x, y)
           └── if idx != hoveredItemIdx → hoveredItemIdx = idx, Invalidate()
```

### 场景 B：自由桌面图标（DesktopMode 右侧）

- **字段**: `DesktopMode.hoveredFreeIdx int`（-1 表示无悬停）
- **初始值**: `-1`
- **检测**: `bodyWidget.MouseMove` 中手动计算图标 tile 区域（位置公式与 `paintFreeItems` 一致）
- **触发重绘**: 仅 `newIdx != dm.hoveredFreeIdx` 时调用 `dm.bodyWidget.Invalidate()`
- **绘制**: `paintFreeItems` 传入 `i == dm.hoveredFreeIdx`

## 交互流程

```
鼠标移入图标区域
├── MouseMove 触发
├── 计算图标索引 idx
├── idx 与 hoveredIdx 不同？
│   ├── 是 → 更新 hoveredIdx，Invalidate()
│   └── 否 → 跳过
├── paintDesktop → paintFreeItems（自由图标）
│   └── paintIconTile(canvas, item, x, y, hovered)
│       ├── hovered 为 true → drawHoverRect（先画框）
│       ├── 绘制图标
│       └── 绘制文字
└── paintBody → paintIconGrid（卡片内图标，同上）
```

## 调试经验总结

### 透明度选择历程

| 阶段 | 填充 | 边框 | 用户反馈 |
|------|------|------|----------|
| 初版 | `A=0x40` 蓝 (75% 透明) | 不透明蓝 | "太显了" |
| 更淡 | `A=0x12` 蓝 | `A=0x40` 蓝 | "边框也要透明" |
| 统一 90% | `A=0x1A` 蓝 | `A=0x1A` 蓝 | "太显了" |
| 仅边框 | `A=0x00` 无填充 | `A=0x0A` 蓝 | "需要填充" |
| 深色 95% | `A=0x0D` 黑 | `A=0x0D` 黑 | "深蓝色" |
| 深蓝 95% | `A=0x0D` 深蓝 | `A=0x0D` 深蓝 | 未满意 |
| 还原初版 | `A=0x40` 蓝 (初版值) | `A=0x40` 蓝 | 参考图片后改方向 |
| 最终 | `A=0x0D` 淡冰蓝 + 亮蓝 | `A=0x0D` 亮蓝 | ✅ |

### 关键经验

1. **深色填充不可取**：黑色/深蓝色填充在桌面背景上会明显变暗，视觉突兀。
2. **大面积填充 vs 仅边框**：大面积填充即使透明度很高（95%），在对比色图标（如橙色）上仍显眼。但用户明确要求需要填充。
3. **淡色填充更协调**：淡冰蓝（偏白）填充 + 亮蓝边框的视觉重量更接近 Windows 原生效果，看起来比深色填充自然。
4. **边框不要单独用 Pen 绘制**：`walk.CosmeticPen` 不支持 alpha 通道。透明边框必须用像素位图实现。
5. **hoveredIdx 必须初始化为 -1**：Go int 零值为 0，如果初始化为 0 会导致第一个图标在未悬停时默认高亮。
6. **Invalidate 频率控制**：只在 hoveredIdx 变化时触发重绘，不在每次 MouseMove 都无脑 Invalidate，否则 CPU 占用高。
7. **检测与绘制坐标一致**：自由图标的悬停检测坐标公式必须与 `paintFreeItems` 中 `paintIconTile` 的 x/y 完全一致（`startX = bounds.Width - desktopIconItemWidth - 20`, `startY = 60`），否则悬停区域与绘制区域错位。
8. **卡片内图标悬停范围**：`getItemIndexAt` 的 `colWidth` 包含 tile 间距（`desktopIconItemWidth + 8 + 8`），判定区域比实际磁贴略宽。而 `drawHoverRect` 只覆盖 `desktopIconItemWidth`，导致鼠标在间距处也算悬停，但高亮框不会超出磁贴边界，行为合理。

## 检查清单

- [ ] 卡片内图标悬停正确高亮
- [ ] 自由桌面图标悬停正确高亮
- [ ] hoveredIdx 初始化为 -1，无初始误亮
- [ ] hoveredIdx 变化时才触发 Invalidate，性能合理
- [ ] 鼠标离开图标区域后高亮消失（idx 变 -1）
- [ ] 拖拽/缩放中不触发悬停检测
- [ ] 填充和边框使用像素级 alpha 位图，无需 Pen

## 图标选中状态

### 选中/悬停透明度

| 状态 | 函数 | 颜色 | Alpha | 边框 |
|------|------|------|-------|------|
| **选中** | `DrawSelectionRect` | `(0x00, 0x55, 0xAA)` 深蓝 | 70 (≈28%) | 1px `#003A7A` |
| **悬停** | `DrawHoverRect` | `(0x00, 0x45, 0x8A)` 浅蓝 | 50 (≈20%) | 1px `#003A7A` |

透明度通过 `DrawBitmapWithOpacityPixels` 的 `opacity` 参数控制（位图自身 alpha 固定为 255），而非位图像素 alpha。这样 walk 的 `alphaBlendPart` 中 `opacity != 255` 就不会走 `StretchBlt` 优化分支，始终走 `AlphaBlend`，透明通道正确。

### 长文字选中显示不全的根因与修复

**根因**：卡片使用 `PaintNoErase` 模式，`WM_PAINT` 中 `BeginPaint` 返回的 HDC 自带裁剪区域（`ps.RcPaint`），只包含 `InvalidateRect` 标记的无效矩形。`invalidateTile` 原来只用 `desktopIconItemHeight`（2 行文字高度）标记无效区域，选中时扩展的文字区域（3-4 行）不在重绘区域内，被 HDC 裁剪。

桌面版未分组图标使用 `PaintBuffered` 模式，先画到全尺寸内存位图再 `BitBlt` 到屏幕，不受 `ps.RcPaint` 裁剪影响，所以无此问题。

**修复方案**：`invalidateTile` 始终用最大文字高度（4 行）计算无效矩形，不区分选中/非选中。这样选中时扩展区域在重绘范围内，取消选中时扩展区域的选中框残留也能被正确清除。

```go
// invalidateTile — 始终用最大可能高度（4行文字）
func (gc *GroupCard) invalidateTile(idx int) {
    x, y := gc.getIconTileBounds(idx)
    lines := SplitTextToLines(gc.items[idx].Name, 4)
    tileH := DesktopIconLabelTop() + len(lines)*DesktopIconLineHeight() + 8
    if tileH < desktopIconItemHeight {
        tileH = desktopIconItemHeight
    }
    r := win.RECT{
        Left: int32(x), Top: int32(y),
        Right:  int32(x + TileColWidth()),
        Bottom: int32(y + tileH),
    }
    win.InvalidateRect(gc.bodyWidget.Handle(), &r, false)
}
```

### 编辑模式下选中框残留

**问题**：进入图标标题编辑模式时，编辑框覆盖在磁贴上方，但上一帧画出的选中框没有被清除，编辑模式下绘制函数检测到 `isEditing`/`EditingPath` 直接跳过绘制选中框，残留的选中框仍然可见。

**修复**：编辑模式入口处清除选中状态并触发重绘。
- **卡片内**（`startCardItemEdit`）：调用 `gc.ClearSelection()`
- **未分组**（`startItemEdit`）：设置 `dm.SelectedPath = ""` + `dm.InvalidateBody()`
