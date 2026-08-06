package group

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"desktop_go/internal/config"
)

// selfExePath 缓存程序自身可执行文件路径（规范化），运行时初始化
var selfExePath string

func init() {
	// 获取并缓存程序自身路径，后续过滤使用
	if exe, err := os.Executable(); err == nil {
		// 清理路径分隔符，统一为小写便于比较
		selfExePath = strings.ToLower(filepath.Clean(exe))
	}
}

// isSelfOrShortcutToSelf 检查文件是否指向程序自身。
// 返回 true 表示应该被过滤（不处理）。
func isSelfOrShortcutToSelf(fullPath string) bool {
	lower := strings.ToLower(fullPath)

	// 1) 直接匹配可执行文件本身
	if lower == selfExePath {
		return true
	}

	// 2) 检查 .lnk 快捷方式的目标
	if strings.HasSuffix(lower, ".lnk") {
		target := parseLnkTarget(fullPath)
		if target != "" {
			targetLower := strings.ToLower(filepath.Clean(target))
			if targetLower == selfExePath {
				return true
			}
		}
	}

	return false
}

// parseLnkTarget 解析 LNK 快捷方式文件，提取 LocalBasePath（目标路径）。
// 这是 windows_icon.go 中同名方法的轻量独立版本，避免循环依赖。
func parseLnkTarget(lnkPath string) string {
	data, err := os.ReadFile(lnkPath)
	if err != nil || len(data) < 76 {
		return ""
	}

	// 验证 LNK 魔数 0x4C 0x00 0x00 0x00
	if data[0] != 0x4C || data[1] != 0x00 || data[2] != 0x00 || data[3] != 0x00 {
		return ""
	}

	// Flags 在偏移 0x14
	flags := binary.LittleEndian.Uint32(data[0x14:0x18])
	hasTargetIDList := (flags & 0x01) != 0
	hasLinkInfo := (flags & 0x02) != 0

	offset := 0x4C // header 大小

	// 跳过 TargetIDList
	if hasTargetIDList {
		if offset+2 > len(data) {
			return ""
		}
		idListSize := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2 + idListSize
	}

	// 解析 LinkInfo
	if hasLinkInfo && offset+4 <= len(data) {
		linkInfoSize := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		if linkInfoSize > 0 && offset+linkInfoSize <= len(data) {
			linkInfo := data[offset : offset+linkInfoSize]
			if len(linkInfo) >= 28 {
				localBasePathOffset := int(binary.LittleEndian.Uint32(linkInfo[16:20]))
				if localBasePathOffset > 0 && localBasePathOffset < len(linkInfo) {
					end := localBasePathOffset
					for end < len(linkInfo) && linkInfo[end] != 0 {
						end++
					}
					target := string(linkInfo[localBasePathOffset:end])
					if target != "" {
						return target
					}
				}
			}
		}
	}

	return ""
}

// Manager 分组数据管理器
type Manager struct {
	cfg            *config.Config
	mu             sync.RWMutex
	onChange       func()
	suppressNotify int32 // >0 时 notifyChange 跳过回调（原子操作，用于批量操作避免重复刷新）
	itemOrder      map[string][]string // groupName -> ordered path list (in-memory, for drag reorder)
}

// NewManager 创建分组管理器
func NewManager() *Manager {
	cfg := config.Load()
	return &Manager{
		cfg:       cfg,
		itemOrder: make(map[string][]string),
	}
}

// SetOnChange 设置变更回调函数
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// SuppressNotify 临时抑制 notifyChange 回调（原子操作，可嵌套调用）
// 调用一次 SuppressNotify 必须对应一次 UnsuppressNotify
func (m *Manager) SuppressNotify() {
	atomic.AddInt32(&m.suppressNotify, 1)
}

// UnsuppressNotify 恢复 notifyChange 回调（原子操作）
func (m *Manager) UnsuppressNotify() {
	atomic.AddInt32(&m.suppressNotify, -1)
}

// SetFreeItemIndex 设置未分组项的网格索引（列优先：从上到下、从左到右）
// idx 为 -1 表示待分配（稍后由 DesktopMode 自动分配空闲格子）
func (m *Manager) SetFreeItemIndex(path string, idx int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.UngroupedIndices[path] = idx
	config.Save(m.cfg)
}

