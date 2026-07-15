package dowork

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type execTimer struct {
	timer *AbsoluteTimer
	fn    Op
}

// timerHeap 最小堆，按 nextInv 排序，每次只检查最早到期的定时器
type timerHeap []*execTimer

func (h timerHeap) Len() int            { return len(h) }
func (h timerHeap) Less(i, j int) bool  { return h[i].timer.NextInv() < h[j].timer.NextInv() }
func (h timerHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *timerHeap) Push(x interface{}) { *h = append(*h, x.(*execTimer)) }
func (h *timerHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// GoPost 投递操作接口
type GoPost interface {
	Post(op Op)
}

// StrandTimer 定时器驱动器，周期性 tick 驱动定时器检查。
// 定时器存储采用最小堆，每次 tick 只检查最早到期的定时器，O(logN) 操作。
type StrandTimer struct {
	sync.WaitGroup
	timerHeap timerHeap     // 最小堆，按下次触发时间排序
	ctx       context.Context
	tickTime  int           // 心跳 tick 周期（毫秒）
	isStop    atomic.Bool   // 是否已停止
	strand    GoPost        // 投递操作的 Strand
	stopOp    chan any
	startOnce sync.Once
	stopOnce  sync.Once     // 保证 Stop 只执行一次，防止并发死锁
}

// NewStrandTimer 创建定时器驱动器
// tickTime: 心跳周期（毫秒）
func NewStrandTimer(tickTime int, ctx context.Context, strand GoPost) *StrandTimer {
	return &StrandTimer{
		tickTime: tickTime,
		stopOp:   make(chan any, 1),
		strand:   strand,
		ctx:      ctx,
	}
}

// Start 启动定时器 goroutine，多次调用仅首次生效
func (w *StrandTimer) Start() {
	w.startOnce.Do(func() {
		w.Add(1)
		go func() {
			defer w.Done()
			w.loop()
			w.isStop.Store(true)
		}()
	})
}

// Stop 停止定时器（幂等，可安全重复调用，多个 goroutine 并发也安全）
func (w *StrandTimer) Stop() {
	w.stopOnce.Do(func() {
		w.stopOp <- nil
		w.Wait()
	})
}

func (w *StrandTimer) loop() {
	tick := time.NewTicker(time.Millisecond * time.Duration(w.tickTime))
	defer tick.Stop()

	tickData := NewTickData()

	for {
		select {
		case <-w.stopOp:
			return
		case <-w.ctx.Done():
			return
		case <-tick.C:
			tickData.Update()
			w.onUpdate(*tickData)
		}
	}
}

// onceRun 使用最小堆检查到期定时器，O(logN) per expired timer。
// 注意：
//   - IsExpire 有副作用（更新 nextInv），必须在 heap.Pop 之后调用，
//     否则堆排序依据的 NextInv() 返回值可能过时。
//   - nextInv <= 0 表示新定时器尚未初始化，首次 IsExpire 仅设初值不触发回调，
//     处理完后 continue 让后续定时器也有机会在同一次 tick 中初始化。
func (w *StrandTimer) onceRun(tickData *TickData) {
	for w.timerHeap.Len() > 0 {
		et := heap.Pop(&w.timerHeap).(*execTimer)

		// 新定时器（未初始化），仅设 nextInv 初值，不触发回调
		if et.timer.NextInv() <= 0 {
			et.timer.IsExpire(tickData)
			heap.Push(&w.timerHeap, et)
			continue
		}

		if !et.timer.IsExpire(tickData) {
			// 堆顶未到期，后续更大 nextInv 的定时器也必然未到期
			heap.Push(&w.timerHeap, et)
			break
		}

		et.fn()
		// IsExpire 已更新 nextInv，重新入堆
		heap.Push(&w.timerHeap, et)
	}
}

func (w *StrandTimer) onUpdate(tickData TickData) {
	w.strand.Post(func() {
		w.onceRun(&tickData)
	})
}

// AddTimer 添加定时器
// inv: 定时间隔（毫秒）
func (w *StrandTimer) AddTimer(inv int, fn Op) {
	w.strand.Post(func() {
		heap.Push(&w.timerHeap, &execTimer{timer: NewAbsoluteTimer(int64(inv)), fn: fn})
	})
}

// Post 通过 strand 投递操作
func (w *StrandTimer) Post(op Op) {
	w.strand.Post(op)
}
