// Package ui — tty detection helpers and the ShouldUseTUI decision.
//
// Package ui — TTY 探测 helpers 和 ShouldUseTUI 决策。
//
// The factory in factory.go asks ShouldUseTUI whether to start the
// bubbletea TUI; the function centralises every signal we trust:
//   - explicit flags (NoTUI, Silent)
//   - stdout-is-a-tty
//   - common CI / dumb-term environment variables
//   - terminal width (TUI's two-column layout collapses below 80 cols)
//
// 工厂（factory.go）通过 ShouldUseTUI 决定是否启动 bubbletea TUI；
// 该函数集中了所有可信任的信号：
//   - 显式 flag（NoTUI、Silent）
//   - stdout 是 TTY
//   - 常见 CI / dumb-term 环境变量
//   - 终端宽度（TUI 的双栏布局在 80 列以下会错乱）
package ui

import (
	"os"

	"golang.org/x/term"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// minTUIWidth is the smallest terminal width that the TUI's two-column
// layout (Targets + Live Events) renders cleanly. Below this we fall
// back to TextUI, whose line-oriented output survives any width.
//
// minTUIWidth 是 TUI 双栏布局（Targets + Live Events）能正常渲染的
// 最小终端宽度。低于此宽度降级到 TextUI，其行式输出对任意宽度鲁棒。
const minTUIWidth = 80

// IsTerminalStdout reports whether os.Stdout is attached to a terminal.
//
// On Windows the underlying x/term call checks console mode; on POSIX
// it checks isatty(2). Returns false for pipes, redirects, CI runners
// that don't expose a TTY, and disconnected file descriptors.
//
// IsTerminalStdout 报告 os.Stdout 是否连到终端。管道/重定向/无 TTY
// 的 CI runner/已断开的 fd 一律返回 false。
func IsTerminalStdout() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsTerminalStderr reports whether os.Stderr is attached to a
// terminal. TextUI uses this to decide between the \r-overwrite
// stats trick (TTY) and newline-terminated lines (redirected).
//
// IsTerminalStderr 报告 os.Stderr 是否连到终端。TextUI 据此在 \r
// 覆盖（TTY）和换行（重定向）之间二选一。
func IsTerminalStderr() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// TerminalWidth returns the current stdout width in columns, or an
// error if it cannot be determined (e.g. stdout is not a tty).
//
// TerminalWidth 返回当前 stdout 的列数；无法确定时（如 stdout 非 TTY）
// 返回错误。
func TerminalWidth() (int, error) {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	return w, err
}

// IsCI reports whether the current process is running inside a
// continuous-integration runner. Detection covers the common hosts
// (GitHub Actions, GitLab CI, CircleCI, Travis, Jenkins, Buildkite,
// Drone, AppVeyor, TeamCity) by checking their well-known env vars.
//
// IsCI 报告当前进程是否在 CI runner 内运行。通过常见 CI 的环境变量
// 检测（GitHub Actions / GitLab CI / CircleCI / Travis / Jenkins /
// Buildkite / Drone / AppVeyor / TeamCity）。
func IsCI() bool {
	for _, k := range []string{
		"CI",                // generic + GitHub Actions / GitLab / Travis / AppVeyor
		"CONTINUOUS_INTEGRATION", // TeamCity generic
		"BUILD_NUMBER",      // Jenkins / TeamCity (combined with above)
		"CI_NAME",           // Buildkite
		"DRONE",             // Drone
		"CIRCLECI",          // CircleCI explicit
	} {
		if v, ok := os.LookupEnv(k); ok && v != "" && v != "false" && v != "0" {
			return true
		}
	}
	return false
}

// IsDumbTerm reports whether the terminal advertises itself as dumb
// (no ANSI cursor control, no colour). Returning true here pushes us
// off the TUI path onto TextUI.
//
// IsDumbTerm 报告终端是否声明为 dumb（无 ANSI 控制、无颜色）。
// 此处返回 true 会让我们走 TextUI 而非 TUI。
func IsDumbTerm() bool {
	return os.Getenv("TERM") == "dumb"
}

// IsNoColor reports whether the operator has opted out of color via
// the NO_COLOR env var. Per https://no-color.org/ — any non-empty
// value of NO_COLOR disables color; "0" and "false" are explicit
// no-ops. We treat this as a TUI palette override, not a TUI exit
// signal (the user still wants the dashboard, just monochrome).
//
// IsNoColor 报告操作员是否通过 NO_COLOR 环境变量禁用颜色。按
// https://no-color.org/ —— NO_COLOR 任何非空值都禁用颜色；"0" 和
// "false" 显式 opt-out。我们把它当 TUI 调色板覆盖，不当 TUI 退出
// 信号（用户仍要 dashboard，只是单色）。
func IsNoColor() bool {
	v, ok := os.LookupEnv("NO_COLOR")
	if !ok {
		return false
	}
	if v == "" || v == "0" || v == "false" || v == "False" || v == "FALSE" {
		return false
	}
	return true
}

// ShouldUseTUI centralises every signal that should switch us out of
// the bubbletea TUI. The function is the single source of truth for
// "do we render the dashboard or fall back to plain text?" — used by
// the factory and by tests.
//
// The checks run in cheapest-first order: explicit flags first (so
// the user can always force a mode), then environment signals, then
// the (slightly more expensive) tty / width probes.
//
// ShouldUseTUI 集中所有应当切出 TUI 的信号。该函数是"渲染 dashboard
// 还是降级纯文本"的唯一事实源——供工厂和测试使用。
//
// 校验顺序按开销从低到高：先显式 flag（用户总能强制指定），再环境
// 变量，最后是（略贵的）tty / 宽度探测。
func ShouldUseTUI(cfg *types.Config) bool {
	if cfg == nil {
		return false
	}
	// 1. Explicit user override. / 1. 用户显式覆盖。
	if cfg.NoTUI {
		return false
	}
	// 2. CI / dumb term — even if stdout is a tty, a CI runner wrapping
	//    us (e.g. `script(1)`) may hand us a tty fd that doesn't
	//    actually render ANSI. Bail to TextUI.
	// 2. CI / dumb term —— 即便 stdout 是 tty，CI runner 包装
	//    （如 `script(1)`）也可能给我们一个不渲染 ANSI 的 tty fd。
	if IsCI() || IsDumbTerm() {
		return false
	}
	// 3. Output is going somewhere other than a tty (pipe / redirect).
	// 3. 输出走向非 tty 设备（管道/重定向）。
	if !IsTerminalStdout() {
		return false
	}
	// 4. Width probe — bail to TextUI if the terminal is too narrow
	//    for the two-column layout. term.GetSize on a non-tty errors
	//    out, so this also catches the rare case where step 3 was
	//    bypassed by a CI tool that provides a fake tty fd.
	// 4. 宽度探测——终端太窄以至于双栏布局错乱时降级。
	if w, err := TerminalWidth(); err != nil || w < minTUIWidth {
		return false
	}
	return true
}
