# 配置模块 (Config)

## 元信息

- **文件**: `internal/config/config.go`
- **包**: `config`
- **存储路径**: `%USERPROFILE%\.desktop_go\config.json`

## 数据结构

```go
type Position struct {
    X float64 `json:"x"` // 相对坐标 0.0~1.0
    Y float64 `json:"y"`
}

type Size struct {
    Width  float64 `json:"width"`
    Height float64 `json:"height"`
}

type Group struct {
    Name     string   `json:"name"`
    Position Position `json:"position"`
    Size     Size     `json:"size"`
    Color    string   `json:"color"` // #RRGGBBAA
}

type Config struct {
    Groups       []Group           `json:"groups"`
    DesktopItems map[string]string `json:"desktop_items"` // 路径 → 分组名
}
```

## 默认分组

| 名称 | X | Y | W | H | 颜色 |
|------|---|---|---|---|------|
| 快捷方式 | 0.017 | 0.079 | 0.156 | 0.563 | #342333B8 |
| 备份文件 | 0.175 | 0.079 | 0.158 | 0.225 | #A783BEB8 |
| Word | 0.334 | 0.079 | 0.168 | 0.225 | #24A892B8 |
| 图片 | 0.175 | 0.367 | 0.158 | 0.225 | #276BA6B8 |
| 桌面 | 0.334 | 0.367 | 0.168 | 0.225 | #C54834B8 |

> 以上相对坐标基于 1920×1040 工作区换算

## API

| 方法 | 功能 |
|------|------|
| `Load() *Config` | 加载配置文件，不存在则返回默认值；自动检测旧版配置并迁移 |
| `Save(cfg *Config) error` | 保存配置到 JSON 文件 |

## 旧版兼容

### 旧版分组检测
- 检查分组名是否包含"工作"/"娱乐"/"常用"（旧版默认分组名）
- 匹配 ≥ 2 个且分组数 ≤ 3 → 视为旧版配置 → 重置为默认分组

### 绝对坐标迁移
- 检测任一坐标/尺寸值 > 1.0 → 视为旧版绝对像素坐标
- 除以 1920(width)/1040(height) 转为相对坐标

## 检查清单

- [ ] 首次启动时自动创建默认配置
- [ ] 旧版配置自动升级到新版格式
- [ ] 配置文件损坏时自动恢复默认值
- [ ] 配置修改后即时持久化
