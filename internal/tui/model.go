// model.go — type stubs scaffolded ahead of Task 3. Task 2's
// view_test.go references eventEntry (used by severityColor), so we
// declare it here even though Task 3 will finalise the LIVE EVENTS
// panel model. / model.go — 提前于 Task 3 搭的 type 桩。Task 2 的
// view_test.go 引用 eventEntry（severityColor 要用），所以我们
// 在这里声明它；Task 3 会最终化 LIVE EVENTS 面板 model。
package tui

import "time"

// eventEntry is one row in the LIVE EVENTS panel. / eventEntry
// 是 LIVE EVENTS 面板的一行。
type eventEntry struct {
	Host    string
	Port    int
	Service string
	Kind    string // "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
	At      time.Time
}
