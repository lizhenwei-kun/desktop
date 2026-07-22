package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Position 表示位置坐标（相对坐标，0.0~1.0 表示在工作区中的比例）
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Size 表示尺寸（相对坐标，0.0~1.0 表示占工作区的比例）
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Group 表示分组
type Group struct {
	Name     string   `json:"name"`
	Position Position `json:"position"`
	Size     Size     `json:"size"`
	Color    string   `json:"color"`
}

// SystemItem 系统桌面项（如"此电脑"）
type SystemItem struct {
	ID   string `json:"id"`   // 唯一标识，如 "MyComputer"
	Name string `json:"name"` // 显示名称
}

// CardFontPresets 卡片标题字体预设
// key 为 preset 名（小写），value 为 {字体名, 字号}
var CardFontPresets = map[string]struct {
	Name string
	Size int
}{
	"consolas": {"Consolas", 14},           // 等宽感
	"segoeui":  {"Segoe UI", 13},           // 拉丁感
	"yahei":    {"Microsoft YaHei UI", 13}, // 中文现代感
}

// Config 表示应用配置
type Config struct {
	Groups             []Group           `json:"groups"`
	DesktopItems       map[string]string `json:"desktop_items"`        // 桌面项路径 -> 分组名
	UngroupedPositions map[string]Position `json:"ungrouped_positions"` // 未分组项路径 -> 相对位置
	SystemItems        []SystemItem      `json:"system_items"`         // 系统桌面项
	CardFontName       string            `json:"card_font_name"`
	CardFontSize       int               `json:"card_font_size"`
	CardFontPreset     string            `json:"card_font_preset"` // 预设: "consolas" / "segoeui" / "yahei" / "custom"
	IconFontName       string            `json:"icon_font_name"`
	IconFontSize       int               `json:"icon_font_size"`
}

// DefaultGroups 返回默认分组配置（相对坐标，基于1920x1040工作区计算的比例）
func DefaultGroups() []Group {
	return []Group{
		{Name: "快捷方式", Position: Position{0.017, 0.079}, Size: Size{0.156, 0.563}, Color: "#342333B8"},
		{Name: "备份文件", Position: Position{0.175, 0.079}, Size: Size{0.158, 0.225}, Color: "#A783BEB8"},
		{Name: "Word", Position: Position{0.334, 0.079}, Size: Size{0.168, 0.225}, Color: "#24A892B8"},
		{Name: "图片", Position: Position{0.175, 0.367}, Size: Size{0.158, 0.225}, Color: "#276BA6B8"},
		{Name: "桌面", Position: Position{0.334, 0.367}, Size: Size{0.168, 0.225}, Color: "#C54834B8"},
	}
}

// configDir 返回配置文件目录
func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".desktop_go")
}

// configPath 返回配置文件路径
func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

// Load 加载配置，不存在则使用默认值
func Load() *Config {
	cfg := &Config{
		Groups:             DefaultGroups(),
		DesktopItems:       make(map[string]string),
		UngroupedPositions: make(map[string]Position),
		CardFontName:       "Consolas",
		CardFontSize:       14,
		CardFontPreset:     "consolas",
		IconFontName:       "宋体",
		IconFontSize:       11,
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}

	loaded := &Config{}
	if err := json.Unmarshal(data, loaded); err != nil {
		return cfg
	}

	// 验证加载的配置
	if len(loaded.Groups) == 0 {
		loaded.Groups = DefaultGroups()
	}
	if loaded.DesktopItems == nil {
		loaded.DesktopItems = make(map[string]string)
	}
	if loaded.UngroupedPositions == nil {
		loaded.UngroupedPositions = make(map[string]Position)
	}
	if loaded.SystemItems == nil {
		loaded.SystemItems = []SystemItem{}
	}

	// 识别旧版默认配置并升级
	if isOldConfig(loaded) {
		loaded.Groups = DefaultGroups()
	}

	// 迁移旧版绝对像素坐标到相对坐标（旧版坐标值 > 1.0）
	migrateAbsoluteToRelative(loaded)

	return loaded
}

// migrateAbsoluteToRelative 将旧版绝对像素坐标迁移为相对坐标
// 旧版坐标值为像素值（如 32, 82, 300 等），大于 1.0
// 新版坐标值为 0.0~1.0 的比例值
func migrateAbsoluteToRelative(cfg *Config) {
	// 假设旧版基于 1920x1040 的工作区
	const refW = 1920.0
	const refH = 1040.0

	needMigrate := false
	for _, g := range cfg.Groups {
		if g.Position.X > 1.0 || g.Position.Y > 1.0 || g.Size.Width > 1.0 || g.Size.Height > 1.0 {
			needMigrate = true
			break
		}
	}

	if !needMigrate {
		return
	}

	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		if g.Position.X > 1.0 {
			g.Position.X = g.Position.X / refW
		}
		if g.Position.Y > 1.0 {
			g.Position.Y = g.Position.Y / refH
		}
		if g.Size.Width > 1.0 {
			g.Size.Width = g.Size.Width / refW
		}
		if g.Size.Height > 1.0 {
			g.Size.Height = g.Size.Height / refH
		}
	}
}

// Save 保存配置到文件
func Save(cfg *Config) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath(), data, 0644)
}

// isOldConfig 检查是否为旧版配置
func isOldConfig(cfg *Config) bool {
	oldNames := map[string]bool{"工作": true, "娱乐": true, "常用": true}
	matchCount := 0
	for _, g := range cfg.Groups {
		if oldNames[g.Name] {
			matchCount++
		}
	}
	return matchCount >= 2 && len(cfg.Groups) <= 3
}