// GetFreeItemIndex 获取未分组项的网格索引，不存在则返回 -1 表示待分配
func (m *Manager) GetFreeItemIndex(path string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if idx, ok := m.cfg.UngroupedIndices[path]; ok {
		return idx
	}
	return -1
}

// RemoveFreeItemIndex 删除未分组项的索引记录（移入分组时调用）
func (m *Manager) RemoveFreeItemIndex(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cfg.UngroupedIndices, path)
	config.Save(m.cfg)
}

// notifyChange 通知变更
func (m *Manager) notifyChange() {
	if atomic.LoadInt32(&m.suppressNotify) > 0 {
		return
	}
	if m.onChange != nil {
		m.onChange()
	}
}

// save 保存配置
func (m *Manager) save() {
	config.Save(m.cfg)
}

// desktopItemIndex 返回路径在 DesktopItems 切片中的索引，不存在返回 -1
// 注意：调用方需持有 m.mu 锁（读锁或写锁）
func (m *Manager) desktopItemIndex(path string) int {
	for i := range m.cfg.DesktopItems {
		if m.cfg.DesktopItems[i].Path == path {
			return i
		}
	}
	return -1
}

// getItemGroup 获取项目所属分组名（或空字符串）
// 注意：调用方需持有 m.mu 锁（读锁或写锁）
func (m *Manager) getItemGroup(path string) string {
	if i := m.desktopItemIndex(path); i >= 0 {
		return m.cfg.DesktopItems[i].Group
	}
	return ""
}

// setItemGroup 设置项目分组；不存在则追加到末尾
// 注意：调用方需持有 m.mu 写锁
func (m *Manager) setItemGroup(path, group string) {
	if i := m.desktopItemIndex(path); i >= 0 {
		m.cfg.DesktopItems[i].Group = group
		return
	}
	m.cfg.DesktopItems = append(m.cfg.DesktopItems, config.DesktopItem{Path: path, Group: group})
}

// removeDesktopItem 从 DesktopItems 切片中移除路径
// 注意：调用方需持有 m.mu 写锁
func (m *Manager) removeDesktopItem(path string) {
	if i := m.desktopItemIndex(path); i >= 0 {
		m.cfg.DesktopItems = append(m.cfg.DesktopItems[:i], m.cfg.DesktopItems[i+1:]...)
	}
}

// GetConfig 获取当前配置
func (m *Manager) GetConfig() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// GetIconSizeLevel 获取桌面图标大小档位
//   0=大(48px)  1=中(48px)  2=小(32px)
func (m *Manager) GetIconSizeLevel() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.IconSizeLevel
}

// SetIconSizeLevel 设置桌面图标大小档位并保存
func (m *Manager) SetIconSizeLevel(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if level < 0 {
		level = 0
	}
	if level > 2 {
		level = 2
	}
	if m.cfg.IconSizeLevel == level {
		return
	}
	m.cfg.IconSizeLevel = level
	config.Save(m.cfg)
}

// GetAutoArrangeEnabled 获取是否启用"自动排列图标"
func (m *Manager) GetAutoArrangeEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AutoArrange
}

// SetAutoArrangeEnabled 设置是否启用"自动排列图标"并保存
func (m *Manager) SetAutoArrangeEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.AutoArrange == enabled {
		return
	}
	m.cfg.AutoArrange = enabled
	config.Save(m.cfg)
}

// GetAlignToGridEnabled 获取是否启用"将图标与网格对齐"
func (m *Manager) GetAlignToGridEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AlignToGrid
}

// SetAlignToGridEnabled 设置是否启用"将图标与网格对齐"并保存
func (m *Manager) SetAlignToGridEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.AlignToGrid == enabled {
		return
	}
	m.cfg.AlignToGrid = enabled
	config.Save(m.cfg)
}

// SetCardFontPreset 设置卡片标题字体预设并保存
// preset 为 "consolas" / "segoeui" / "yahei" / "custom" 之一
func (m *Manager) SetCardFontPreset(preset string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.CardFontPreset = preset
	// 预设发生变更时，同步刷新 name/size 以便降级或导出
	if p, ok := config.CardFontPresets[preset]; ok {
		m.cfg.CardFontName = p.Name
		m.cfg.CardFontSize = p.Size
	}
	config.Save(m.cfg)
}

