// session.go — Session ties together Config, State, Project, UI, and
// the pipeline channels. It is the per-run context passed to core/plugins.
//
// session.go — Session 把 Config、State、Project、UI 以及 pipeline channel
// 串在一起，作为单次运行的上下文传递给 core/plugins。
//
// Session lives in its own package so the leaf packages (types,
// output, store, ui) can stay loop-free. Anyone touching Session
// (cmd, core, plugins) imports session; nothing here is imported
// by the leaf packages.
//
// Session 单独成包，确保叶子包（types / output / store / ui）无 import
// 环。所有需要 Session 的代码（cmd / core / plugins）都 import session；
// 叶子包反过来不依赖 session。
package session

import (
	"context"

	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/store"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/ui"
)

// Session is the mutable, per-invocation state bag. Created by
// NewSession() and passed to core.RunScan and every plugin.
//
// Session 是单次调用的可变状态集合。由 NewSession() 创建，传递给
// core.RunScan 和每个插件。
type Session struct {
	// Ctx is the root context for this scan; cancel propagates to
	// every producer/consumer goroutine.
	// Ctx 是本次扫描的根 context；cancel 传播到所有 producer/consumer 协程。
	Ctx context.Context

	// Config is the validated, immutable configuration snapshot.
	// Config 是已校验、不可变的配置快照。
	Config *types.Config

	// State holds dedup + counters; shared across producers/consumers.
	// State 持有去重和计数器；跨 producer/consumer 共享。
	State *types.State

	// Store is the optional bbolt persistence (nil in ephemeral mode).
	// Store 是可选的 bbolt 持久化（即扫即走模式下为 nil）。
	Store *store.Store

	// Out is the multi-format result sink.
	// Out 是多格式结果汇。
	Out *output.Output

	// UI is the user-facing event sink (TUI or plain text).
	// UI 是面向用户的事件汇（TUI 或纯文本）。
	UI ui.UI

	// Log is the per-session English logger.
	// Log 是单次会话的英文 logger。
	Log types.Logger

	// ProjectName is the active project name (empty in ephemeral mode).
	// ProjectName 是当前激活的项目名（即扫即走模式下为空）。
	ProjectName string
}

// NewSession constructs a Session with the given config + project context.
// It does NOT open files; the caller (workspace.Open / cmd.runScan) is
// responsible for opening them and assigning to Session.Out / Session.Store.
//
// NewSession 用给定 config + project 上下文构造 Session。
// 它不打开文件；调用方（workspace.Open / cmd.runScan）负责打开并赋值给
// Session.Out / Session.Store。
func NewSession(ctx context.Context, cfg *types.Config, projectName string) (*Session, error) {
	return &Session{
		Ctx:         ctx,
		Config:      cfg,
		State:       types.NewState(),
		ProjectName: projectName,
		UI:          ui.NopUI(),
		Log:         types.DiscardLogger{},
	}, nil
}
