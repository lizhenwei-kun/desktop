# 壁纸管理 (Wallpaper)

## 元信息

- **文件**: `internal/ui/wallpaper.go`, `internal/ui/desktop/wallpaper_methods.go`
- **包**: `ui`, `desktop`
- **依赖**: `user32.dll(SystemParametersInfoW)`, `advapi32.dll(Registry)`, `image/jpeg`, `image/png`, `walk`

## API

| 函数 | 功能 |
|------|------|
| `GetCurrentWallpaper() string` | 三级回退获取壁纸路径 |
| `WallpaperExists() bool` | 检查壁纸文件是否存在 |
| `LoadWallpaperImage(targetW, targetH int) *image.RGBA` | 加载壁纸并按 Fill 模式缩放/裁剪到目标尺寸（目标即工作区物理像素尺寸） |
| `WallpaperState.LoadWallpaper(dpiFn, workW, workH)` | 直接按工作区尺寸 1:1 加载壁纸（不二次缩放） |
| `WallpaperState.PaintWallpaper(canvas, bounds)` | 1:1 绘制壁纸，无缩放 |
| `WallpaperState.HasWallpaper() bool` | 线程安全地判断壁纸是否已加载（供跨包调试日志） |

## 核心原则

### 1. 能用 Windows API 的尽量用

壁纸相关操作优先使用 Windows 原生 API：

- **壁纸路径获取**: `SystemParametersInfoW(SPI_GETDESKWALLPAPER)` + 注册表回退
- **壁纸解码**: Go 标准库 `image/jpeg` / `image/png`（避免 GDI+ 颜色失真）
- **裁剪/缩放**: Go `image/draw` + `image.NewRGBA`（精确可控）

> 注意: `walk.NewBitmapFromFileForDPI` 底层用 GDI+，加载 TranscodedWallpaper 时会有颜色失真（偏青/偏亮），因此优先用 Go 标准库解码。

### 2. 壁纸直接按工作区尺寸 1:1 加载（无二次缩放）

**当前实现**：不使用"全屏加载再裁剪"的两步流程，而是直接调用 `LoadWallpaperImage(workW, workH)`，`LoadWallpaperImage` 内部已按 Fill 模式把壁纸缩放/裁剪到工作区物理像素尺寸（targetW=workW, targetH=workH），输出的 `image.RGBA` 像素尺寸就是 `workW × workH`。

**关键注意事项（⚠️ 多次刷新背景跳动/颜色变化的根因）**：

- `LoadWallpaperImage` 返回的 `image.RGBA` 已经是**物理像素**尺寸，与 DPI 无关。
- 把它转成 walk 位图时**必须**用 `walk.NewBitmapFromImage`（1:1 物理像素），**绝不能**用 `walk.NewBitmapFromImageForDPI(img, 96)`。
- `NewBitmapFromImageForDPI(img, 96)` 会把 `img` 的像素当成 **96 DPI 的逻辑像素**，在 DPI≠96 的屏幕上绘制时 walk 会再按屏幕 DPI 放大，导致：
  1. 壁纸被二次缩放 → 多次刷新/不同 DPI 上下文下画面**位置跳动**；
  2. 重采样 → **颜色变化**、与系统壁纸对不上。
- `NewBitmapFromImage` 虽有 deprecation hint，但此处必须用它才能保证 1:1 物理像素，忽略该提示。

```
LoadWallpaper(dpiFn, workW, workH)
├── LoadWallpaperImage(workW, workH)         ← 直接输出工作区物理像素尺寸 image.RGBA
│   ├── Go 标准库解码
│   ├── Fill 模式缩放（等比缩放 + 居中裁剪）到 workW×workH
│   └── 输出 workW×workH
└── walk.NewBitmapFromImage(img)             ← 1:1 物理像素，不传 DPI
```

### 3. 隐藏→显示时壁纸处理

窗口从隐藏变为可见时（`showDesktopMode`）：
1. 先设置 BodyWidget 的 bounds 到正确的工作区尺寸
2. 调用 `SetPaintDirty()` 确保下次 WM_PAINT 执行全量绘制
3. 再执行 MoveWindow 改变窗口位置
4. 使用已有的 `WallpaperBmp` 缓存（启动时已裁剪好），不重新加载

### 4. 壁纸位图的并发安全（⚠️ 异步刷新竞态）

- 壁纸加载在后台 strand/goroutine（`dm.Work.Post`）执行，绘制在 UI 主线程执行，二者会并发访问 `WallpaperBmp` 指针。
- **必须**用互斥锁保护：`swapBitmap()`（加锁 Dispose 旧位图 + 赋值新位图）和 `getBitmap()`/`HasWallpaper()`（加锁读取）。
- **禁止**在绘制线程或日志里直接裸读 `s.WallpaperBmp`，否则会读到正在被 Dispose 的半释放位图，表现为颜色错乱/跳动。

