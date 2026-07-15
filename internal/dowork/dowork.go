package dowork

import (
	"context"
	"sync/atomic"
)

var (
	defaultTickTime = 500 // 默认心跳周期 500ms
	defaultRecvSize = 128 // 默认 channel 缓冲区大小
)

// GoWork 顶层工作入口，组合 Strand 和 StrandTimer。
//
// 使用顺序：
//  1. NewGoWork / NewGoWorkMilli / NewGoWorkWithContext 创建
//  2. AddTimer / Post 添加操作（Post 仅在 Run 之后可安全使用）
//  3. Run 启动（幂等）
//  4. Stop 停止（幂等）
type GoWork struct {
	timer  *StrandTimer
	strand *Strand
	isStop atomic.Bool
}

func newGoWork(size, tickTime int, ctx context.Context) *GoWork {
	work := &GoWork{}
	work.strand = NewStrand(size)
	work.timer = NewStrandTimer(tickTime, ctx, work.strand)
	return work
}

// NewGoWork 创建 GoWork，默认心跳 500ms，使用 context.Background()
func NewGoWork() *GoWork {
	return newGoWork(defaultRecvSize, defaultTickTime, context.Background())
}

// NewGoWorkMilli 创建 GoWork，自定义心跳周期
// tickTime: 毫秒
func NewGoWorkMilli(tickTime int) *GoWork {
	return newGoWork(defaultRecvSize, tickTime, context.Background())
}

// NewGoWorkWithContext 创建 GoWork，自定义心跳周期和 context
// tickTime: 毫秒；可通过 context 统一取消
func NewGoWorkWithContext(tickTime int, ctx context.Context) *GoWork {
	return newGoWork(defaultRecvSize, tickTime, ctx)
}

// Post 投递操作到串行执行队列。
// 注意：在 Run() 之前调用可能因 channel 满而阻塞。
func (w *GoWork) Post(op Op) {
	w.strand.Post(op)
}

// Stop 停止定时器和串行执行器（幂等，可安全重复调用）。
// 先停 timer goroutine，再关闭 strand channel，保证不会向已关闭的 channel 投递。
func (w *GoWork) Stop() {
	if w.isStop.Load() {
		return
	}
	w.isStop.Store(true)
	w.timer.Stop()
	w.strand.Stop()
}

// Run 启动串行执行器和定时器（幂等，可安全重复调用）。
func (w *GoWork) Run() {
	w.strand.Start()
	w.timer.Start()
}

// AddTimer 添加定时器
// inv: 定时间隔（毫秒），必须 >= StrandTimer 的 tick 周期
// fn: 定时器回调函数
func (w *GoWork) AddTimer(inv int, fn Op) {
	w.timer.AddTimer(inv, fn)
}
