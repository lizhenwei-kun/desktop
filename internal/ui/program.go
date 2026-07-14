package ui

import (
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
	// 系统桌面项（shell: 前缀）
	if strings.HasPrefix(path, "shell:") {
		return pe.executeSystemItem(path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".exe" {
		return pe.executeExe(path)
	}
	return pe.openWithAssoc(path)
}

// executeSystemItem 执行系统桌面项
func (pe *ProgramExecutor) executeSystemItem(path string) error {
	clsid := systemIconCLSID(path)
	if clsid == "" {
		return nil
	}
	// 用 explorer.exe 打开系统文件夹
	cmd := exec.Command("explorer.exe", clsid)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: false,
	}
	return cmd.Start()
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