package group

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	cfg       *config.Config
	mu        sync.RWMutex
	onChange  func()
	itemOrder map[string][]string // groupName -> ordered path list (in-memory, for drag reorder)
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

// SetFreeItemPosition 设置未分组项的相对位置
func (m *Manager) SetFreeItemPosition(path string, pos config.Position) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.UngroupedPositions[path] = pos
	config.Save(m.cfg)
}

// GetFreeItemPosition 获取未分组项的相对位置，不存在则返回 {-1,-1} 表示待分配
func (m *Manager) GetFreeItemPosition(path string) config.Position {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if pos, ok := m.cfg.UngroupedPositions[path]; ok {
		return pos
	}
	return config.Position{X: -1, Y: -1}
}

// RemoveFreeItemPosition 删除未分组项的位置记录（移入分组时调用）
func (m *Manager) RemoveFreeItemPosition(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cfg.UngroupedPositions, path)
	config.Save(m.cfg)
}

// notifyChange 通知变更
func (m *Manager) notifyChange() {
	if m.onChange != nil {
		m.onChange()
	}
}

// save 保存配置
func (m *Manager) save() {
	config.Save(m.cfg)
}

// GetConfig 获取当前配置
func (m *Manager) GetConfig() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
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
func (m *Manager) GetGroupItems(groupName string) []GroupItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build map: path -> name
	itemMap := make(map[string]string)
	for path, gName := range m.cfg.DesktopItems {
		if gName == groupName {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			itemMap[path] = name
		}
	}

	// Use itemOrder if available
	if order, ok := m.itemOrder[groupName]; ok && len(order) > 0 {
		var items []GroupItem
		added := make(map[string]bool, len(order))
		for _, path := range order {
			if name, ok := itemMap[path]; ok {
				items = append(items, GroupItem{Path: path, Name: name})
				added[path] = true
			}
		}
		// Append items not yet in order
		for path, name := range itemMap {
			if !added[path] {
				items = append(items, GroupItem{Path: path, Name: name})
			}
		}
		return items
	}

	// Fallback: sorted by name
	var items []GroupItem
	for path, gName := range m.cfg.DesktopItems {
		if gName == groupName {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			items = append(items, GroupItem{Path: path, Name: name})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
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
	for path, gName := range m.cfg.DesktopItems {
		// 未分组 或 分组卡片不存在的项目
		if gName == "" || !existingGroups[gName] {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			items = append(items, GroupItem{Path: path, Name: name})
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
	for path, gName := range m.cfg.DesktopItems {
		if gName == "" || !existingGroups[gName] {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			ungrouped = append(ungrouped, ItemInfo{Path: path, Name: name, GroupName: gName})
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

// collectGroupItems 收集指定分组内的项目（按 itemOrder 或名称排序）
func (m *Manager) collectGroupItems(groupName string) []ItemInfo {
	itemMap := make(map[string]string)
	for path, gName := range m.cfg.DesktopItems {
		if gName == groupName {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			itemMap[path] = name
		}
	}

	var items []ItemInfo
	if order, ok := m.itemOrder[groupName]; ok && len(order) > 0 {
		added := make(map[string]bool, len(order))
		for _, path := range order {
			if name, ok := itemMap[path]; ok {
				items = append(items, ItemInfo{Path: path, Name: name, GroupName: groupName})
				added[path] = true
			}
		}
		for path, name := range itemMap {
			if !added[path] {
				items = append(items, ItemInfo{Path: path, Name: name, GroupName: groupName})
			}
		}
	} else {
		for path, name := range itemMap {
			items = append(items, ItemInfo{Path: path, Name: name, GroupName: groupName})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
	}
	return items
}

// GetItemGroupPath 获取项目所属分组名（或空字符串）
func (m *Manager) GetItemGroupPath(path string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.DesktopItems[path]
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

	// 将该分组的项目标记为未分组，并分配位置标记
	for path, gName := range m.cfg.DesktopItems {
		if gName == name {
			m.cfg.DesktopItems[path] = ""
			if _, exists := m.cfg.UngroupedPositions[path]; !exists {
				m.cfg.UngroupedPositions[path] = config.Position{X: -1, Y: -1}
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

	// 更新桌面项映射
	for path, gName := range m.cfg.DesktopItems {
		if gName == oldName {
			m.cfg.DesktopItems[path] = newName
		}
	}

	m.save()
	m.notifyChange()
}

// AddItemToGroup 添加项目到分组
func (m *Manager) AddItemToGroup(groupName, itemPath, itemName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.DesktopItems[itemPath] = groupName
	// 追加到 order 末尾
	m.itemOrder[groupName] = append(m.itemOrder[groupName], itemPath)
	m.save()
	m.notifyChange()
}

// RemoveItemFromGroup 从分组移除项目
func (m *Manager) RemoveItemFromGroup(groupName, itemPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.DesktopItems[itemPath] == groupName {
		m.cfg.DesktopItems[itemPath] = ""
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
	delete(m.cfg.DesktopItems, itemPath)

	// 从 UngroupedPositions 中移除
	delete(m.cfg.UngroupedPositions, itemPath)

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
	oldGroup := m.cfg.DesktopItems[itemPath]
	if order, ok := m.itemOrder[oldGroup]; ok {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[oldGroup] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}

	// 清除未分组位置记录
	delete(m.cfg.UngroupedPositions, itemPath)

	m.cfg.DesktopItems[itemPath] = groupName
	// 追加到新分组 order
	m.itemOrder[groupName] = append(m.itemOrder[groupName], itemPath)

	m.save()
	m.notifyChange()
}

// MoveItemToDesktop 将项目移出分组到桌面区域
func (m *Manager) MoveItemToDesktop(itemPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldGroup := m.cfg.DesktopItems[itemPath]
	if order, ok := m.itemOrder[oldGroup]; ok {
		for i, p := range order {
			if p == itemPath {
				m.itemOrder[oldGroup] = append(order[:i], order[i+1:]...)
				break
			}
		}
	}

	m.cfg.DesktopItems[itemPath] = ""
	// 确保未分组项有默认位置（使用0表示稍后在DesktopMode中分配）
	if _, exists := m.cfg.UngroupedPositions[itemPath]; !exists {
		m.cfg.UngroupedPositions[itemPath] = config.Position{X: -1, Y: -1} // 标记为待分配
	}
	m.save()
	m.notifyChange()
}

// AddItemToDesktop 添加新项目到未分组桌面
func (m *Manager) AddItemToDesktop(itemPath, itemName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果已存在则跳过
	if _, exists := m.cfg.DesktopItems[itemPath]; exists {
		return
	}

	m.cfg.DesktopItems[itemPath] = ""
	m.cfg.UngroupedPositions[itemPath] = config.Position{X: -1, Y: -1} // 标记为待分配
	m.save()
	m.notifyChange()
}

// MoveItemWithinGroup 在分组内移动项目到新位置（拖拽排序）
func (m *Manager) MoveItemWithinGroup(groupName, itemPath string, newIndex int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	order := m.itemOrder[groupName]
	// 如果 order 为空（首次拖拽），从所有分组项目构建初始排序
	if len(order) == 0 {
		for path, gName := range m.cfg.DesktopItems {
			if gName == groupName {
				order = append(order, path)
			}
		}
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
	m.itemOrder[groupName] = newOrder
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

	// 更新 DesktopItems 映射
	if gName, ok := m.cfg.DesktopItems[oldPath]; ok {
		delete(m.cfg.DesktopItems, oldPath)
		m.cfg.DesktopItems[newPath] = gName
	}

	// 更新 UngroupedPositions
	if pos, ok := m.cfg.UngroupedPositions[oldPath]; ok {
		delete(m.cfg.UngroupedPositions, oldPath)
		m.cfg.UngroupedPositions[newPath] = pos
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
	for path := range m.cfg.DesktopItems {
		if !desktopSet[path] {
			delete(m.cfg.DesktopItems, path)
			delete(m.cfg.UngroupedPositions, path)
		}
	}

	// 添加桌面目录中存在但配置中缺失的项（默认未分组）
	for _, item := range desktopPaths {
		if _, exists := m.cfg.DesktopItems[item.Path]; !exists {
			m.cfg.DesktopItems[item.Path] = ""
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