// GetGuideLineColor 获取参考线颜色（#RRGGBBAA），空时返回默认红色
func (m *Manager) GetGuideLineColor() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.GuideLineColor == "" {
		return "#FF0000FF"
	}
	return m.cfg.GuideLineColor
}

// SetGuideLineColor 设置参考线颜色并保存
func (m *Manager) SetGuideLineColor(color string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.GuideLineColor = color
	config.Save(m.cfg)
}

// GetGroups 获取所有分组
func (m *Manager) GetGroups() []config.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]config.Group, len(m.cfg.Groups))
	copy(result, m.cfg.Groups)
	return result
}

// GetGroupItems 获取指定分组的所有项目
// DesktopItems 为有序切片，默认按配置中的顺序返回；若存在 itemOrder（拖拽排序）则按 itemOrder 顺序。
func (m *Manager) GetGroupItems(groupName string) []GroupItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 从有序切片中筛选该分组的项，保持配置顺序
	ordered := make([]GroupItem, 0)
	for _, di := range m.cfg.DesktopItems {
		if di.Group == groupName {
			name := strings.TrimSuffix(filepath.Base(di.Path), filepath.Ext(di.Path))
			ordered = append(ordered, GroupItem{Path: di.Path, Name: name})
		}
	}

	// 有拖拽排序记录时，按 itemOrder 顺序排列（未记录的项追加到末尾，保持配置顺序）
	if order, ok := m.itemOrder[groupName]; ok && len(order) > 0 {
		itemMap := make(map[string]bool, len(ordered))
		for _, it := range ordered {
			itemMap[it.Path] = true
		}
		var items []GroupItem
		added := make(map[string]bool, len(order))
		for _, path := range order {
			if itemMap[path] {
				name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				items = append(items, GroupItem{Path: path, Name: name})
				added[path] = true
			}
		}
		// 追加不在 order 中的项（保持配置切片顺序，稳定）
		for _, it := range ordered {
			if !added[it.Path] {
				items = append(items, it)
			}
		}
		return items
	}

	return ordered
}

// GetSystemItems 获取系统桌面项（如"此电脑"），作为未分组项展示
func (m *Manager) GetSystemItems() []GroupItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]GroupItem, len(m.cfg.SystemItems))
	for i, si := range m.cfg.SystemItems {
		items[i] = GroupItem{Path: "shell:" + si.ID, Name: si.Name}
	}
	return items
}

// AddSystemItem 添加系统桌面项
func (m *Manager) AddSystemItem(id, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, si := range m.cfg.SystemItems {
		if si.ID == id {
			return
		}
	}
	m.cfg.SystemItems = append(m.cfg.SystemItems, config.SystemItem{ID: id, Name: name})
	m.save()
	m.notifyChange()
}

// HasSystemItem 检查系统桌面项是否已添加
func (m *Manager) HasSystemItem(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, si := range m.cfg.SystemItems {
		if si.ID == id {
			return true
		}
	}
	return false
}

// RemoveSystemItem 移除系统桌面项
func (m *Manager) RemoveSystemItem(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, si := range m.cfg.SystemItems {
		if si.ID == id {
			m.cfg.SystemItems = append(m.cfg.SystemItems[:i], m.cfg.SystemItems[i+1:]...)
			break
		}
	}
	m.save()
	m.notifyChange()
}

