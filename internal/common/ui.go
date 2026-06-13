// ui.go — UI abstraction so core/plugins don't depend on TUI vs text.
// ui.go — UI 抽象层，使 core/plugins 不依赖 TUI 或纯文本具体实现。
package common

// UI is the interface through which core/plugins report events to the
// user. Two implementations:
//   - tui.Program (Bubbletea)  — TUI mode
//   - log-only fallback        — plain text mode
//
// UI 是 core/plugins 向用户报告事件的接口。两种实现：
//   - tui.Program (Bubbletea)  —— TUI 模式
//   - log-only fallback        —— 纯文本模式
type UI interface {
	// Banner displays the startup banner (TUI) or summary (text).
	// Banner 显示启动 banner（TUI）或摘要（文本）。
	Banner(cfg *Config)
	// Stats pushes an updated counter snapshot. Called periodically.
	// Stats 推送最新的计数器快照。周期性调用。
	Stats(s *State)
	// Event pushes a single live result event.
	// Event 推送单个实时结果事件。
	Event(r *Result)
	// CredFound highlights a credential hit.
	// CredFound 高亮显示凭据命中。
	CredFound(r *Result)
	// Done signals end of scan and prints/shows final summary.
	// Done 通知扫描结束，打印/显示最终摘要。
	Done(summary string)
}

// nopUI is a no-op UI used in unit tests and as the zero value.
// nopUI 是单元测试和零值使用的空实现 UI。
type nopUI struct{}

func (nopUI) Banner(*Config)    {}
func (nopUI) Stats(*State)      {}
func (nopUI) Event(*Result)     {}
func (nopUI) CredFound(*Result) {}
func (nopUI) Done(string)       {}

// NopUI returns a no-op UI.
// NopUI 返回一个空实现 UI。
func NopUI() UI { return nopUI{} }
