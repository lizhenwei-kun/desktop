# 程序执行器 (Program Executor)

## 元信息

- **文件**: `internal/ui/program.go`
- **包**: `ui`
- **依赖**: `os/exec`, `syscall`

## 核心类型

```go
type ProgramExecutor struct{}
```

## API

| 方法 | 功能 |
|------|------|
| `NewProgramExecutor() *ProgramExecutor` | 创建执行器 |
| `Execute(path string) error` | 执行程序或打开文件 |
| `GetDesktopPath() string` | 获取用户桌面路径 |
| `GetPublicDesktopPath() string` | 获取公共桌面路径 |

## 执行规则

| 文件类型 | 执行方式 |
|----------|----------|
| `.exe` | `exec.Command(path)` + `HideWindow`, `Start()` |
| 其他 | `cmd /c start "" <path>` + `HideWindow` |

## 检查清单

- [ ] .exe 程序正确启动且不显示控制台窗口
- [ ] 非 .exe 文件使用关联程序打开
- [ ] 执行失败时不阻塞 UI
