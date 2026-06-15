// factory.go — single entry point for picking a ui.UI implementation.
//
// factory.go — 选择 ui.UI 实现的统一入口。
//
// Both the scan and resume subcommands need a UI; centralising the
// choice in Select() keeps ShouldUseTUI's contract (env, tty, width)
// in one place and makes the choice unit-testable without spinning up
// a bubbletea program.
//
// scan / resume 子命令都需要 UI；把选择集中到 Select() 能让
// ShouldUseTUI 的契约（env、tty、宽度）只有一份实现，并且无需启
// bubbletea 程序就能单测选择逻辑。
package ui

import (
	"github.com/LCUstinian/FG-QiMen/internal/tui"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Select returns the UI implementation appropriate for cfg's
// environment and the user's explicit overrides. The returned
// UI is freshly constructed; the caller owns its lifecycle.
//
// Note: tui.NewProgram constructs but does not start the bubbletea
// program — the event loop only runs once the caller's Run() is
// invoked. Select is therefore safe to call from tests that just
// want to verify the decision; only the actual Run() will touch the
// terminal.
//
// cfg is propagated to both the bubbletea program and TextUI so the
// ShowCleartext redaction gate is honoured on every UI surface
// (P0#2, P0#3).
//
// Select 根据 cfg 的环境和用户的显式覆盖返回合适的 UI 实现。返回的
// UI 是新构造的；调用方负责生命周期。
//
// 注意：tui.NewProgram 只构造不启动 bubbletea program —— 事件循环
// 要等调用方调用 Run() 才会跑。所以 Select 可在单测中调用以验证
// 选择；只有真正 Run() 才会触碰终端。
//
// cfg 传给 bubbletea program 和 TextUI，确保 ShowCleartext redact
// 门在每个 UI 表面都生效（P0#2、P0#3）。
func Select(cfg *types.Config) UI {
	if ShouldUseTUI(cfg) {
		return tui.NewProgram(cfg)
	}
	return NewTextUI(cfg)
}
