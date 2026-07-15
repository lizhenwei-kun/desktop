package dowork

// AbsoluteTimer 绝对时间定时器，基于逻辑时间判断是否到期。
// 首次调用 IsExpire 时仅初始化 nextInv（不触发），
// 从第一个完整间隔之后才开始正常判断到期。
type AbsoluteTimer struct {
	inv     int64 // 定时间隔（毫秒）
	nextInv int64 // 下次触发时间点
}

// NewAbsoluteTimer 创建绝对时间定时器
// millisecond: 定时间隔（毫秒）
func NewAbsoluteTimer(millisecond int64) *AbsoluteTimer {
	return &AbsoluteTimer{
		inv:     millisecond,
		nextInv: 0,
	}
}

// NextInv 返回下次触发时间点（毫秒逻辑时间），供最小堆排序使用
func (t *AbsoluteTimer) NextInv() int64 {
	return t.nextInv
}

// IsExpire 判断是否到期，若到期则更新下次触发时间并返回 true。
// 首次调用时 nextInv 为 0，仅初始化时间点，返回 false。
func (t *AbsoluteTimer) IsExpire(tickData *TickData) bool {
	if t.nextInv <= 0 {
		t.nextInv = tickData.New() + t.inv
		return false
	}
	if t.nextInv > tickData.New() {
		return false
	}
	t.nextInv = tickData.New() + t.inv
	return true
}
