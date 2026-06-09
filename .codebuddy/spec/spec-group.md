# 分组管理 (Group)

## 元信息

- **文件**: `internal/group/manager.go`
- **包**: `group`
- **依赖**: `config`, `os`, `path/filepath`

## 核心类型

```go
type Manager struct {
    cfg      *config.Config
    mu       sync.RWMutex
    onChange func()
}

type GroupItem struct {
    Path string // 文件完整路径
    Name string // 显示名称（不含扩展名）
}

type desktopItemInfo struct {
    Path  string
    Name  string
    IsDir bool
}
```

## API 清单

| 方法 | 功能 | 锁 |
|------|------|-----|
| `GetGroups() []config.Group` | 获取所有分组 | RLock |
| `GetGroupItems(groupName) []GroupItem` | 获取指定分组的所有项目 | RLock |
| `GetUngroupedItems() []GroupItem` | 获取未分组项目 | RLock |
| `CreateGroup(name, color)` | 创建新分组（重名检测） | Lock |
| `DeleteGroup(name)` | 删除分组，项目移回未分组 | Lock |
| `RenameGroup(oldName, newName)` | 重命名分组 + 更新项目映射 | Lock |
| `AddItemToGroup(groupName, itemPath, itemName)` | 添加项目到分组 | Lock |
| `RemoveItemFromGroup(groupName, itemPath)` | 从分组移除项目 | Lock |
| `MoveItemToGroup(itemPath, groupName)` | 移动项目到指定分组 | Lock |
| `MoveItemToDesktop(itemPath)` | 移出分组到桌面区域 | Lock |
| `UpdateGroupPosition(name, x, y)` | 更新分组位置 | Lock |
| `UpdateGroupSize(name, w, h)` | 更新分组尺寸 | Lock |
| `UpdateGroupColor(name, color)` | 更新分组颜色 | Lock |
| `ReloadDesktopItems()` | 从桌面目录同步（查漏补缺） | Lock |
| `SetOnChange(fn)` | 设置变更回调 | Lock |

## 自动分类规则 (groupForPath)

| 条件 | 分组 |
|------|------|
| 扩展名 .lnk / .url / .exe | 快捷方式 |
| isDir == true | 备份文件 |
| 扩展名 .doc/.docx/.txt/.pdf/.xls/.xlsx/.ppt/.pptx/.rtf | Word |
| 扩展名 .png/.jpg/.jpeg/.gif/.bmp/.ico/.svg/.webp | 图片 |
| 名称包含"快捷" | 快捷方式 |
| 名称包含"文件"/"备份" | 备份文件 |
| 名称包含"word"/"文档" | Word |
| 名称包含"图片" | 图片 |
| 名称包含"桌面" | 桌面 |
| 兜底 | 桌面 |

## 桌面同步流程

```
ReloadDesktopItems()
├── collectDesktopPaths() 扫描两个桌面目录
│   ├── %USERPROFILE%\Desktop
│   └── C:\Users\Public\Desktop
│   └── 跳过 desktop.ini
├── 清理: 移除 DesktopItems 中已不存在的文件记录
├── 新增: 对新文件通过 groupForPath 自动分类
└── Save + notifyChange
```

## 检查清单

- [ ] 重命名分组时同步更新项目映射
- [ ] 删除分组时项目不会丢失（回到未分组）
- [ ] 启动时 ReloadDesktopItems 正确同步
- [ ] 并发安全（读写锁覆盖所有操作）
