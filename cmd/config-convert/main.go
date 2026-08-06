// config-convert 工具：将旧版配置文件（desktop_items 为对象 {路径:分组名}）转换为新版格式（数组 [{path,group}]）。
//
// 用法：
//   config-convert [旧配置文件路径] [新配置文件路径]
//
// 若省略参数：
//   - 输入默认 %USERPROFILE%\.desktop_go\config.json
//   - 输出默认覆盖输入文件（原文件备份为 config.json.bak）
//
// 说明：工具只在 desktop_items 为旧版对象格式时执行转换；若已是新版数组格式则原样输出。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	var inPath, outPath string
	switch len(os.Args) {
	case 3:
		inPath = os.Args[1]
		outPath = os.Args[2]
	case 2:
		inPath = os.Args[1]
		outPath = inPath
	case 1:
		home, _ := os.UserHomeDir()
		inPath = filepath.Join(home, ".desktop_go", "config.json")
		outPath = inPath
	default:
		fmt.Fprintln(os.Stderr, "用法: config-convert [旧配置文件路径] [新配置文件路径]")
		os.Exit(2)
	}

	if err := run(inPath, outPath); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath string) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析顶层为 map，定位 desktop_items 字段
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}

	rawDI, hasDI := root["desktop_items"]
	if !hasDI {
		fmt.Println("未找到 desktop_items 字段，无需转换。")
		return writeIfNeeded(data, outPath, inPath)
	}

	trimmed := trimSpace(rawDI)
	if len(trimmed) == 0 {
		fmt.Println("desktop_items 为空，无需转换。")
		return writeIfNeeded(data, outPath, inPath)
	}

	// 已是新版数组格式，直接返回
	if trimmed[0] == '[' {
		fmt.Println("desktop_items 已是新版数组格式，无需转换。")
		return writeIfNeeded(data, outPath, inPath)
	}

	// 旧版对象格式 {路径: 分组名} -> 数组 [{path,group}]
	var legacy map[string]string
	if err := json.Unmarshal(rawDI, &legacy); err != nil {
		return fmt.Errorf("desktop_items 对象解析失败: %w", err)
	}
	// 按路径排序，保证转换后顺序稳定
	paths := make([]string, 0, len(legacy))
	for p := range legacy {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	items := make([]DesktopItemJSON, 0, len(paths))
	for _, p := range paths {
		items = append(items, DesktopItemJSON{Path: p, Group: legacy[p]})
	}

	newRawDI, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("生成新 desktop_items 失败: %w", err)
	}
	root["desktop_items"] = newRawDI

	outData, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("生成新配置失败: %w", err)
	}

	return writeIfNeeded(outData, outPath, inPath)
}

// DesktopItemJSON 新格式的桌面项结构（与 config.DesktopItem 字段一致）
type DesktopItemJSON struct {
	Path  string `json:"path"`
	Group string `json:"group"`
}

// writeIfNeeded 输出到文件；若输出路径与输入相同则先备份原文件
func writeIfNeeded(data []byte, outPath, inPath string) error {
	if outPath == inPath {
		bak := inPath + ".bak"
		if err := os.WriteFile(bak, data, 0644); err != nil {
			return fmt.Errorf("备份失败: %w", err)
		}
		fmt.Println("已备份原配置到:", bak)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}
	fmt.Println("转换完成，输出到:", outPath)
	return nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}
