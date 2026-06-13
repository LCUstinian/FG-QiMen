// textui.go — plain-text ui.UI implementation.
// textui.go — 纯文本 ui.UI 实现。
//
// Used when stdout is not a TTY (pipe / redirect / CI) or -no-tui is
// passed. Prints banner, stats, and live events to stderr so they
// don't pollute the result files on stdout.
//
// 用于 stdout 非 TTY（管道/重定向/CI）或显式 -no-tui 的情况。
// 把 banner、stats、live events 打印到 stderr，避免污染 stdout 上的结果文件。
package ui

import (
	"fmt"
	"os"
	"sync"
	"time"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TextUI writes events to stderr. Safe for concurrent use.
// TextUI 把事件写入 stderr。并发安全。
type TextUI struct {
	mu  sync.Mutex
	ran time.Time
}

// NewTextUI returns a fresh text ui.UI. / NewTextUI 返回一个纯文本 ui.UI。
func NewTextUI() *TextUI { return &TextUI{ran: time.Now()} }

// Banner prints the startup banner. / Banner 打印启动 banner。
func (u *TextUI) Banner(cfg *types.Config) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if cfg == nil {
		return
	}
	fmt.Fprintf(os.Stderr,
		"\n[*] FG-QiMen %s  project=%q  mode=%s  ports=%s  threads=%d  timeout=%s\n",
		"v0.1", cfg.Project, cfg.Mode, cfg.Ports, cfg.Threads, cfg.Timeout)
}

// Stats pushes an updated counter snapshot. / Stats 推送最新计数器快照。
func (u *TextUI) Stats(s *types.State) {
	if s == nil {
		return
	}
	v := s.Snapshot()
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(os.Stderr,
		"\r%s alive=%d ports=%d results=%d creds=%d err=%d elapsed=%s ",
		"[.]", v.Alive, v.Ports, v.Results, v.Creds, v.Errors,
		time.Since(u.ran).Round(time.Second))
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
// CredFound 打印凭据命中事件。
func (u *TextUI) CredFound(r *types.Result) {
	if r == nil || r.Cred == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(os.Stderr, "\n[!] %s:%d  [%s]  %s / %s  ← CREDENTIAL FOUND\n",
		r.Host, r.Port, r.Service, r.Cred.User, r.Cred.Pass)
}

// Done prints the final summary. / Done 打印最终摘要。
func (u *TextUI) Done(summary string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintln(os.Stderr, "\n"+summary)
}
