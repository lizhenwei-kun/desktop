package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"desktop_go/internal/logger"
)

// DesktopWatcherState 桌面目录文件变更监听状态
// 注意：lastChangeTime 和 changePending 仅在同一个 fsnotify goroutine 中访问，无需加锁。
// watcherRunning 通过 sync.Mutex 保护，可在不同 goroutine 中安全读写。
type DesktopWatcherState struct {
	mu             sync.Mutex
	watcherRunning bool
	stopOnce       sync.Once
	stopCh         chan struct{}
	lastChangeTime time.Time // 上次变更时间，用于延迟触发（仅在 fsnotify goroutine 中访问）
	changePending  bool      // 是否有待处理的变更（仅在 fsnotify goroutine 中访问）
}

func (s *DesktopWatcherState) setRunning(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watcherRunning = v
}

func (s *DesktopWatcherState) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watcherRunning
}

// desktopWatchDelay 变更事件延迟触发时间（秒），避免高频事件频繁刷新
const desktopWatchDelay = 2

// initDesktopWatcher 启动桌面目录文件变更监听
// 使用 fsnotify 监听用户桌面目录的创建、删除、重命名、写入等操作，
// 事件到来后延迟 2 秒再触发刷新，避免文件复制/重命名过程中频繁刷新。
func (dm *DesktopMode) initDesktopWatcher() {
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("desktopWatcher: cannot get user home dir: %v", err)
		return
	}
	desktopDir := filepath.Join(home, "Desktop")

	dm.DesktopWatcherState.stopCh = make(chan struct{})

	// 启动监听 goroutine
	go dm.watchDesktopDirectory(desktopDir)
}

// watchDesktopDirectory 在独立 goroutine 中使用 fsnotify 监听桌面目录
func (dm *DesktopMode) watchDesktopDirectory(dir string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("desktopWatcher: panic: %v", r)
		}
	}()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Warn("desktopWatcher: fsnotify.NewWatcher failed: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		logger.Warn("desktopWatcher: watcher.Add(%q) failed: %v", dir, err)
		return
	}

	logger.Debug("desktopWatcher: watching %q with fsnotify", dir)
	dm.DesktopWatcherState.setRunning(true)

	// 定时器 ticker，每秒检查一次是否需要触发延迟刷新
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dm.DesktopWatcherState.stopCh:
			logger.Debug("desktopWatcher: stopped")
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			dm.handleWatchEvent(event)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Warn("desktopWatcher: fsnotify error: %v", err)

		case <-ticker.C:
			dm.checkDelayedRefresh()
		}
	}
}

// handleWatchEvent 处理 fsnotify 事件，记录变更时间
func (dm *DesktopMode) handleWatchEvent(event fsnotify.Event) {
	// 过滤 desktop.ini
	name := filepath.Base(event.Name)
	if strings.EqualFold(name, "desktop.ini") {
		return
	}

	// 只关注创建、删除、写入、重命名
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) ||
		event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Chmod) {
		dm.DesktopWatcherState.lastChangeTime = time.Now()
		dm.DesktopWatcherState.changePending = true
		logger.Debug("desktopWatcher: event %s, pending refresh", event)
	}
}

// checkDelayedRefresh 检查是否需要触发延迟刷新
// 如果上次变更时间已超过 delay 秒，则执行刷新
func (dm *DesktopMode) checkDelayedRefresh() {
	if !dm.DesktopWatcherState.changePending {
		return
	}

	if time.Since(dm.DesktopWatcherState.lastChangeTime) < desktopWatchDelay*time.Second {
		return
	}

	dm.DesktopWatcherState.changePending = false

	// 投递到 UI 主线程刷新桌面
	dm.Post(func() {
		logger.Debug("desktopWatcher: directory changed, reloading desktop items")
		dm.Manager.ReloadDesktopItems()
		dm.BodyWidget.Invalidate()
	})
}

// stopDesktopWatcher 停止桌面目录监听
func (dm *DesktopMode) stopDesktopWatcher() {
	dm.DesktopWatcherState.stopOnce.Do(func() {
		if dm.DesktopWatcherState.stopCh != nil {
			close(dm.DesktopWatcherState.stopCh)
		}
		dm.DesktopWatcherState.setRunning(false)
	})
}