// GetUngroupedItems 获取未分组的项目
// 包括：gName 为空的项目，以及 gName 对应的 Group 卡片不存在的项目（孤儿项），以及系统桌面项
func (m *Manager) GetUngroupedItems() []GroupItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 构建现有分组名集合
	existingGroups := make(map[string]bool, len(m.cfg.Groups))
	for _, g := range m.cfg.Groups {
		existingGroups[g.Name] = true
	}

	var items []GroupItem
	for _, di := range m.cfg.DesktopItems {
		// 未分组 或 分组卡片不存在的项目
		if di.Group == "" || !existingGroups[di.Group] {
			name := strings.TrimSuffix(filepath.Base(di.Path), filepath.Ext(di.Path))
			items = append(items, GroupItem{Path: di.Path, Name: name})
		}
	}

	// 追加系统桌面项
	for _, si := range m.cfg.SystemItems {
		items = append(items, GroupItem{Path: "shell:" + si.ID, Name: si.Name})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

// GroupItem 分组中的项目
type GroupItem struct {
	Path string
	Name string
}

// ItemInfo 统一项目信息（分组内 + 未分组）
type ItemInfo struct {
	Path      string // 文件路径
	Name      string // 显示名（不含扩展名）
	GroupName string // 所属分组名，"" 表示未分组
}

// GetAllItems 返回所有项目，按先分组内（按分组顺序）后未分组的顺序排列
// 同一组内按 itemOrder 或名称排序，未分组按名称排序
func (m *Manager) GetAllItems() []ItemInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ItemInfo

	// 按配置中的分组顺序输出
	for _, g := range m.cfg.Groups {
		// 收集该组内的项目
		groupItems := m.collectGroupItems(g.Name)
		result = append(result, groupItems...)
	}

	// 收集未分组/孤儿项目
	existingGroups := make(map[string]bool, len(m.cfg.Groups))
	for _, g := range m.cfg.Groups {
		existingGroups[g.Name] = true
	}
	var ungrouped []ItemInfo
	for _, di := range m.cfg.DesktopItems {
		if di.Group == "" || !existingGroups[di.Group] {
			name := strings.TrimSuffix(filepath.Base(di.Path), filepath.Ext(di.Path))
			ungrouped = append(ungrouped, ItemInfo{Path: di.Path, Name: name, GroupName: di.Group})
		}
	}
	sort.Slice(ungrouped, func(i, j int) bool {
		return ungrouped[i].Name < ungrouped[j].Name
	})
	result = append(result, ungrouped...)

	// 追加系统桌面项
	for _, si := range m.cfg.SystemItems {
		result = append(result, ItemInfo{
			Path:      "shell:" + si.ID,
			Name:      si.Name,
			GroupName: "",
		})
	}

	return result
}

// collectGroupItems 收集指定分组内的项目（按 itemOrder 或配置切片顺序）
func (m *Manager) collectGroupItems(groupName string) []ItemInfo {
	// 从有序切片中筛选该分组的项，保持配置顺序
	ordered := make([]ItemInfo, 0)
	for _, di := range m.cfg.DesktopItems {
		if di.Group == groupName {
			name := strings.TrimSuffix(filepath.Base(di.Path), filepath.Ext(di.Path))
			ordered = append(ordered, ItemInfo{Path: di.Path, Name: name, GroupName: groupName})
		}
	}

	if order, ok := m.itemOrder[groupName]; ok && len(order) > 0 {
		pathSet := make(map[string]bool, len(ordered))
		for _, it := range ordered {
			pathSet[it.Path] = true
		}
		var items []ItemInfo
		added := make(map[string]bool, len(order))
		for _, path := range order {
			if pathSet[path] {
				name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				items = append(items, ItemInfo{Path: path, Name: name, GroupName: groupName})
				added[path] = true
			}
		}
		// 追加不在 order 中的项（保持配置切片顺序，稳定）
		for _, it := range ordered {
			if !added[it.Path] {
				items = append(items, it)
			}
		}
		return items
	}
	return ordered
}

// GetItemGroupPath 获取项目所属分组名（或空字符串）
func (m *Manager) GetItemGroupPath(path string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getItemGroup(path)
}

// CreateGroup 创建新分组
func (m *Manager) CreateGroup(name, color string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	for _, g := range m.cfg.Groups {
		if g.Name == name {
			return
		}
	}

	m.cfg.Groups = append(m.cfg.Groups, config.Group{
		Name:     name,
		Position: config.Position{X: 0.1, Y: 0.1},
		Size:     config.Size{Width: 0.156, Height: 0.288},
		Color:    color,
	})

	m.save()
	m.notifyChange()
}

