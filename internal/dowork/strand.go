package dowork

import (
	"sync"
	"sync/atomic"
)

// Op 通用操作函数
type Op func()

// Strand 串行执行器，单 goroutine 从 channel 中取 Op 依次执行
type Strand struct {
	opQueues  chan Op
	startOnce sync.Once
	closed    atomic.Bool // Stop 后标记为 true，阻止后续 Post 导致 panic
}

// NewStrand 创建串行执行器
// size: channel 缓冲区大小
func NewStrand(size int) *Strand {
	return &Strand{
		opQueues: make(chan Op, size),
	}
}

// Post 投递操作到执行队列。
// 若 Stop 之后调用，操作会被静默丢弃。
// recover 兜底 TOCTOU 竞态：检查 closed 后、send 前 channel 被关闭的可能性。
func (ex *Strand) Post(op Op) {
	if ex.closed.Load() {
		return
	}
	defer func() { recover() }()
	ex.opQueues <- op
}

// Stop 停止执行器（关闭 channel，goroutine 退出 for-range 循环）。
// 相比发送 nil 信号的方式，close 不会因 channel 满而阻塞。
func (ex *Strand) Stop() {
	ex.closed.Store(true)
	close(ex.opQueues)
}

// Start 启动执行器 goroutine，多次调用仅首次生效。
func (ex *Strand) Start() {
	ex.startOnce.Do(func() {
		go func() {
			for t := range ex.opQueues {
				t()
			}
		}()
	})
}
