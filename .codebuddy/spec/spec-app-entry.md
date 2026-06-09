# 应用入口 (app-entry)

## 元信息

- **文件**: `main.go`
- **包**: `main`
- **依赖**: `internal/app`, `github.com/lxn/walk`, `kernel32.dll(CreateMutexW)`

## 功能概述

程序入口点，负责任务：
1. 初始化 Walk 应用上下文
2. 单实例保护（互斥体）
3. 创建 Runner 并启动主循环

## 流程

```
main()
├── walk.App().SetOrganizationName / SetProductName
├── ensureSingleInstance()
│   └── CreateMutexW("Global\\DesktopGo_SingleInstance")
│       └── 已存在 → os.Exit(0)
├── app.NewRunner()
│   ├── logger.Init
│   ├── group.NewManager → config.Load
│   ├── detectMode
│   └── manager.ReloadDesktopItems
└── runner.Run()
    ├── ModeDesktop → runDesktopMode
    └── ModeWindow → runWindowMode
```

## 边界条件

| 条件 | 行为 |
|------|------|
| 已有实例运行 | 直接静默退出 |
| NewRunner 失败 | `os.Exit(1)` 并输出错误 |
| Run 失败 | `os.Exit(1)` 并输出错误 |

## 检查清单

- [ ] 单实例保护在所有场景下正常工作
- [ ] 崩溃后能重新启动（互斥体会自动释放）