// DeleteGroup 删除分组（保留磁盘文件）
func (m *Manager) DeleteGroup(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newGroups := make([]config.Group, 0, len(m.cfg.Groups))
	for _, g := range m.cfg.Groups {
		if g.Name != name {
			newGroups = append(newGroups, g)
		}
	}
	m.cfg.Groups = newGroups

	// 将该分组的项目标记为未分组，并分配索引标记
	for i := range m.cfg.DesktopItems {
		if m.cfg.DesktopItems[i].Group == name {
			m.cfg.DesktopItems[i].Group = ""
			if _, exists := m.cfg.UngroupedIndices[m.cfg.DesktopItems[i].Path]; !exists {
				m.cfg.UngroupedIndices[m.cfg.DesktopItems[i].Path] = -1
			}
		}
	}

	delete(m.itemOrder, name)

	m.save()
	m.notifyChange()
}

// RenameGroup 重命名分组
func (m *Manager) RenameGroup(oldName, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.cfg.Groups {
		if g.Name == oldName {
			m.cfg.Groups[i].Name = newName
			break
		}
	}

	// 更新桌面项分组
	for i := range m.cfg.DesktopItems {
		if m.cfg.DesktopItems[i].Group == oldName {
			m.cfg.DesktopItems[i].Group = newName
		}
	}

	m.save()
	m.notifyChange()
}

// AddItemToGroup 添加项目到分组
func (m *Manager) AddItemToGroup(groupName, itemPath, itemName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.setItemGroup(itemPath, groupName)
	// 追加到 order 末尾
	m.itemOrder[groupName] = append(m.itemOrder[groupName], itemPath)
	m.save()
	m.notifyChange()
}

// RemoveItemFromGroup 从分组移除项目
func (m *Manager) RemoveItemFromGroup(groupName, itemPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getItemGroup(itemPath) == groupName {
		m.setItemGroup(itemPath, "")
	}
	// 从 order 中移除
	if order, ok := m.itemOrder[groupName]; ok {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[groupName] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}
	m.save()
	m.notifyChange()
}

// RemoveItem 从所有记录中完全移除项目（文件已删除时调用）
func (m *Manager) RemoveItem(itemPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从 DesktopItems 中移除
	m.removeDesktopItem(itemPath)

	// 从 UngroupedIndices 中移除
	delete(m.cfg.UngroupedIndices, itemPath)

	// 从所有分组的 order 中移除
	for groupName, order := range m.itemOrder {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[groupName] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}
	m.save()
	m.notifyChange()
}

// MoveItemToGroup 移动项目到指定分组
func (m *Manager) MoveItemToGroup(itemPath, groupName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从旧分组的 order 移除
	oldGroup := m.getItemGroup(itemPath)
	if order, ok := m.itemOrder[oldGroup]; ok {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[oldGroup] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}

	// 清除未分组索引记录
	delete(m.cfg.UngroupedIndices, itemPath)

	m.setItemGroup(itemPath, groupName)
	// 追加到新分组 order
	m.itemOrder[groupName] = append(m.itemOrder[groupName], itemPath)

	m.save()
	m.notifyChange()
}

// MoveItemToDesktop 将项目移出分组到桌面区域
func (m *Manager) MoveItemToDesktop(itemPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldGroup := m.getItemGroup(itemPath)
	if order, ok := m.itemOrder[oldGroup]; ok {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[oldGroup] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}

	m.setItemGroup(itemPath, "")
	// 确保未分组项有默认索引（-1 表示稍后在 DesktopMode 中分配）
	if _, exists := m.cfg.UngroupedIndices[itemPath]; !exists {
		m.cfg.UngroupedIndices[itemPath] = -1 // 标记为待分配
	}
	m.save()
	m.notifyChange()
}

// AddItemToDesktop 添加新项目到未分组桌面
func (m *Manager) AddItemToDesktop(itemPath, itemName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果已存在则跳过
	if m.desktopItemIndex(itemPath) >= 0 {
		return
	}

	m.setItemGroup(itemPath, "")
	m.cfg.UngroupedIndices[itemPath] = -1 // 标记为待分配
	m.save()
	m.notifyChange()
}

