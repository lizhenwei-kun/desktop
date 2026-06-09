# 生命周期管理 (Lifecycle)

## 元信息

- **文件**: `internal/ui/window_state.go`
- **包**: `ui`
- **依赖**: `sync`

## 状态机

```
┌────────────┐    MarkReady    ┌────────────┐   MarkClosing   ┌────────────┐
│ StateUninit │ ──────────────► │ StateReady  │ ──────────────► │ StateClosing│
│  (未初始化)  │                │  (运行中)    │                │  (关闭中)    │
└────────────┘                └────────────┘                └────────────┘
```

## 核心类型

```go
type WindowLifecycle int

const (
    StateUninit  WindowLifecycle = iota
    StateReady
    StateClosing
)

type LifecycleManager struct {
    state         WindowLifecycle
    stateMu       sync.RWMutex
    onCloseFuncs  []func()
}
```

## 消息过滤规则

| 当前状态 | ShouldProcess 返回值 |
|----------|---------------------|
| StateUninit | 始终返回 false |
| StateReady | 始终返回 true |
| StateClosing | 仅放行关闭相关消息，其余返回 false |

## 关闭相关消息

| 消息 | 值 |
|------|-----|
| WM_CLOSE | 0x0010 |
| WM_DESTROY | 0x0002 |
| WM_QUIT | 0x0012 |
| WM_QUERYENDSESSION | 0x0011 |
| WM_ENDSESSION | 0x0016 |
| WM_NCDESTROY | 0x0082 |

## API

| 方法 | 功能 |
|------|------|
| `NewLifecycleManager() *LifecycleManager` | 创建管理器（初始状态 StateUninit） |
| `MarkReady()` | 标记初始化完成 → StateReady |
| `MarkClosing()` | 标记关闭开始 → StateClosing |
| `State() WindowLifecycle` | 获取当前状态 |
| `ShouldProcess(msgType uint32) bool` | 判断是否应处理消息 |
| `RegisterCleanup(fn func())` | 注册清理函数 |
| `ExecuteCleanups()` | LIFO 顺序执行所有清理函数 |

## 检查清单

- [ ] 初始化期间不处理任何系统消息
- [ ] 关闭期间仅放行关闭相关消息
- [ ] 清理函数按 LIFO 顺序执行
- [ ] 线程安全（读写锁保护）
