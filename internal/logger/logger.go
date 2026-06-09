package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var l *zap.SugaredLogger

// Init 初始化日志，level: "debug", "info", "warn", "error"
func Init(level string, filePath string) {
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
	if filePath != "" {
		// 自动创建日志目录
		if dir := filepath.Dir(filePath); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			writeSyncer = zapcore.AddSync(os.Stdout)
		} else {
			writeSyncer = zapcore.AddSync(f)
		}
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		MessageKey:     "msg",
		CallerKey:      "caller",
		EncodeTime:     zapcore.TimeEncoderOfLayout("15:04:05.000"),
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

// Sync 刷新日志缓冲
func Sync() {
	if l != nil {
		_ = l.Sync()
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
