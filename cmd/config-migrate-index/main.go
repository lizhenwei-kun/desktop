// config-migrate-index 工具：将旧版配置文件（含独立 ungrouped_indices 字段）迁移为新格式。
//
// 迁移内容：
//   - 删除顶层 ungrouped_indices 字段
//   - 将每个未分组项（desktop_items 中 group==""）的索引并入其 index 字段
//   - 未分组项在 ungrouped_indices 中无记录时置 index=-1（待分配）
//   - 分组项不使用 index
//
// 用法：
//   config-migrate-index [配置文件路径] [输出文件路径]
//
// 若省略参数：
//   - 输入默认 %USERPROFILE%\.desktop_go\config.json
//   - 输出默认覆盖输入文件（原文件备份为 config.json.bak）
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		fmt.Fprintln(os.Stderr, "用法: config-migrate-index [配置文件路径] [输出文件路径]")
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

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 读取旧 ungrouped_indices map
	indexMap := map[string]int{}
	if raw, ok := root["ungrouped_indices"]; ok && len(trimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &indexMap); err != nil {
			return fmt.Errorf("ungrouped_indices 解析失败: %w", err)
		}
	}

	// 解析 desktop_items 数组
	var items []ItemJSON
	if raw, ok := root["desktop_items"]; ok && len(trimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("desktop_items 解析失败: %w", err)
		}
	}

	// 解析 system_items 数组
	var sysItems []SystemItemJSON
	if raw, ok := root["system_items"]; ok && len(trimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &sysItems); err != nil {
			return fmt.Errorf("system_items 解析失败: %w", err)
		}
	}

	// 合并索引到未分组项（desktop_items）
	changed := false
	for i := range items {
		if items[i].Group == "" {
			if idx, ok := indexMap[items[i].Path]; ok {
				if items[i].Index != idx {
					items[i].Index = idx
					changed = true
				}
			} else if items[i].Index != -1 {
				items[i].Index = -1 // 待分配
				changed = true
			}
		} else if items[i].Index != 0 {
			items[i].Index = 0 // 分组项不使用 index
			changed = true
		}
	}

	// 合并索引到系统项（system_items，旧 ungrouped_indices 中以 shell: 开头的 key）
	for i := range sysItems {
		shellPath := "shell:" + sysItems[i].ID
		if idx, ok := indexMap[shellPath]; ok {
			if sysItems[i].Index != idx {
				sysItems[i].Index = idx
				changed = true
			}
		} else if sysItems[i].Index != -1 {
			sysItems[i].Index = -1 // 待分配
			changed = true
		}
	}

	// 删除旧 ungrouped_indices 字段
	if _, ok := root["ungrouped_indices"]; ok {
		delete(root, "ungrouped_indices")
		changed = true
	}

	// 写回 desktop_items
	newRaw, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("生成 desktop_items 失败: %w", err)
	}
	root["desktop_items"] = newRaw

	// 写回 system_items
	sysRaw, err := json.Marshal(sysItems)
	if err != nil {
		return fmt.Errorf("生成 system_items 失败: %w", err)
	}
	root["system_items"] = sysRaw

	outData, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("生成新配置失败: %w", err)
	}

	return writeIfNeeded(outData, outPath, inPath, changed)
}

// ItemJSON 新格式桌面项（含 index）
type ItemJSON struct {
	Path  string `json:"path"`
	Group string `json:"group"`
	Index int    `json:"index,omitempty"`
}

// SystemItemJSON 系统桌面项（含 index）
type SystemItemJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Index int    `json:"index,omitempty"`
}

func writeIfNeeded(data []byte, outPath, inPath string, changed bool) error {
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
	if changed {
		fmt.Println("迁移完成，输出到:", outPath)
	} else {
		fmt.Println("无需迁移，配置已是新格式。输出到:", outPath)
	}
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
