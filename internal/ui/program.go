package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ProgramExecutor 程序执行器
type ProgramExecutor struct{}

// NewProgramExecutor 创建程序执行器
func NewProgramExecutor() *ProgramExecutor {
	return &ProgramExecutor{}
}

// Execute 执行程序或打开文件
func (pe *ProgramExecutor) Execute(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".exe" {
		return pe.executeExe(path)
	}
	return pe.openWithAssoc(path)
}

// executeExe 直接启动 exe 程序
func (pe *ProgramExecutor) executeExe(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return cmd.Start()
}

// openWithAssoc 使用关联程序打开文件
func (pe *ProgramExecutor) openWithAssoc(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return cmd.Start()
}

// GetDesktopPath 获取桌面路径
func GetDesktopPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Desktop")
}

// GetPublicDesktopPath 获取公共桌面路径
func GetPublicDesktopPath() string {
	public := os.Getenv("PUBLIC")
	if public == "" {
		public = `C:\Users\Public`
	}
	return filepath.Join(public, "Desktop")
}

// CollectDesktopPaths 收集桌面目录中的所有文件路径
func CollectDesktopPaths() []DesktopItem {
	var items []DesktopItem

	desktopPaths := []string{GetDesktopPath(), GetPublicDesktopPath()}

	for _, desktopDir := range desktopPaths {
		entries, err := os.ReadDir(desktopDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			// 跳过 desktop.ini
			if strings.EqualFold(name, "desktop.ini") {
				continue
			}
			fullPath := filepath.Join(desktopDir, name)
			items = append(items, DesktopItem{
				Path:  fullPath,
				Name:  strings.TrimSuffix(name, filepath.Ext(name)),
				IsDir: entry.IsDir(),
			})
		}
	}

	return items
}
