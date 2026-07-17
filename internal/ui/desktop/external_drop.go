package desktop

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"desktop_go/internal/logger"
	"desktop_go/internal/ui"
)

var procGetMessagePos = syscall.NewLazyDLL("user32.dll").NewProc("GetMessagePos")

// ============================================================
// DesktopMode 外部拖放集成方法
// ============================================================

// RegisterExternalDropTarget 注册外部文件拖放
// 使用 walk 内置的 DropFiles 事件（WindowBase.DropFiles），无需手动子类化
func (dm *DesktopMode) RegisterExternalDropTarget() {
	if dm.BodyWidget == nil {
		logger.Error("RegisterExternalDropTarget: BodyWidget is nil")
		return
	}

	// walk 的 WindowBase.DropFiles() 内部会调用 DragAcceptFiles
	// 并且通过 WndProc 自动处理 WM_DROPFILES
	dm.BodyWidget.DropFiles().Attach(func(files []string) {
		// 获取鼠标屏幕坐标
		pos, _, _ := procGetMessagePos.Call()
		screenX := int(int16(pos & 0xFFFF))
		screenY := int(int16((pos >> 16) & 0xFFFF))

		dm.handleExternalFilesDrop(files, screenX, screenY)
	})

	dm.ExternalDropRegistered = true
	dm.ExternalDropHwnd = dm.BodyWidget.Handle()
	logger.Debug("RegisterExternalDropTarget: hwnd=%v registered via walk DropFiles event", dm.ExternalDropHwnd)
}

// UnregisterExternalDropTarget 注销外部文件拖放
func (dm *DesktopMode) UnregisterExternalDropTarget() {
	if !dm.ExternalDropRegistered || dm.BodyWidget == nil {
		return
	}

	// walk 的 DropFiles 事件 Detach 会自动调用 DragAcceptFiles(hwnd, FALSE)
	// 但我们不保存 handler handle，所以直接断开所有 handler
	// 通过 close 方式让 GC 处理
	dm.ExternalDropRegistered = false
	dm.ExternalDropHwnd = 0

	logger.Debug("UnregisterExternalDropTarget: unregistered")
}

// handleExternalFilesDrop 处理外部文件拖放
func (dm *DesktopMode) handleExternalFilesDrop(files []string, screenX, screenY int) {
	if len(files) == 0 {
		return
	}

	logger.Debug("handleExternalFilesDrop: %d files at screen(%d,%d)", len(files), screenX, screenY)

	// 检查是否拖放到某张卡片上
	var targetCard *ui.GroupCard
	for _, card := range dm.Cards {
		sb := card.ScreenBounds()
		if screenX >= sb.X && screenX <= sb.X+sb.Width &&
			screenY >= sb.Y && screenY <= sb.Y+sb.Height {
			targetCard = card
			break
		}
	}

	desktopPath := ui.GetDesktopPath()

	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		ext := filepath.Ext(fileName)
		displayName := fileName[:len(fileName)-len(ext)]

		if targetCard != nil {
			// 拖到卡片：添加到分组（保持原路径引用）
			dm.Manager.AddItemToGroup(targetCard.GroupName(), filePath, displayName)
			logger.Debug("handleExternalFilesDrop: added %s to group %s", filePath, targetCard.GroupName())
		} else {
			// 拖到桌面空白区域：复制文件到系统桌面目录
			destPath := filepath.Join(desktopPath, fileName)
			if filePath != destPath {
				// 目标已存在时自动编号
				destPath = resolveDestPath(destPath)
				if err := copyFile(filePath, destPath); err != nil {
					logger.Error("handleExternalFilesDrop: copy %s to desktop failed: %v", filePath, err)
					continue
				}
				logger.Debug("handleExternalFilesDrop: copied %s -> %s", filePath, destPath)
			}
			// 使用桌面目录下的新路径添加到未分组
			dm.Manager.AddItemToDesktop(destPath, displayName)
		}
	}

	// 刷新所有卡片
	for _, card := range dm.Cards {
		card.Refresh()
	}

	// 刷新桌面
	dm.BodyWidget.Invalidate()
}

// resolveDestPath 如果目标路径已存在，自动添加编号（如 "文件 (2).txt"）
func resolveDestPath(dest string) string {
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	dir := filepath.Dir(dest)
	base := filepath.Base(dest)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// copyFile 复制文件（支持目录递归复制）
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		// 复制目录
		return copyDir(src, dst)
	}

	// 复制文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyDir 递归复制目录
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
