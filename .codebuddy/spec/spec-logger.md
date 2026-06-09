# 日志封装 (Logger)

## 元信息

- **文件**: `internal/logger/logger.go`
- **包**: `logger`
- **底层库**: `go.uber.org/zap`

## 设计目标

封装第三方日志库 `zap`，对外提供统一接口，便于后续切换底层实现。

## 初始化

在 `NewRunner()` 中调用，早于其他所有模块初始化：

```go
logger.Init("debug", "./log/desktop_go.log")
```

## API

| 方法 | 签名 | 说明 |
|------|------|------|
| `Init` | `Init(level, filePath string)` | 初始化日志系统，level: debug/info/warn/error |
| `Sync` | `Sync()` | 刷新日志缓冲，程序退出前调用 |
| `Debug` | `Debug(format string, args ...interface{})` | Debug 级别 |
| `Info` | `Info(format string, args ...interface{})` | Info 级别 |
| `Warn` | `Warn(format string, args ...interface{})` | Warn 级别 |
| `Error` | `Error(format string, args ...interface{})` | Error 级别 |

## 配置

| 配置项 | 值 | 说明 |
|--------|-----|------|
| 日志级别 | debug | 开发阶段使用 debug |
| 输出目录 | `./log/` | 可执行文件同级的 log 目录 |
| 输出文件 | `desktop_go.log` | O_TRUNC 模式每次启动覆盖 |
| 编码格式 | Console | 人类可读格式 |
| 时间格式 | `15:04:05.000` | 精确到毫秒 |
| Caller | 启用 | CallerSkip=1 跳过封装层 |

## 使用规范

| 级别 | 使用场景 |
|------|----------|
| Debug | 开发调试信息（壁纸加载路径、窗口尺寸、组件状态等） |
| Info | 关键业务事件（模式切换、配置加载完成等） |
| Warn | 可恢复的异常（文件不存在使用 fallback、路径获取失败等） |
| Error | 不可恢复的错误（致命配置错误、资源加载失败等） |

## 检查清单

- [ ] 全项目统一使用 logger 包，不使用 fmt.Println / log.Printf
- [ ] 日志文件每次启动覆盖（不追加）
- [ ] 日志时间精确到毫秒
- [ ] 切换底层库只需修改 logger.go 内部实现
