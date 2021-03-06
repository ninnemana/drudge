package drudge

import (
	"os"
	"time"

	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/codes"
)

// codeToLevel redirects OK to DEBUG level logging instead of INFO
// This is example how you can log several gRPC code results
func codeToLevel(code codes.Code) zapcore.Level {
	if code == codes.OK {
		// It is DEBUG
		return zap.DebugLevel
	}

	return grpc_zap.DefaultCodeToLevel(code)
}

func customTimeEncoder(format string) func(time.Time, zapcore.PrimitiveArrayEncoder) {
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format(format))
	}
}

// initLogger sets up uber's zap structured logger for logging our gRPC requests.
func initLogger(lvl int, timeFormat string) *zap.Logger {
	globalLevel := zapcore.Level(lvl)

	// High-priority output should also go to standard error, and low-priority
	// output should also go to standard out.
	// It is useful for Kubernetes deployment.
	// Kubernetes interprets os.Stdout log items as INFO and os.Stderr log items
	// as ERROR by default.
	highPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.ErrorLevel
	})
	lowPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= globalLevel && lvl < zapcore.ErrorLevel
	})
	consoleInfos := zapcore.Lock(os.Stdout)
	consoleErrors := zapcore.Lock(os.Stderr)

	ecfg := zap.NewProductionEncoderConfig()
	ecfg.EncodeTime = customTimeEncoder(time.RFC3339)

	consoleEncoder := zapcore.NewJSONEncoder(ecfg)

	// Join the outputs, encoders, and level-handling functions into
	// zapcore.
	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, consoleErrors, highPriority),
		zapcore.NewCore(consoleEncoder, consoleInfos, lowPriority),
	)

	// From a zapcore.Core, it's easy to construct a Logger.
	lg := zap.New(core)
	zap.RedirectStdLog(lg)

	if len(timeFormat) == 0 {
		lg.Warn("time format for logger is not provided - use zap default")
	}

	return lg
}
