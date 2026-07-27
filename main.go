package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"

	"desktop_go/internal/app"
	"desktop_go/internal/logger"
	"desktop_go/internal/resources"
	"desktop_go/internal/ui"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
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
			// 直接写入崩溃文件，不受日志级别控制
			crashFile, err := os.OpenFile("log/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				fmt.Fprintf(crashFile, "PANIC: %v\n%s\n", r, stack)
				crashFile.Close()
			}
			// 同时尝试写入 logger（受日志级别控制，仅在 error 及以上级别写入）
			logger.Error("PANIC: %v\n%s", r, stack)
			logger.Stop()
			fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, stack)
			os.Exit(2)
		}
	}()

	// 解析命令行参数
	logLevel := flag.String("log-level", "info", "日志级别: debug / info / warn / error")
	showHelp := flag.Bool("help", false, "显示参数列表")
	flag.Parse()

	if *showHelp {
		fmt.Println("DesktopGo 桌面管理工具")
		fmt.Println()
		fmt.Println("用法: desktop_go.exe [参数]")
		fmt.Println()
		fmt.Println("参数:")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// 注册控制台 Ctrl 事件处理函数，当子进程崩溃发送信号时优雅退出
	procSetConsoleCtrlHandler.Call(
		uintptr(syscall.NewCallback(func(ctrlType uint32) uintptr {
			// CTRL_CLOSE_EVENT=2: 控制台窗口关闭（通常由子进程崩溃触发）
			// CTRL_C_EVENT=0, CTRL_BREAK_EVENT=1: 同样处理
			logger.Info("Ctrl handler received signal: ctrlType=%d, exiting gracefully", ctrlType)
			logger.Stop()
			os.Exit(0)
			return 1
		})),
		1, // Add handler
	)

	if !ensureSingleInstance() {
		// 已有实例在运行，直接退出
		os.Exit(0)
	}

	// 注册嵌入的 ico 文件系统到 ui 包
	ui.EmbeddedIcoFS = resources.GetIcoFS()

	runner, err := app.NewRunner(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 运行应用（内部包含消息循环）
	if err := runner.Run(); err != nil {
		logger.Error("运行错误: %v", err)
		logger.Stop()
		fmt.Fprintf(os.Stderr, "运行错误: %v\n", err)
		os.Exit(1)
	}

	// 正常退出时确保日志落盘
	logger.Stop()
}