// MoveItemWithinGroup 在分组内移动项目到新位置（拖拽排序）
// DesktopItems 为有序切片，直接重排切片顺序即可，无需维护 itemOrder。
func (m *Manager) MoveItemWithinGroup(groupName, itemPath string, newIndex int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 收集该分组当前的项目路径（保持切片顺序）
	var order []string
	for _, di := range m.cfg.DesktopItems {
		if di.Group == groupName {
			order = append(order, di.Path)
		}
	}
	if len(order) == 0 {
		return
	}

	// 移除当前项目
	var newOrder []string
	for _, p := range order {
		if p != itemPath {
			newOrder = append(newOrder, p)
		}
	}
	// 插入到新位置
	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex >= len(newOrder) {
		newOrder = append(newOrder, itemPath)
	} else {
		newOrder = append(newOrder, "")
		copy(newOrder[newIndex+1:], newOrder[newIndex:])
		newOrder[newIndex] = itemPath
	}

	// 按新顺序重排该分组的项；非分组项保持原位。
	// 先收集该分组项，按 newOrder 顺序重建，再遍历原切片用新顺序替换分组项位置。
	byPath := make(map[string]config.DesktopItem)
	reordered := make([]config.DesktopItem, 0, len(newOrder))
	for _, di := range m.cfg.DesktopItems {
		if di.Group == groupName {
			byPath[di.Path] = di
		}
	}
	for _, p := range newOrder {
		if di, ok := byPath[p]; ok {
			reordered = append(reordered, di)
		}
	}
	rebuilt := make([]config.DesktopItem, 0, len(m.cfg.DesktopItems))
	rc := 0
	for _, di := range m.cfg.DesktopItems {
		if di.Group == groupName {
			rebuilt = append(rebuilt, reordered[rc])
			rc++
		} else {
			rebuilt = append(rebuilt, di)
		}
	}
	m.cfg.DesktopItems = rebuilt

	// 清除 itemOrder，使读取路径回退到切片顺序
	delete(m.itemOrder, groupName)

	m.save()
	m.notifyChange()
}

// UpdateGroupPosition 更新分组位置并持久化（相对坐标）
func (m *Manager) UpdateGroupPosition(name string, x, y float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.cfg.Groups {
		if g.Name == name {
			m.cfg.Groups[i].Position = config.Position{X: x, Y: y}
			break
		}
	}
	m.save()
}

// UpdateGroupSize 更新分组尺寸并持久化（相对坐标）
func (m *Manager) UpdateGroupSize(name string, w, h float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.cfg.Groups {
		if g.Name == name {
			m.cfg.Groups[i].Size = config.Size{Width: w, Height: h}
			break
		}
	}
	m.save()
}

// UpdateGroupCollapsed 更新分组折叠状态并持久化
func (m *Manager) UpdateGroupCollapsed(name string, collapsed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.cfg.Groups {
		if g.Name == name {
			m.cfg.Groups[i].Collapsed = collapsed
			break
		}
	}
	m.save()
}

// UpdateGroupColor 更新分组颜色并持久化
// 注意：不调用 notifyChange，因为调用方（onColor 回调）之后会调用 refreshCards() 完成完整 UI 刷新。
// 避免 notifyChange 通过 Synchronize 投递 dm.Refresh() 到消息队列后，
// 与 refreshCards() 产生时序冲突（桌面背景覆盖新卡片）。
func (m *Manager) UpdateGroupColor(name, color string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.cfg.Groups {
		if g.Name == name {
			m.cfg.Groups[i].Color = color
			break
		}
	}
	m.save()
}

// RenameItem 重命名文件项，同时更新配置中的路径（支持桌面快捷方式实际文件名修改）
func (m *Manager) RenameItem(oldPath, newName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldDir := filepath.Dir(oldPath)
	oldExt := filepath.Ext(oldPath)
	newPath := filepath.Join(oldDir, newName+oldExt)

	// 检查目标是否已存在
	if _, err := os.Stat(newPath); err == nil {
		return "", os.ErrExist
	}

	// 实际重命名磁盘文件
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", err
	}

	// 更新 DesktopItems（保持原位置，仅替换路径）
	if i := m.desktopItemIndex(oldPath); i >= 0 {
		m.cfg.DesktopItems[i].Path = newPath
	}

	// 更新 UngroupedIndices
	if idx, ok := m.cfg.UngroupedIndices[oldPath]; ok {
		delete(m.cfg.UngroupedIndices, oldPath)
		m.cfg.UngroupedIndices[newPath] = idx
	}

	// 更新 itemOrder
	for gName, order := range m.itemOrder {
		for i, p := range order {
			if p == oldPath {
				m.itemOrder[gName][i] = newPath
				break
			}
		}
	}

	m.save()
	m.notifyChange()
	return newPath, nil
}

