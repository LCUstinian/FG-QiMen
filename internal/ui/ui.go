// ui.go — UI abstraction. Implementations live alongside in this
// package (textui.go) or in internal/tui (Bubbletea).
//
// ui.go — UI 抽象。实现位于本包（textui.go）或 internal/tui（Bubbletea）。
package ui

import "github.com/LCUstinian/FG-QiMen/internal/types"

// UI is the interface through which core/plugins report events to the
// user. Two implementations:
//   - tui.Program (Bubbletea)  — TUI mode
//   - TextUI                    — plain text mode
//
// UI 是 core/plugins 向用户报告事件的接口。两种实现：
//   - tui.Program (Bubbletea)  —— TUI 模式
//   - TextUI                    —— 纯文本模式。
type UI interface {
	// Banner displays the startup banner (TUI) or summary (text).
	// Banner 显示启动 banner（TUI）或摘要（文本）。
	Banner(cfg *types.Config)
	// Stats pushes an updated counter snapshot. Called periodically.
	// Stats 推送最新的计数器快照。周期性调用。
	Stats(s *types.State)
	// Event pushes a single live result event.
	// Event 推送单个实时结果事件。
	Event(r *types.Result)
	// CredFound highlights a credential hit.
	// CredFound 高亮显示凭据命中。
	CredFound(r *types.Result)
	// Done signals end of scan and prints/shows final summary.
	// Done 通知扫描结束，打印/显示最终摘要。
	Done(summary string)
}

// nopUI is a no-op UI used in unit tests and as the zero value.
// nopUI 是单元测试和零值使用的空实现 UI。
type nopUI struct{}

func (nopUI) Banner(*types.Config) {}
func (nopUI) Stats(*types.State)   {}
func (nopUI) Event(*types.Result)  {}
func (nopUI) CredFound(*types.Result) {}
func (nopUI) Done(string)          {}

// NopUI returns a no-op UI.
// NopUI 返回一个空实现 UI。
func NopUI() UI { return nopUI{} }
