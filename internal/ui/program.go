package ui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"desktop_go/internal/logger"
	"desktop_go/internal/safego"
)

// Windows 进程创建标志
const (
	CREATE_NEW_CONSOLE      = 0x00000010
	CREATE_NEW_PROCESS_GROUP = 0x00000200
)

// ProgramExecutor 程序执行器
type ProgramExecutor struct{}

// NewProgramExecutor 创建程序执行器
func NewProgramExecutor() *ProgramExecutor {
	return &ProgramExecutor{}
}

// Execute 执行程序或打开文件（在后台 goroutine 中异步执行，不阻塞 UI 主线程）
func (pe *ProgramExecutor) Execute(path string) error {
	logger.Debug("ProgramExecutor.Execute: path=%q", path)
	safego.Go("executeAsync", func() { pe.executeAsync(path) })
	return nil
}

// executeAsync 在后台 goroutine 中执行程序
func (pe *ProgramExecutor) executeAsync(path string) {
	logger.Debug("ProgramExecutor.executeAsync: path=%q goroutine start", path)
	defer logger.Debug("ProgramExecutor.executeAsync: path=%q goroutine end", path)

	// 系统桌面项（shell: 前缀）
	if strings.HasPrefix(path, "shell:") {
		pe.executeSystemItem(path)
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".exe" {
		pe.executeExe(path)
		return
	}
	pe.openWithAssoc(path)
}

// executeSystemItem 执行系统桌面项
func (pe *ProgramExecutor) executeSystemItem(path string) {
	clsid := systemIconCLSID(path)
	logger.Debug("ProgramExecutor.executeSystemItem: path=%q clsid=%q", path, clsid)
	if clsid == "" {
		return
	}
	cmd := exec.Command("explorer.exe", clsid)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: false,
	}
	if err := cmd.Start(); err != nil {
		logger.Warn("ProgramExecutor.executeSystemItem: cmd.Start failed: %v", err)
		return
	}
	logger.Debug("ProgramExecutor.executeSystemItem: cmd.Start ok")
}

// executeExe 直接启动 exe 程序
func (pe *ProgramExecutor) executeExe(path string) {
	logger.Debug("ProgramExecutor.executeExe: path=%q", path)
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NEW_CONSOLE | CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		logger.Warn("ProgramExecutor.executeExe: cmd.Start failed: %v", err)
		return
	}
	logger.Debug("ProgramExecutor.executeExe: cmd.Start ok")
}

// openWithAssoc 使用关联程序打开文件
func (pe *ProgramExecutor) openWithAssoc(path string) {
	logger.Debug("ProgramExecutor.openWithAssoc: path=%q", path)
	cmd := exec.Command("cmd", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: CREATE_NEW_CONSOLE | CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		logger.Warn("ProgramExecutor.openWithAssoc: cmd.Start failed: %v", err)
		return
	}
	logger.Debug("ProgramExecutor.openWithAssoc: cmd.Start ok")
}