// ReloadDesktopItems 从 Windows 桌面目录重新同步内容
// 以系统桌面目录为主：添加桌面中存在但配置中缺失的项，删除配置中存在但桌面中已不存在的项
func (m *Manager) ReloadDesktopItems() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 收集桌面目录中的所有文件/文件夹
	desktopPaths := collectDesktopPaths()

	// 构建桌面实际路径集合
	desktopSet := make(map[string]bool, len(desktopPaths))
	for _, item := range desktopPaths {
		desktopSet[item.Path] = true
	}

	// 删除配置中存在但桌面目录中已不存在的项（以系统桌面目录为准）
	for i := 0; i < len(m.cfg.DesktopItems); {
		path := m.cfg.DesktopItems[i].Path
		if !desktopSet[path] {
			m.cfg.DesktopItems = append(m.cfg.DesktopItems[:i], m.cfg.DesktopItems[i+1:]...)
			delete(m.cfg.UngroupedIndices, path)
			// 同步清理 itemOrder，避免残留幽灵路径
			for gName, order := range m.itemOrder {
				for j, p := range order {
					if p == path {
						m.itemOrder[gName] = append(order[:j], order[j+1:]...)
						break
					}
				}
			}
		} else {
			i++
		}
	}

	// 添加桌面目录中存在但配置中缺失的项（默认未分组，追加到末尾）
	for _, item := range desktopPaths {
		if m.desktopItemIndex(item.Path) < 0 {
			m.cfg.DesktopItems = append(m.cfg.DesktopItems, config.DesktopItem{Path: item.Path, Group: ""})
		}
	}

	m.save()
	m.notifyChange()
}

// groupForPath 根据文件类型和名称确定默认分组
func (m *Manager) groupForPath(filePath, name string, isDir bool) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	nameLower := strings.ToLower(name)

	// 快捷方式
	switch ext {
	case ".lnk", ".url", ".exe":
		return "快捷方式"
	}

	// 文件夹
	if isDir {
		return "备份文件"
	}

	// 文档
	switch ext {
	case ".doc", ".docx", ".txt", ".pdf", ".xls", ".xlsx", ".ppt", ".pptx", ".rtf":
		return "Word"
	}

	// 图片
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg", ".webp":
		return "图片"
	}

	// 按名称匹配
	if strings.Contains(nameLower, "快捷") {
		return "快捷方式"
	}
	if strings.Contains(nameLower, "文件") || strings.Contains(nameLower, "备份") {
		return "备份文件"
	}
	if strings.Contains(nameLower, "word") || strings.Contains(nameLower, "文档") {
		return "Word"
	}
	if strings.Contains(nameLower, "图片") {
		return "图片"
	}
	if strings.Contains(nameLower, "桌面") {
		return "桌面"
	}

	return "桌面"
}

// desktopItemInfo 桌面项信息（与 ui.DesktopItem 字段保持一致）
type desktopItemInfo struct {
	Path      string // 文件完整路径
	Name      string // 显示名称（不含扩展名）
	IsDir     bool   // 是否为文件夹
	GroupName string // 所属分组名（由 groupForPath 填充，scan 阶段为空）
}

// collectDesktopPaths 收集桌面目录中的文件
func collectDesktopPaths() []desktopItemInfo {
	var items []desktopItemInfo

	home, _ := os.UserHomeDir()
	desktopDir := filepath.Join(home, "Desktop")
	publicDesktop := filepath.Join(os.Getenv("PUBLIC"), "Desktop")
	if publicDesktop == filepath.Join("", "Desktop") {
		publicDesktop = `C:\Users\Public\Desktop`
	}

	for _, dir := range []string{desktopDir, publicDesktop} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.EqualFold(name, "desktop.ini") {
				continue
			}
			fullPath := filepath.Join(dir, name)

			// 过滤掉程序自身及指向自身的快捷方式
			if isSelfOrShortcutToSelf(fullPath) {
				continue
			}

			items = append(items, desktopItemInfo{
				Path:  fullPath,
				Name:  strings.TrimSuffix(name, filepath.Ext(name)),
				IsDir: entry.IsDir(),
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}
