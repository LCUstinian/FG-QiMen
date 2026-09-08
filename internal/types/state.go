// state.go — in-memory + bbolt-backed dedup state with atomic counters.
// state.go — 内存 + bbolt 持久化去重状态，atomic 计数器。
//
// State holds:
//   - a sync.Map of seen hashes (in-memory dedup, no lock for hot path)
//   - an optional *bbolt.DB for persistence (project mode only)
//   - atomic counters for live event/result counts
//
// State 持有：
//   - sync.Map 保存已见 hash（内存去重，热路径无锁）
//   - 可选 *bbolt.DB 用于持久化（仅项目模式）
//   - atomic 计数器统计事件/结果
package types

import (
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// Scan-stage constants. State.Stage holds the current stage's integer
// value. The integer ordering (Idle=0, Alive=1, ..., Done=5) is part
// of the public contract — do NOT reorder when adding new stages.
// / Scan-stage 常量。State.Stage 持有当前阶段的整数值。整数顺序
// 是公开契约的一部分——新增阶段时不要重排。
const (
	StageIdle     int32 = 0
	StageAlive    int32 = 1
	StagePortScan int32 = 2
	StageIdentify int32 = 3
	StageCred     int32 = 4
	StageDone     int32 = 5
)

// StageName returns a short human-readable label for a stage value.
// Unknown / unset stages return "IDLE".
// / StageName 返回阶段值的短人类可读标签。未知 / 未设置返回 "IDLE"。
func StageName(stage int32) string {
	switch stage {
	case StageAlive:
		return "ALIVE"
	case StagePortScan:
		return "PORT-SCAN"
	case StageIdentify:
		return "IDENTIFY"
	case StageCred:
		return "CRED"
	case StageDone:
		return "DONE"
	default:
		return "IDLE"
	}
}

// State is the shared mutable state for a single scan run.
// State 是单次扫描运行的共享可变状态。
//
// Safe for concurrent use; do not copy after first use.
//
// 并发安全；首次使用后请勿复制。
type State struct {
	// seen is the in-memory hash set (host:port:service:plugin → struct{}).
	// seen 是内存 hash 集合（host:port:service:plugin → struct{}）。
	seen sync.Map

	// counters are atomic counters for live dashboard updates.
	// counters 用于实时仪表盘更新的 atomic 计数器。
	Counters Counters

	// StartTime is when the scan started (for elapsed display).
	// StartTime 是扫描开始时间（用于已用时间显示）。
	StartTime time.Time

	// v0.5.2 additions for TUI information density.
	Stage           atomic.Int32
	TotalHosts      atomic.Int64
	TotalPorts      atomic.Int64
	PluginHits      sync.Map // string → *atomic.Int64
	ErrorCategories sync.Map // string → *atomic.Int64
}

// Counters is a struct of atomic counters.
// Counters 是 atomic 计数器集合。
//
// Note: do NOT copy a Counters value by value (it contains
// sync/atomic.Int64 which has a noCopy lock). Use Snapshot() to get a
// plain-int64 copy suitable for logging/display.
//
// 注意：不要按值复制 Counters（含 sync/atomic.Int64 有 noCopy 锁）。
// 用 Snapshot() 获取纯 int64 副本用于日志/显示。
type Counters struct {
	Alive       atomic.Int64
	AliveProbed atomic.Int64 // v0.5.2: probes attempted in current alive sweep (UI progress)
	Ports       atomic.Int64
	Results     atomic.Int64
	Creds       atomic.Int64
	Errors      atomic.Int64
}

// CountersView is a plain-int64 snapshot of Counters for safe display/logging.
// CountersView 是 Counters 的纯 int64 快照，可安全地用于显示/日志。
type CountersView struct {
	Alive       int64
	AliveProbed int64
	Ports       int64
	Results     int64
	Creds       int64
	Errors      int64
	Stage       int64 // v0.5.2: current scan stage (StageIdle=0, StageAlive=1, ...)
}

// NewState creates a fresh State with counters zeroed.
// NewState 创建一个计数器清零的 State。
func NewState() *State {
	return &State{StartTime: time.Now()}
}

// HashKey computes a 16-byte (32 hex char) SHA-1-derived dedup key from
// the given components. Truncating to 16 bytes is enough to avoid
// collisions in practice while keeping keys compact.
//
// HashKey 计算由给定组件派生的 16 字节（32 hex 字符）SHA-1 去重 key。
// 截前 16 字节在实践中足以避免碰撞，同时保持 key 紧凑。
func HashKey(parts ...string) string {
	h := sha1.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// MarkSeen records that the given hash has been observed. Returns true on
// first occurrence, false if it was already present.
//
// MarkSeen 记录给定 hash 已被观察。首次出现返回 true；已存在返回 false。
func (s *State) MarkSeen(hash string) bool {
	if _, loaded := s.seen.LoadOrStore(hash, struct{}{}); loaded {
		return false
	}
	return true
}

// Seen reports whether the given hash was previously recorded.
// Seen 报告给定 hash 是否已被记录。
func (s *State) Seen(hash string) bool {
	_, ok := s.seen.Load(hash)
	return ok
}

// Snapshot returns a plain-int64 view of current counters, safe to
// copy and use for display/logging.
// Snapshot 返回当前计数器的纯 int64 视图，可安全复制用于显示/日志。
func (s *State) Snapshot() CountersView {
	return CountersView{
		Alive:       s.Counters.Alive.Load(),
		AliveProbed: s.Counters.AliveProbed.Load(),
		Ports:       s.Counters.Ports.Load(),
		Results:     s.Counters.Results.Load(),
		Creds:       s.Counters.Creds.Load(),
		Errors:      s.Counters.Errors.Load(),
		Stage:       int64(s.Stage.Load()),
	}
}

// PluginHitsView returns a snapshot of plugin-name → hit count as a
// plain map (atomic load per entry). Read-only; safe to call from
// the TUI's stats handler.
// / PluginHitsView 返回 plugin-name → hit count 快照 map（每项
// atomic load）。只读；可从 TUI 的 stats handler 安全调用。
func (s *State) PluginHitsView() map[string]int64 {
	out := make(map[string]int64)
	s.PluginHits.Range(func(k, v any) bool {
		if c, ok := v.(*atomic.Int64); ok {
			out[k.(string)] = c.Load()
		}
		return true
	})
	return out
}

// ErrorCategoriesView returns a snapshot of category → count as a
// plain map (atomic load per entry).
// / ErrorCategoriesView 返回 category → count 快照 map（每项
// atomic load）。
func (s *State) ErrorCategoriesView() map[string]int64 {
	out := make(map[string]int64)
	s.ErrorCategories.Range(func(k, v any) bool {
		if c, ok := v.(*atomic.Int64); ok {
			out[k.(string)] = c.Load()
		}
		return true
	})
	return out
}

// (P2 dead-code purge: SetPaused / IsPaused / pauseMu / paused /
// pauseCh removed in v0.2 audit. The TUI's [p] pause / [r] resume
// keys remain unimplemented; if/when a real pause path is built,
// re-introduce them with a real consumer.)
// （P2 死代码清理：v0.2 审计删了 SetPaused / IsPaused / pauseMu /
// paused / pauseCh。TUI 的 [p] pause / [r] resume 键仍未实现；若将来
// 真要加暂停路径，请带回有真实消费者的版本。）
