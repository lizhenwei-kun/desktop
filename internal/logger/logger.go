package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	l         *zap.SugaredLogger
	logCh     chan interface{}
	dir       string
	progName  string
	curFile   *os.File
	curSize   int64  // 当前文件已写入字节数
	curDate   string // "20060102"，跨天轮转
)

const (
	maxFileSize = 50 * 1024 * 1024 // 50MB
	chanBufSize = 4096
)

// Init 初始化日志，level: "debug", "info", "warn", "error"
// logDir 日志目录（如 "./log"），会在其中按文件名格式生成日志文件
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
		_ = os.MkdirAll(logDir, 0755)
		dir = logDir
		progName = filepath.Base(os.Args[0])
		if ext := filepath.Ext(progName); ext != "" {
			progName = progName[:len(progName)-len(ext)]
		}
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
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case logCh <- buf:
	default:
	}
	return len(p), nil
}

func (s *chanSyncer) Sync() error {
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

func openLogFile(date string) {
	now := time.Now()
	filename := fmt.Sprintf("%s_%s.log",
		progName,
		now.Format("2006_02_01-15_04_05"),
	)
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
func Stop() {
	if logCh == nil {
		return
	}
	// 先刷 zap 内部 buffer
	if l != nil {
		_ = l.Sync()
	}
	// 关闭 chan，writeLoop 退出 for range
	close(logCh)
	// 关闭文件
	if curFile != nil {
		_ = curFile.Close()
		curFile = nil
	}
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
