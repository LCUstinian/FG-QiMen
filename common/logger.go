// logger.go — minimal English-only logger.
// logger.go — 极简纯英文日志器。
//
// Two implementations:
//   - StderrLogger  : writes to a writer (typically os.Stderr); safe for
//     concurrent use.
//   - DiscardLogger : no-op; used in TUI mode where messages are sent
//     to the Bubbletea program instead.
//
// 两种实现：
//   - StderrLogger ：写入 writer（通常是 os.Stderr），并发安全。
//   - DiscardLogger ：空实现，用于 TUI 模式（消息走 Bubbletea program）。
package common

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Logger is the minimal logging interface used by core/plugins.
// Logger 是 core/plugins 使用的最小日志接口。
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
	Debug(format string, args ...any)
	Success(format string, args ...any)
	CredFound(format string, args ...any)
}

// StderrLogger writes log lines to an io.Writer with a timestamp prefix.
// StderrLogger 写入带时间戳前缀的日志行到 io.Writer。
type StderrLogger struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStderrLogger returns a logger that writes to os.Stderr.
// NewStderrLogger 返回写入 os.Stderr 的 logger。
func NewStderrLogger() *StderrLogger {
	return &StderrLogger{w: os.Stderr}
}

// NewLoggerTo returns a logger that writes to the given writer.
// NewLoggerTo 返回写入给定 writer 的 logger。
func NewLoggerTo(w io.Writer) *StderrLogger {
	return &StderrLogger{w: w}
}

func (l *StderrLogger) write(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.w, "%s %s ", ts, level)
	fmt.Fprintf(l.w, format, args...)
	fmt.Fprintln(l.w)
}

// Info logs an info-level message. / Info 输出 info 级消息。
func (l *StderrLogger) Info(format string, args ...any)  { l.write("[*]", format, args...) }
func (l *StderrLogger) Warn(format string, args ...any)  { l.write("[!]", format, args...) }
func (l *StderrLogger) Error(format string, args ...any) { l.write("[-]", format, args...) }
func (l *StderrLogger) Debug(format string, args ...any) { l.write("[.]", format, args...) }
func (l *StderrLogger) Success(format string, args ...any) {
	l.write("[+]", format, args...)
}
func (l *StderrLogger) CredFound(format string, args ...any) {
	l.write("[!]", format, args...)
}

// DiscardLogger is a no-op logger for TUI mode.
// DiscardLogger 是 TUI 模式下使用的空实现 logger。
type DiscardLogger struct{}

func (DiscardLogger) Info(string, ...any)  {}
func (DiscardLogger) Warn(string, ...any)  {}
func (DiscardLogger) Error(string, ...any) {}
func (DiscardLogger) Debug(string, ...any) {}
func (DiscardLogger) Success(string, ...any) {
}
func (DiscardLogger) CredFound(string, ...any) {}
