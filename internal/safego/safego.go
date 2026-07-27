package safego

import (
	"fmt"
	"os"
	"runtime/debug"

	"desktop_go/internal/logger"
)

// Go 安全启动一个 goroutine，panic 时记录日志并终止进程。
// name: goroutine 名称（用于日志标识）
// fn: 要执行的函数
//
// 用法：
//
//	safego.Go("myTask", func() {
//		// ... 你的代码 ...
//	})
func Go(name string, fn func()) {
	go func() {
		defer recoverGoroutine(name)
		fn()
	}()
}

// recoverGoroutine 捕获 panic 并终止进程
func recoverGoroutine(name string) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())

		// 写入 logger（会异步写到 chan，需要 Stop 确保落盘）
		logger.Error("PANIC in goroutine [%s]: %v\n%s", name, r, stack)

		// 写入 crash.log 作为兜底
		crashFile, err := os.OpenFile("log/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			fmt.Fprintf(crashFile, "PANIC in goroutine [%s]: %v\n%s\n", name, r, stack)
			crashFile.Close()
		}

		// 确保所有日志落盘后终止进程
		logger.Stop()
		os.Exit(2)
	}
}
