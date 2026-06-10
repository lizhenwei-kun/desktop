package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"

	"desktop_go/internal/app"
	"desktop_go/internal/logger"
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
	// 全局 panic 捕获，写入日志文件
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			// 尝试写入日志
			logger.Error("PANIC: %v\n%s", r, stack)
			logger.Sync()
			// 同时写入崩溃文件（防止 logger 未初始化）
			crashFile, err := os.OpenFile("log/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				fmt.Fprintf(crashFile, "PANIC: %v\n%s\n", r, stack)
				crashFile.Close()
			}
			fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, stack)
			os.Exit(2)
		}
	}()

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
