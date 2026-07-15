package dowork

import "time"

func nowTimeUnix() int64 {
	return time.Now().UnixMilli()
}

func NowTimeUnixSeconds() int64 {
	return time.Now().Unix()
}

// TickData 时间滴答数据，记录逻辑时间
type TickData struct {
	beginTime int64 // 起始时间（毫秒）
	offset    int64 // 距起始时间的偏移量
}

// NewTickData 创建 TickData
func NewTickData() *TickData {
	return &TickData{
		beginTime: nowTimeUnix(),
		offset:    0,
	}
}

// Update 更新偏移量
func (t *TickData) Update() {
	t.offset = nowTimeUnix() - t.beginTime
}

// Init 重新初始化起始时间
func (t *TickData) Init() {
	t.beginTime = nowTimeUnix()
}

// GetOffset 获取偏移量（毫秒）
func (t *TickData) GetOffset() int64 {
	return t.offset
}

// GetOffsetSecond 获取偏移量（秒）
func (t *TickData) GetOffsetSecond() int64 {
	return t.offset / 1000
}

// New 获取当前逻辑时间（毫秒）
func (t *TickData) New() int64 {
	return t.beginTime + t.offset
}

// NewSecond 获取当前逻辑时间（秒）
func (t *TickData) NewSecond() int64 {
	return (t.beginTime + t.offset) / 1000
}
