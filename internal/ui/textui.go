// textui.go — plain-text ui.UI implementation.
// textui.go — 纯文本 ui.UI 实现。
//
// Used when stdout is not a TTY (pipe / redirect / CI), -no-tui is
// passed, or ShouldUseTUI() declines for environmental reasons (CI,
// TERM=dumb, narrow terminal). Prints banner, stats, and live events
// to stderr so they don't pollute the result files on stdout.
//
// 用于 stdout 非 TTY（管道/重定向/CI）、显式 -no-tui、或 ShouldUseTUI
// 因环境原因（CI / TERM=dumb / 终端过窄）拒绝的情况。把 banner、
// stats、live events 打印到 stderr，避免污染 stdout 上的结果文件。
package ui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/version"
)

// TextUI writes events to stderr. Safe for concurrent use.
//
// The doneOnce flag guards against double-printing the final summary:
// scanner.go calls Done() from both the success path (line 69) and
// the early-error path (line 163). Without a guard, the summary
// would print twice on error paths and twice on retry races.
//
// stderrIsTTY is captured once at construction: the runtime cost of
// term.IsTerminal is negligible, but the value doesn't change for
// the life of a process, so we don't re-poll every Stats() tick.
//
// cfg is the source of truth for the redaction gate (P0#3): when
// cfg.ShowCleartext is false, the cleartext credential pair is
// replaced with a length-only fingerprint. Held by value; never
// mutated.
//
// TextUI 把事件写入 stderr。并发安全。
//
// doneOnce 防止重复打印最终摘要：scanner.go 在成功路径（第 69 行）和
// 早错路径（第 163 行）都会调 Done()。没有守卫，错误路径和重试竞态
// 下摘要会打印两次。
//
// stderrIsTTY 构造时一次性捕获：term.IsTerminal 的运行时开销可忽略，
// 但值在进程生命期不变，没必要每个 Stats() 滴答都重新检测。
//
// cfg 是 redact 门（P0#3）的真源：cfg.ShowCleartext 为 false 时，明文
// 凭据对替换为仅含长度的指纹。按值持有；永不修改。
type TextUI struct {
	mu           sync.Mutex
	ran          time.Time
	doneOnce     bool
	stderrIsTTY  bool
	cfg          *types.Config
}

// NewTextUI returns a fresh text ui.UI. / NewTextUI 返回一个纯文本 ui.UI。
func NewTextUI(cfg *types.Config) *TextUI {
	return &TextUI{
		ran:         time.Now(),
		stderrIsTTY: IsTerminalStderr(),
		cfg:         cfg,
	}
}

// Banner prints the startup banner. / Banner 打印启动 banner。
func (u *TextUI) Banner(cfg *types.Config) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if cfg == nil {
		return
	}
	fmt.Fprintf(os.Stderr,
		"\n[*] FG-QiMen %s  project=%q  mode=%s  ports=%s  threads=%d  timeout=%s\n",
		version.Value, cfg.Project, cfg.Mode, cfg.Ports, cfg.Threads, cfg.Timeout)
}

// Stats pushes an updated counter snapshot.
//
// When stderr is a TTY we use the in-place \r overwrite (1 Hz) to
// avoid flooding the terminal; when stderr is redirected (e.g.
// `2>log.txt` or piped into a non-terminal) the \r trick would
// produce one giant run-on line, so we emit a newline-terminated
// line instead. Detection: a single term.IsTerminal call cached at
// construction time.
//
// Stats 推送最新计数器快照。
//
// 当 stderr 是 TTY 时使用 \r 就地覆盖（1Hz）避免刷屏；当 stderr 被
// 重定向（如 `2>log.txt` 或管道到非终端）时 \r 会让所有 stats 黏成一
// 行，所以改用换行符。检测方式：构造时一次性查 term.IsTerminal 并
// 缓存。
func (u *TextUI) Stats(s *types.State) {
	if s == nil {
		return
	}
	v := s.Snapshot()
	u.mu.Lock()
	defer u.mu.Unlock()
	line := fmt.Sprintf("[.] alive=%d ports=%d results=%d creds=%d err=%d elapsed=%s",
		v.Alive, v.Ports, v.Results, v.Creds, v.Errors,
		time.Since(u.ran).Round(time.Second))
	if u.stderrIsTTY {
		// \r + content + spaces (Erase-to-EOL not always supported in
		// minimal terminals; spaces are a portable fallback).
		// \r + 内容 + 空格（Erase-to-EOL 在 minimal 终端未必支持，空格
		// 是可移植的回退方案）。
		fmt.Fprintf(os.Stderr, "\r%-80s", line)
	} else {
		fmt.Fprintln(os.Stderr, line)
	}
}

// Event prints a single non-cred live event. / Event 打印单条非凭据事件。
func (u *TextUI) Event(r *types.Result) {
	if r == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(os.Stderr, "\n[+] %s:%d  [%s]  %s\n", r.Host, r.Port, r.Service, r.Banner)
}

// CredFound prints a high-priority credential event.
// Renders via types.ShowUserPassword so cfg.ShowCleartext controls
// whether the cleartext pair or a redacted fingerprint is shown on
// stderr. P0#3 — stderr is captured into CI logs / journald by
// default, so writing cleartext there is a real leak in shared
// environments.
//
// CredFound 打印凭据命中事件。
// 走 types.ShowUserPassword 渲染，cfg.ShowCleartext 决定 stderr
// 上显示明文对还是脱敏指纹。P0#3——stderr 默认会被 CI 日志 /
// journald 捕获，共享环境里写明文是真泄露。
func (u *TextUI) CredFound(r *types.Result) {
	if r == nil || r.Cred == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(os.Stderr, "\n[!] %s:%d  [%s]  %s  ← CREDENTIAL FOUND\n",
		r.Host, r.Port, r.Service,
		types.ShowUserPassword(u.cfg, r.Cred.User, r.Cred.Pass))
}

// Done prints the final summary once. / Done 一次性打印最终摘要。
func (u *TextUI) Done(summary string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.doneOnce {
		return
	}
	u.doneOnce = true
	fmt.Fprintln(os.Stderr, "\n"+summary)
}
