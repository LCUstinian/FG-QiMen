// tty.go — TTY detection for TUI auto-fallback.
// tty.go — TTY 检测，用于 TUI 自动降级。
package common

import "os"

import "golang.org/x/term"

// IsTerminalStdout reports whether os.Stdout is a terminal.
// IsTerminalStdout 报告 os.Stdout 是否为终端。
func IsTerminalStdout() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsTerminalStderr reports whether os.Stderr is a terminal.
// IsTerminalStderr 报告 os.Stderr 是否为终端。
func IsTerminalStderr() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}
