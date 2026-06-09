# 壁纸管理 (Wallpaper)

## 元信息

- **文件**: `internal/ui/wallpaper.go`
- **包**: `ui`
- **依赖**: `user32.dll(SystemParametersInfoW)`, `advapi32.dll(Registry)`

## API

| 函数 | 功能 |
|------|------|
| `GetCurrentWallpaper() string` | 三级回退获取壁纸路径 |
| `WallpaperExists() bool` | 检查壁纸文件是否存在 |

## 三级回退策略

```
GetCurrentWallpaper()
├── 方法1: SystemParametersInfoW(SPI_GETDESKWALLPAPER)
│   └── 成功且文件存在 → 返回
├── 方法2: 注册表 HKCU\Control Panel\Desktop\Wallpaper
│   └── 成功且文件存在 → 返回
└── 方法3: Windows 缓存壁纸
    ├── %APPDATA%\Microsoft\Windows\Themes\TranscodedWallpaper
    └── CachedFiles\CachedImage_1920_1080_POS4.jpg
    └── 成功 → 返回
    └── 全部失败 → 返回 ""
```

## 检查清单

- [ ] 三种方法依次回退，不跳过
- [ ] 壁纸文件存在性检查（os.Stat）
- [ ] 无壁纸时返回空字符串，不崩溃
- [ ] 壁纸变化后重启程序加载新壁纸
