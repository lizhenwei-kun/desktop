package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
	Name      string   `json:"name"`
	Position  Position `json:"position"`
	Size      Size     `json:"size"`
	Color     string   `json:"color"`
	Collapsed bool     `json:"collapsed,omitempty"` // 是否收缩（只显示标题栏）
}

// SystemItem 系统桌面项（如"此电脑"）
type SystemItem struct {
	ID    string `json:"id"`    // 唯一标识，如 "MyComputer"
	Name  string `json:"name"`  // 显示名称
	Index int    `json:"index,omitempty"` // 网格索引（列优先），-1 表示待分配
}

// DesktopItem 桌面项（文件）
type DesktopItem struct {
	Path  string `json:"path"`  // 文件完整路径
	Group string `json:"group"` // 所属分组名，"" 表示未分组
	Index int    `json:"index,omitempty"` // 未分组项的网格索引（列优先：从上到下，再从左到右），-1 表示待分配；分组项不使用该字段
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
	Groups            []Group           `json:"groups"`
	DesktopItems      []DesktopItem     `json:"desktop_items"`   // 桌面项有序切片（保持配置中的顺序）
	SystemItems       []SystemItem      `json:"system_items"`    // 系统桌面项
	CardFontName      string            `json:"card_font_name"`
	CardFontSize      int               `json:"card_font_size"`
	CardFontPreset    string            `json:"card_font_preset"` // 预设: "consolas" / "segoeui" / "yahei" / "custom"
	IconFontName      string            `json:"icon_font_name"`
	IconFontSize      int               `json:"icon_font_size"`
	IconSizeLevel     int               `json:"icon_size_level"`  // 桌面图标大小档位: 0=大(48) 1=中(48) 2=小(32)
	AutoArrange       bool              `json:"auto_arrange"`     // 是否自动排列图标
	AlignToGrid       bool              `json:"align_to_grid"`    // 是否将图标与网格对齐
	GuideLineColor    string            `json:"guide_line_color"` // 卡片拖拽参考线颜色（#RRGGBBAA），默认红色
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
		Groups:         DefaultGroups(),
		DesktopItems:   []DesktopItem{},
		CardFontName:   "Consolas",
		CardFontSize:     14,
		CardFontPreset:   "consolas",
		IconFontName:     "宋体",
		IconFontSize:     11,
		IconSizeLevel:    1,  // 默认中档
		AutoArrange:      false,
		AlignToGrid:      false,
		GuideLineColor:   "#FF0000FF", // 默认红色
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}

	// desktop_items 兼容迁移：
	// 旧版格式为对象 {路径: 分组名}，新版格式为数组 [{path, group}, ...]。
	// 由于类型不同（map vs slice），直接整体 Unmarshal 会在旧格式下失败导致配置丢失。
	// 因此先从原始 JSON 中剥离 desktop_items，主体正常解析，再单独处理 desktop_items。
	var probe struct {
		DesktopItems json.RawMessage `json:"desktop_items"`
	}
	_ = json.Unmarshal(data, &probe)

	// 构造去除 desktop_items 的 JSON，避免类型不匹配导致整体解析失败
	filtered := filterOutKey(data, "desktop_items")

	loaded := &Config{}
	if err := json.Unmarshal(filtered, loaded); err != nil {
		return cfg
	}

	if len(probe.DesktopItems) > 0 {
		trimmed := bytes.TrimSpace(probe.DesktopItems)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			// 旧版对象格式 {路径: 分组名}，迁移为切片
			var legacyDesktopItems map[string]string
			if json.Unmarshal(probe.DesktopItems, &legacyDesktopItems) == nil {
				items := make([]DesktopItem, 0, len(legacyDesktopItems))
				// 排序保证顺序稳定
				paths := make([]string, 0, len(legacyDesktopItems))
				for p := range legacyDesktopItems {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				for _, p := range paths {
					items = append(items, DesktopItem{Path: p, Group: legacyDesktopItems[p]})
				}
				loaded.DesktopItems = items
			}
		} else if len(trimmed) > 0 && trimmed[0] == '[' {
			// 新版数组格式，直接解析
			var items []DesktopItem
			if json.Unmarshal(probe.DesktopItems, &items) == nil {
				loaded.DesktopItems = items
			}
		}
	}

	// 验证加载的配置
	if len(loaded.Groups) == 0 {
		loaded.Groups = DefaultGroups()
	}
	if loaded.DesktopItems == nil {
		loaded.DesktopItems = []DesktopItem{}
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

// filterOutKey 从 JSON 中移除指定顶层 key（用于剥离类型不兼容的字段后再整体解析）。
// data 必须是 JSON 对象；若不是对象则原样返回。
func filterOutKey(data []byte, key string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}
	if _, ok := m[key]; !ok {
		return data
	}
	delete(m, key)
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
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
