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