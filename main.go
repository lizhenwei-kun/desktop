package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"

	"desktop_go/internal/app"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

const ERROR_ALREADY_EXISTS = 183

func init() {
	// 初始化 Walk 应用
	walk.App().SetOrganizationName("DesktopGo")
	walk.App().SetProductName("DesktopGo")
}

// ensureSingleInstance 确保只运行一个实例
func ensureSingleInstance() bool {
	mutexName, _ := syscall.UTF16PtrFromString("Global\\DesktopGo_SingleInstance")
	_, _, err := procCreateMutexW.Call(
		0,
		0,
		uintptr(unsafe.Pointer(mutexName)),
	)
	return err.(syscall.Errno) != ERROR_ALREADY_EXISTS
}

func main() {
	if !ensureSingleInstance() {
		// 已有实例在运行，直接退出
		os.Exit(0)
	}

	runner, err := app.NewRunner()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 运行应用（内部包含消息循环）
	if err := runner.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行错误: %v\n", err)
		os.Exit(1)
	}
}