### 5. 刷新时捕获尺寸快照（⚠️ 分辨率/ DPI 变化竞态）

- `refreshDesktop` 在后台 goroutine 里读取 `dm.WorkW/dm.WorkH` 加载壁纸，若在 goroutine 排队期间屏幕分辨率/DPI 变化，加载的壁纸尺寸会与当前绘制 bounds 不一致 → 跳动。
- **修复**：在 UI 线程（调用 `refreshDesktop` 时）先捕获 `workW, workH, dpiFn` 快照，再传入后台任务，保证加载尺寸与后续绘制 bounds 一致。

## 壁纸路径获取策略（优先级从高到低）

```
GetCurrentWallpaper()
├── 方法1: TranscodedWallpaper（系统缓存，已适配分辨率）
│   ├── %APPDATA%\Microsoft\Windows\Themes\TranscodedWallpaper
│   └── CachedFiles\CachedImage_1920_1080_POS4.jpg（旧版 Windows）
├── 方法2: SystemParametersInfoW(SPI_GETDESKWALLPAPER)
│   └── 成功且文件存在 → 返回
├── 方法3: 注册表 HKCU\Control Panel\Desktop\Wallpaper
│   └── 成功且文件存在 → 返回
└── 全部失败 → 返回 ""
```

## 壁纸加载方案

### 关键决策：使用 Go 标准库解码，而非 GDI+

**问题**: `walk.NewBitmapFromFileForDPI`（底层 GDI+ `GdipCreateBitmapFromFile`）加载 TranscodedWallpaper 时：
1. GDI+ alpha blend 拉伸处理导致颜色失真（偏青/偏亮）
2. TranscodedWallpaper 无扩展名，GDI+ 颜色管理行为不可控

**方案**: 用 Go `image/jpeg` / `image/png` 直接解码，颜色精确可控。

### Fill 模式裁剪逻辑

```
LoadWallpaperImage(targetW, targetH)
├── Go 标准库解码图片（jpeg → png → image.Decode 三级回退）
├── 计算 Fill 缩放比 = max(targetW/srcW, targetH/srcH)
├── scale ≈ 1.0（差距 <1%）→ 直接居中裁剪（无损）
└── scale != 1.0 → 最近邻缩放 + 居中裁剪
```

**典型场景**: TranscodedWallpaper 为 2560x1600，工作区为 2560x1400：
- scaleX = 1.0, scaleY = 0.875 → scale = 1.0
- 直接居中裁剪：上下各去掉 100px

### 在 desktop 包中的集成

```go
// LoadWallpaper 中：
img := LoadWallpaperImage(workW, workH)              // 直接输出工作区物理像素尺寸
bmp, err := walk.NewBitmapFromImage(img)             // 1:1 物理像素，不传 DPI
s.swapBitmap(bmp)                                    // 加锁替换（Dispose 旧位图）

// PaintWallpaper 中：
if bmp := s.getBitmap(); bmp != nil {                // 加锁读取
    canvas.DrawBitmapWithOpacityPixels(bmp, bounds, 255)  // 1:1 绘制，无拉伸
}
```

## 检查清单

- [ ] 优先使用 Windows API（SystemParametersInfoW）
- [ ] 使用 Go 标准库解码，不用 GDI+（避免颜色失真）
- [ ] 壁纸直接按工作区尺寸加载（`LoadWallpaperImage(workW, workH)`），不再全屏加载+裁剪
- [ ] 转 walk 位图必须用 `NewBitmapFromImage`（1:1 物理像素），**禁止**用 `NewBitmapFromImageForDPI(img, 96)`（会导致 DPI 二次缩放、跳动、颜色变化）
- [ ] 壁纸位图尺寸与工作区精确一致，绘制时 1:1 无拉伸
- [ ] Fill 模式：等比缩放 + 居中裁剪，不拉伸变形
- [ ] 隐藏→显示时先设 bounds + SetPaintDirty，再 MoveWindow
- [ ] `WallpaperBmp` 的所有读写必须加锁（swapBitmap/getBitmap/HasWallpaper），禁止裸读
- [ ] 刷新壁纸时在 UI 线程捕获 workW/workH/dpiFn 快照传入后台，避免尺寸竞态
- [ ] 壁纸文件存在性检查（os.Stat）
- [ ] 无壁纸时返回 nil/空字符串，不崩溃
