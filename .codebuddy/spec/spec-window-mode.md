# 窗口模式 (Window Mode)

## 元信息

- **文件**: `internal/app/runner.go` (runWindowMode / setupWindowModeUI)
- **包**: `app`

## 功能概述

标准窗口应用，以 3 列网格布局展示分组卡片。

## UI 布局

```
MainWindow (1000×700, MinSize 800×600, VBox)
├── Toolbar Composite (HBox)
│   ├── Label "DesktopGo" (Font 16 Bold)
│   └── PushButton "+ 新建分组"
│       └── Clicked → ShowInputDialog → CreateGroup → refreshWindowUI
└── CustomWidget (body, stretch=1)
    └── paintWindowModeBody
        └── 3列网格 × N
            └── paintWindowCard
                ├── 半透明背景（分组颜色）
                ├── 标题 + 分隔线
                └── 项目名称列表（无图标）
```

## 卡片布局计算

```
cols = 3
padding = 16
cardW = (areaWidth - padding*(cols+1)) / cols
cardH = 200
startY = padding

for i, grp := range groups {
    col = i % cols
    row = i / cols
    x = padding + col*(cardW+padding)
    y = startY + row*(cardH+padding)
}
```

## 交互

| 操作 | 行为 |
|------|------|
| "+ 新建分组" 点击 | 弹出输入对话框 → 创建分组 → 刷新 |
| 关闭窗口 | 隐藏到托盘 |
| 数据变更 | manager.SetOnChange → body.Invalidate |

## 检查清单

- [ ] 窗口默认 1000×700
- [ ] 3列网格正确排列
- [ ] 添加分组后正确刷新
- [ ] 最小尺寸 800×600
