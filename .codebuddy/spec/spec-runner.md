# 运行器 (Runner)

## 元信息

- **文件**: `internal/app/runner.go`
- **包**: `app`
- **依赖**: `group`, `config`, `desktop`, `ui`, `logger`, `github.com/lxn/walk`

## 功能概述

应用运行器，负责任务：
1. 模式检测（命令行 / 环境变量）
2. 桌面模式全流程（创建窗口、去边框、系统托盘）
3. 窗口模式全流程（创建窗口、网格卡片）
4. 系统托盘管理

## 模式检测规则

```
detectMode()
├── 命令行参数（最高优先级）
│   ├── --desktop / -d → ModeDesktop
│   └── --window / -w → ModeWindow
├── 环境变量
│   └── DESKTOPGO_MODE=desktop → ModeDesktop
└── 默认
    └── ModeWindow
```

## 桌面模式流程

```
runDesktopMode()
├── winAPI.GetWorkAreaRect → workW, workH
├── MainWindow{Size: workW×workH, VBox layout}
├── Closing事件 → 隐藏到托盘（canceled=true）
├── ui.NewDesktopMode(mw, manager, winAPI, lifecycle)
├── dm.Setup()
├── setupNotifyIcon()
├── manager.SetOnChange → dm.Refresh
├── lifecycle.RegisterCleanup → ni.Dispose
└── mw.Run()
```

## 窗口模式流程

```
runWindowMode()
├── MainWindow{Size: 1000×700, MinSize: 800×600}
├── setupWindowModeUI(mw)
│   ├── toolbar: 标题 + "+ 新建分组" 按钮
│   ├── body: CustomWidget 绘制网格卡片
│   └── manager.SetOnChange → body.Invalidate
├── setupNotifyIcon()
├── Closing → 隐藏到托盘
├── lifecycle.MarkReady()
└── mw.Run()
```

## 系统托盘规格

| 功能 | 实现 |
|------|------|
| 右键菜单 | 显示/隐藏 + 分隔线 + 退出 |
| 关闭窗口 | 隐藏到托盘（不退出） |
| 双击托盘 | 显示/隐藏切换 |
| 托盘图标 | 蓝色圆形 + 白色网格 (16×16 ICO) |
| 图标来源 | 优先 exe 嵌入资源(rsrc ID 7) → 文件回退 → IconApplication |

## 窗口模式卡片绘制

```
paintWindowModeBody()
├── 3列网格布局
├── padding=16
├── cardW = (areaWidth - 64) / 3
├── cardH = 200
└── paintWindowCard() × N
    ├── 半透明背景（分组颜色 + alpha）
    ├── 标题 + 分隔线
    └── 项目列表（名称，无图标）
```

## 检查清单

- [ ] 桌面模式窗口完全覆盖工作区，无白边
- [ ] 窗口模式 1000×700 默认尺寸
- [ ] 系统托盘右键菜单正常工作
- [ ] 关闭窗口隐藏到托盘
- [ ] 退出按钮正确清理资源
