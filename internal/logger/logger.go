package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var stopOnce sync.Once

var (
	l          *zap.SugaredLogger
	logCh      chan interface{}
	dir        string
	progName   string
	startTime  string    // 程序启动时间 "20060102150405"，无分隔符
	curFile    *os.File
	curSize    int64     // 当前文件已写入字节数
	curDate    string    // "20060102"，跨天轮转
	curSeq     int       // 当天文件轮转编号
	closed     int32     // atomic: 1=Stop 已调用
)

const (
	maxFileSize = 50 * 1024 * 1024 // 50MB
	chanBufSize = 4096
)

// Init 初始化日志，level: "debug", "info", "warn", "error"
// logDir 日志目录（如 "./log"），会在其中按文件名格式生成日志文件
// 文件名格式: <程序名>-<启动时间>-<年_月_日>-<时_分_秒>_<轮转编号>.log
// 示例: desktop_go-20260728143025-2026_07_28-14_30_25_00.log
//   - 启动时间: 无分隔符紧凑格式，固定在该次程序启动时生成
//   - 轮转编号: 两位小数（00-99），同一天内每轮转一次 +1，跨天重置为 00
func Init(level string, logDir string) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	var writeSyncer zapcore.WriteSyncer
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "logger: 创建日志目录失败 %s: %v\n", logDir, err)
			os.Exit(1)
		}
		dir = logDir
		progName = filepath.Base(os.Args[0])
		if ext := filepath.Ext(progName); ext != "" {
			progName = progName[:len(progName)-len(ext)]
		}
		startTime = time.Now().Format("20060102150405")
		openLogFile(time.Now().Format("20060102"))
		logCh = make(chan interface{}, chanBufSize)
		go writeLoop()
		writeSyncer = &chanSyncer{}
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		MessageKey:     "msg",
		CallerKey:      "caller",
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		writeSyncer,
		zapLevel,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	l = logger.Sugar()
}

// chanSyncer 将日志写入 chan，完全不阻塞调用方
type chanSyncer struct{}

func (s *chanSyncer) Write(p []byte) (int, error) {
	if atomic.LoadInt32(&closed) != 0 {
		return len(p), nil
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case logCh <- buf:
	default:
	}
	return len(p), nil
}

func (s *chanSyncer) Sync() error {
	if atomic.LoadInt32(&closed) != 0 {
		return nil
	}
	done := make(chan struct{})
	logCh <- done
	<-done
	return nil
}

// writeLoop 后台 goroutine，从 chan 读取日志数据并写入文件，同时按 50MB 轮转
func writeLoop() {
	for msg := range logCh {
		switch v := msg.(type) {
		case chan struct{}:
			// flush 标记：刷盘并通知调用方
			if curFile != nil {
				_ = curFile.Sync()
			}
			close(v)
		case []byte:
			// 跨天或文件超过 50MB 则轮转
			date := time.Now().Format("20060102")
			if date != curDate || curSize >= maxFileSize {
				_ = curFile.Close()
				curFile = nil
				curSize = 0
				openLogFile(date)
			}
			if curFile != nil {
				n, _ := curFile.Write(v)
				curSize += int64(n)
			}
		}
	}
}

// openLogFile 创建新的日志文件。
// 文件名包含程序启动时间（固定不变）、当前日期时间、以及当天轮转编号。
// 轮转编号在跨天时重置为 0，同一天内每次调用 +1。
func openLogFile(date string) {
	now := time.Now()
	// 跨天时重置轮转编号
	if date != curDate {
		curSeq = 0
	}
	timeStr := now.Format("15_04_05")
	dateStr := now.Format("2006_01_02")
	filename := fmt.Sprintf("%s-%s-%s-%s_%02d.log", progName, startTime, dateStr, timeStr, curSeq)
	path := filepath.Join(dir, filename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		curFile = nil
		return
	}
	curFile = f
	curDate = date
	curSize = 0
}

// Sync 刷新日志缓冲
func Sync() {
	if l != nil {
		_ = l.Sync()
	}
}

// Stop 停止日志后台 goroutine，等待所有积压日志写入文件后关闭文件
// 程序退出前必须调用，否则可能丢失日志
// 可多次调用，只有第一次生效
func Stop() {
	stopOnce.Do(func() {
		if logCh == nil {
			return
		}
		// 先刷 zap 内部 buffer
		if l != nil {
			_ = l.Sync()
		}
		// 标记关闭，后续 Write/Sync 直接跳过
		atomic.StoreInt32(&closed, 1)
		// 关闭 chan，writeLoop 退出 for range
		close(logCh)
		// 关闭文件
		if curFile != nil {
			_ = curFile.Close()
			curFile = nil
		}
	})
}

func Debug(format string, args ...interface{}) {
	if l != nil {
		l.Debugf(format, args...)
	}
}

func Info(format string, args ...interface{}) {
	if l != nil {
		l.Infof(format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	if l != nil {
		l.Warnf(format, args...)
	}
}

func Error(format string, args ...interface{}) {
	if l != nil {
		l.Errorf(format, args...)
	}
}
