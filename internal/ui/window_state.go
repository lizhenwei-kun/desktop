package ui

import "sync"

// WindowLifecycle 窗口生命周期状态枚举
type WindowLifecycle int

const (
	StateUninit  WindowLifecycle = iota // 未初始化
	StateReady                          // 就绪，可正常处理消息
	StateClosing                        // 关闭中，仅处理退出相关消息
)

// 关闭相关消息类型
const (
	WM_CLOSE           uint32 = 0x0010
	WM_DESTROY         uint32 = 0x0002
	WM_QUIT            uint32 = 0x0012
	WM_QUERYENDSESSION uint32 = 0x0011
	WM_ENDSESSION      uint32 = 0x0016
	WM_NCDESTROY       uint32 = 0x0082
)

// LifecycleManager 窗口生命周期管理器
type LifecycleManager struct {
	state       WindowLifecycle
	stateMu     sync.RWMutex
	onCloseFuncs []func()
}

// NewLifecycleManager 创建生命周期管理器
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		state: StateUninit,
	}
}

// MarkReady 标记窗口初始化完成
func (lm *LifecycleManager) MarkReady() {
	lm.stateMu.Lock()
	defer lm.stateMu.Unlock()
	lm.state = StateReady
}

// MarkClosing 标记开始关闭
func (lm *LifecycleManager) MarkClosing() {
	lm.stateMu.Lock()
	defer lm.stateMu.Unlock()
	lm.state = StateClosing
}

// State 获取当前状态
func (lm *LifecycleManager) State() WindowLifecycle {
	lm.stateMu.RLock()
	defer lm.stateMu.RUnlock()
	return lm.state
}

// ShouldProcess 根据当前状态判断是否应处理该消息
func (lm *LifecycleManager) ShouldProcess(msgType uint32) bool {
	lm.stateMu.RLock()
	defer lm.stateMu.RUnlock()

	switch lm.state {
	case StateUninit:
		return false
	case StateReady:
		return true
	case StateClosing:
		return isCloseRelatedMsg(msgType)
	default:
		return false
	}
}

// RegisterCleanup 注册清理函数
func (lm *LifecycleManager) RegisterCleanup(fn func()) {
	lm.stateMu.Lock()
	defer lm.stateMu.Unlock()
	lm.onCloseFuncs = append(lm.onCloseFuncs, fn)
}

// ExecuteCleanups 按注册逆序执行所有清理函数（LIFO）
func (lm *LifecycleManager) ExecuteCleanups() {
	lm.stateMu.Lock()
	fns := make([]func(), len(lm.onCloseFuncs))
	copy(fns, lm.onCloseFuncs)
	lm.stateMu.Unlock()

	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// isCloseRelatedMsg 检查是否为关闭相关消息
func isCloseRelatedMsg(msgType uint32) bool {
	switch msgType {
	case WM_CLOSE, WM_DESTROY, WM_QUIT, WM_QUERYENDSESSION, WM_ENDSESSION, WM_NCDESTROY:
		return true
	default:
		return false
	}
}
