# 壁纸管理 (Wallpaper)

## 元信息

- **文件**: `internal/ui/wallpaper.go`
- **包**: `ui`
- **依赖**: `user32.dll(SystemParametersInfoW)`, `advapi32.dll(Registry)`, `image/jpeg`, `image/png`

## API

| 函数 | 功能 |
|------|------|
| `GetCurrentWallpaper() string` | 三级回退获取壁纸路径 |
| `WallpaperExists() bool` | 检查壁纸文件是否存在 |
| `LoadWallpaperImage(targetW, targetH int) *image.RGBA` | 加载壁纸并按 Fill 模式裁剪到目标尺寸 |

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

### 在 desktop_mode.go 中的集成

```go
// loadWallpaper 中：
img := LoadWallpaperImage(dm.workW, dm.workH)  // 已裁剪为精确工作区尺寸
bmp, _ := walk.NewBitmapFromImageForDPI(img, 96)
dm.wallpaperBmp = bmp

// paintWallpaper 中：
canvas.DrawBitmapWithOpacityPixels(dm.wallpaperBmp, bounds, 255)  // 1:1 绘制，无拉伸
```

## 检查清单

- [ ] 优先使用 TranscodedWallpaper（已适配的缓存版本）
- [ ] 使用 Go 标准库解码，不用 GDI+（避免颜色失真）
- [ ] Fill 模式：等比缩放 + 居中裁剪，不拉伸变形
- [ ] 壁纸位图尺寸必须与工作区精确一致，绘制时 1:1 无拉伸
- [ ] LoadWallpaperImage 失败时回退到 GDI+ 加载
- [ ] 壁纸文件存在性检查（os.Stat）
- [ ] 无壁纸时返回 nil/空字符串，不崩溃
