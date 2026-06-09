# 图标提取 (Icon Extraction)

## 元信息

- **文件**: `internal/ui/windows_icon.go`
- **包**: `ui`
- **依赖**: `shell32.dll(SHGetFileInfoW)`, `gdi32.dll(GetDIBits)`, `user32.dll(GetIconInfo)`

## 核心类型

```go
type IconExtractor struct{}

type SHFILEINFOW struct {
    HIcon         uintptr
    IIcon         int32
    DwAttributes  uint32
    SzDisplayName [260]uint16
    SzTypeName    [80]uint16
}

type ICONINFO struct {
    FIcon    int32
    XHotspot uint32
    YHotspot uint32
    HbmMask  uintptr
    HbmColor uintptr
}

type BITMAPINFOHEADER struct {
    BiSize          uint32
    BiWidth         int32
    BiHeight        int32
    BiPlanes        uint16
    BiBitCount      uint16
    BiCompression   uint32
    BiSizeImage     uint32
    // ...
}
```

## 图标缓存

```go
var iconCache sync.Map // key=filePath, value=image.Image
```

## 提取流程

```
GetIconImage(filePath)
├── 缓存命中 → 直接返回
├── resolveIconPath(filePath)
│   ├── .lnk → parseLnkTarget（解析 LNK 二进制格式）
│   ├── .url → parseURLIconFile（解析 IconFile 字段）
│   └── 其他 → 使用原路径
├── extractIcon(actualPath)
│   ├── SHGetFileInfoW → HICON
│   ├── GetIconInfo → HBM
│   ├── GetDIBits → 像素数据
│   ├── BGRA → RGBA 转换
│   └── *image.RGBA
├── isLowQualityIcon? → 使用回退
├── getFallbackIcon → 文件夹或文件图标
├── iconCache.Store
└── return img
```

## 回退图标

| 类型 | 图标 |
|------|------|
| 文件夹 | 黄色文件夹 (48×48) |
| 文件 | 白色文档 + 灰色折角 (48×48) |

## 质量检测

```
isLowQualityIcon(img)
├── 计算可见像素数
├── 计算亮像素比例
└── 可见像素 < 总面积/10 → 低质量 → 使用回退
```

## 工具函数

| 函数 | 功能 |
|------|------|
| `GetCachedIconPath(filePath) string` | 获取缓存 PNG 路径 |
| `SaveIconToFile(img, path) error` | 保存图标 PNG |
| `CreateTrayIcon() *image.RGBA` | 创建托盘图标 |
| `CreateAppIconImage() *image.RGBA` | 创建应用图标 |
| `SaveImageAsICO(img, path) error` | 保存 ICO 文件 |
| `SaveTrayIconToFile() string` | 保存托盘 ICO |
| `SaveAppIconToFile() string` | 保存应用 ICO |

## 检查清单

- [ ] LNK 快捷方式正确解析目标路径
- [ ] URL 快捷方式正确解析 IconFile
- [ ] 图标缓存避免重复提取
- [ ] 低质量图标正确回退
- [ ] ICO 文件格式正确，可被 Windows 识别
