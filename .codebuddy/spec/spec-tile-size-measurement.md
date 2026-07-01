# 图标磁贴尺寸测量重构经验总结

## 背景

图标磁贴在首次绘制时需要计算自身尺寸（宽度和高度），确保能完整显示图标、4 个汉字或 9 个西文字符（取较大值）。最初使用 walk 框架的 `canvas.MeasureTextPixels` 进行测量，但结果不稳定（部分场景返回异常大/小的值），不得不依靠公式回退。

## 问题

1. **`walk.Canvas.MeasureTextPixels` 精度不可靠**：返回的 bounding box 在某些字体/字号组合下有误差，需要复杂的校验逻辑（测量值必须在公式值的 0.5~2.0 倍范围内才采用）。
2. **字体硬编码**：`draggable_icon.go` 和 `group_card.go` 多处硬编码 `"Microsoft YaHei UI"`、`"Microsoft YaHei"` 等字体名，缺乏统一管理。
3. **磁贴尺寸常量分散**：`desktopIconItemWidth`、`desktopIconItemHeight` 在 `draggable_icon.go` 和 `runner.go` 中重复定义。
4. **文本换行算法粗糙**：`splitTextToLines` 使用固定 rune 计数截断，未考虑中英文字符宽度差异（全角 vs 半角）。

## 解决方案

### 1. 改用 Win32 API 直接测量（核心变更）

参考 `walk_test_2.go` 中已验证的测量方案，使用原生 Win32 API：

```go
// 创建内存 DC
hdc := win.CreateCompatibleDC(0)

// 从 DPI 和字号创建 LOGFONT
dpi := int(win.GetDeviceCaps(hdc, win.LOGPIXELSY))
lf.LfHeight = -win.MulDiv(int32(ptSize), int32(dpi), 72)

// 创建 Win32 字体
hFont := win.CreateFontIndirect(&lf)
win.SelectObject(hdc, win.HGDIOBJ(hFont))

// 实际测量
win.GetTextExtentPoint32(hdc, cjkText, 4, &cjkSize)
win.GetTextExtentPoint32(hdc, asciiText, 9, &asciiSize)
```

**关键发现**：
- `walk.Font` 没有导出 `Handle()` 方法（`dpi2hFont` 字段和 `handleForDPI` 方法均为非导出），**不能直接从 walk.Font 获取 HFONT**。
- 改用 `CreateFontIndirect` 创建一个**相同属性**的 Win32 字体：通过 `Family()`、`PointSize()`、`Style()` 获取属性，构造 `LOGFONT`。
- 测量结果示例（宋体 11pt, 96 DPI）：4 汉字 = 60px，9 字母 = 72px → 磁贴宽度 = 80px。

### 2. 字体管理统一化

新增字体管理模块（`helpers.go`）：

| 函数 | 作用 |
|------|------|
| `InitIconFont(name, size)` | 应用启动时从 config 读取并初始化 |
| `GetIconFont()` | 获取图标标签字体，带回退链 |
| `InitCardFont(name, size)` | 标题字体初始化 |
| `GetCardTitleFont()` | 获取卡片标题字体（Bold），带回退链 |

**回退链**：配置字体 → 宋体同字号 → 宋体 11pt（图标）/ 14pt（标题）。

### 3. 配置化支持

`config.go` 新增字段：

```go
CardFontName string  `json:"card_font_name"`  // 默认 "宋体"
CardFontSize int     `json:"card_font_size"`  // 默认 14
IconFontName string  `json:"icon_font_name"`  // 默认 "宋体"
IconFontSize int     `json:"icon_font_size"`  // 默认 11
```

`runner.go` 在 `NewRunner` 中读取配置并初始化：

```go
cfg := r.manager.GetConfig()
ui.InitIconFont(cfg.IconFontName, cfg.IconFontSize)
ui.InitCardFont(cfg.CardFontName, cfg.CardFontSize)
```

### 4. 布局常量调整

| 常量 | 旧值 | 新值 |
|------|------|------|
| `desktopIconTop` | 4 | 2 |
| `desktopIconLabelTop` | 56 | 52 |
| `desktopIconLineHeight` | 17 | 24 |
| `desktopIconItemWidth` | 74 | 动态计算（默认 80） |
| `desktopIconItemHeight` | 96 | `labelTop + 2*lineHeight + 4` |
| `desktopIconGap` | - | 8（新增，磁贴间距） |

### 5. 文本换行算法改进

`splitTextToLines` 从简单的 rune 计数截断改为**宽度感知型换行**：
- 全角字符（CJK、中文标点）= 2 单位宽度
- 半角字符（ASCII、数字、英文标点）= 1 单位宽度
- 每行总宽度不超过 `maxCJK * 2`（默认可容纳 4 个汉字 = 8 单位）
- 优先在空格处换行

### 6. 磁贴间距逻辑修正

`group_card.go` 中 `paintIconGrid` 和 `getItemIndexAt` 的 `colWidth` 从 `desktopIconItemWidth` 改为 `desktopIconItemWidth + 8 + 8`（左右间距各 8px），确保磁贴之间有可见间隔。

## 关键经验

1. **Win32 API 比 walk 封装更可靠**：`GetTextExtentPoint32` 直接调用 GDI 底层，测量结果与 Windows 实际渲染一致，无需复杂的校验回退逻辑。
2. **walk.Font 的 HFONT 无法直接获取**：需通过 `CreateFontIndirect` + 从 walk.Font 读取属性来重新创建。
3. **`sync.Once` 确保只测一次**：`ensureTileSizeMeasured` 使用 `sync.Once`，首次绘制时触发，后续直接使用缓存值。
4. **字体回退链的重要性**：由于系统字体名称可能不同（如 "Microsoft YaHei UI" vs "Microsoft YaHei"），需要多级回退保证跨平台兼容。
5. **中英文混排宽度计算**：使用全角=2、半角=1 的宽度模型比简单 rune 计数更准确。
