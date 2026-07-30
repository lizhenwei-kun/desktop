# 分组卡片 (Group Card)

## 元信息

- **文件**: `internal/ui/group_card.go`
- **包**: `ui`
- **依赖**: `walk`, `win`, `config`, `group`

## 核心字段说明

卡片由 container（walk.Composite，无 Layout 用于绝对定位）和 bodyWidget（walk.CustomWidget，自定义绘制区域）组成。位置和尺寸使用 0~1 相对坐标存储，绘制时转为像素值。

拖拽状态包含：isDragging（是否正在拖拽）、isPressed（鼠标是否仍按下，用于 goroutine 竞态保护）、dragStartTime（按下时间戳）、以及两组坐标——dragStartX/Y（按下时鼠标相对 bodyWidget 的位置，仅用于双击区判断，拖拽不用）和 dragScreenX/Y + dragCardX/Y（按下时的屏幕绝对坐标和卡片屏幕坐标，用于精确跟随鼠标）。

缩放状态包含：isResizing、resizeEdge（8 方向）、resizeStartX/Y（缩放起点鼠标位置）和 resizeStartW/H（缩放前卡片尺寸）。

回调包含：onPositionChanged、onSizeChanged、onRename、onColor、onDelete。

## 常量

| 常量 | 值 | 说明 |
|------|-----|------|
| cardMinWidth | 220 | 卡片最小像素宽度 |
| cardMinHeight | 160 | 卡片最小像素高度 |
| cardHeaderHeight | 30 | 标题栏高度 |
| resizeHandleSize | 8 | 缩放热区大小 |
| actionBtnWidth | 22 | 操作按钮宽度 |
| actionBtnHeight | 20 | 操作按钮高度 |
| actionBtnGap | 2 | 按钮间距 |
| doubleClickMs | 500 | 双击判定窗口（毫秒） |

## 交互规格

| 操作 | 触发条件 | 行为 |
|------|----------|------|
| 拖拽卡片 | 标题栏空白处长按 3 秒 | isPressed 保护，goroutine 3秒后置 isDragging=true，MouseMove 用屏幕绝对坐标偏移量更新位置 + container.SetBoundsPixels + manager.UpdateGroupPosition |
| 缩放卡片 | 拖动 8 方向边缘热区（8px） | 实时更新 size 和 position，实时回调 onSizeChanged / onPositionChanged |
| 缩放结束 | 鼠标释放 | 持久化 size 和 position |
| 操作按钮 | 点击标题栏右侧按钮 | × 删除 / 色 改颜色 / ✎ 重命名，弹对应对话框 |
| 图标双击 | 图标区域双击（500ms 内） | executor.Execute 执行程序 |
| 拖拽结束 | 鼠标释放 | 清除 isPressed / isDragging，触发 bodyWidget.Invalidate 清除残留 |

### 拖拽实现要点

1. **触发时机**：仅标题栏空白处（y < cardHeaderHeight 且不在操作按钮热区）记录按下状态，启动 3 秒 goroutine。操作按钮和图标区域不触发。
2. **竞态保护**：MouseDown 时设 isPressed=true，MouseUp 时设 false。goroutine 醒来后检查 isPressed 才置 isDragging=true，避免鼠标已释放仍误判。
3. **精准跟随**：按下时通过 win.ClientToScreen 获取鼠标屏幕绝对坐标和卡片屏幕坐标。MouseMove 中再次转为屏幕坐标求差值，新位置 = 初始卡片位置 + 屏幕偏移量。避免因窗口移动导致相对坐标变化引起的计算偏差。
4. **闪烁处理**：拖拽中只移动 container，bodyWidget 作为子控件自动跟随，无需重复 SetBoundsPixels。使用 container.SetBoundsPixels 正常触发重绘，不额外 suppress redraw，避免残留。
5. **拖拽速度**：每次 MouseMove 计算完整的屏幕偏移（不是增量累加），确保卡片与鼠标 1:1 跟随，不加速不滞后。

### 拖拽/缩放结束后的重叠重绘（⚠️ 关键坑）

walk 中每张卡片是独立的 `Container` 子窗口，拖拽/缩放结束后 `applyBounds` 只使当前卡片自身窗口无效化。被移动卡片覆盖的其他卡片**不会**收到 `WM_PAINT`，导致重叠区域不重绘。

**正确做法**：在 `OnCardDragOutlineEnd` / `OnResizeOutlineEnd` 回调中，遍历所有其他卡片，用矩形相交检测（`cx < ox+ow && cx+cw > ox && cy < oy+oh && cy+ch > oy`）判断是否有交集，对相交卡片调用 `win.InvalidateRect(c.BodyWidgetHandle(), nil, false)`。

- **不能**无差别 invalidate 所有卡片，否则导致不必要的闪烁
- **不能**只依赖 `dm.InvalidateBody()`，它只刷新桌面背景（壁纸 + 未分组图标），不刷新卡片内部
- 实现位置：`internal/ui/desktop/card_management.go` 的 `setupCardActions` 中
- GroupCard 需导出 `PixelX()` / `PixelY()` 方法供 desktop 包调用

## 缩放方向检测

8px 热区判定规则：
- 左上角（x < 8 && y < 8）：TopLeft
- 右上角（x > w-8 && y < 8）：TopRight
- 左下角（x < 8 && y > h-8）：BottomLeft
- 右下角（x > w-8 && y > h-8）：BottomRight
- 左边缘（x < 8）：Left
- 右边缘（x > w-8）：Right
- 上边缘（y < 8）：Top
- 下边缘（y > h-8）：Bottom
- 其他：None

## 光标更新映射

| 边缘 | 光标样式 |
|------|---------|
| 左/右 | SizeWE（水平双向箭头） |
| 上/下 | SizeNS（垂直双向箭头） |
| 左上/右下 | SizeNWSE（斜向↘） |
| 右上/左下 | SizeNESW（斜向↗） |
| 普通 | Arrow |

## API

| 方法 | 功能 |
|------|------|
| Container() | 返回底层的 walk.Composite 容器 |
| SetOnPositionChanged(fn) | 设置位置变更回调 |
| SetOnSizeChanged(fn) | 设置尺寸变更回调 |
| SetOnRename(fn) | 设置重命名回调 |
| SetOnColor(fn) | 设置改颜色回调 |
| SetOnDelete(fn) | 设置删除回调 |
| Refresh() | 刷新项目列表并触发重绘 |
| SetPosition(x, y) | 用相对坐标设置位置 |
| SetSize(w, h) | 用相对坐标设置尺寸 |
| ReapplyBounds() | 重新应用位置和尺寸（用于布局变动后恢复） |

## 检查清单

- [x] 标题栏空白处长按 3 秒触发拖拽，操作按钮和图标区不误触
- [x] 拖拽精确跟随鼠标（屏幕绝对坐标保证）
- [x] MouseUp 后清除 isPressed 避免拖拽残留
- [x] 拖拽结束清除残留重绘
- [x] 8 方向缩放正确
- [x] 最小尺寸 220×160 限制生效
- [x] 缩放结束持久化配置
