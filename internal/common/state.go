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
package common

import (
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

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

	// pauseCh gates producer emission. Closed = running; open struct{}{} = paused.
	// pauseCh 控制 producer 发射。关闭 = 运行中；写入 struct{}{} = 暂停。
	pauseMu sync.RWMutex
	paused  bool
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
	Alive   atomic.Int64
	Ports   atomic.Int64
	Results atomic.Int64
	Creds   atomic.Int64
	Errors  atomic.Int64
}

// CountersView is a plain-int64 snapshot of Counters for safe display/logging.
// CountersView 是 Counters 的纯 int64 快照，可安全地用于显示/日志。
type CountersView struct {
	Alive   int64
	Ports   int64
	Results int64
	Creds   int64
	Errors  int64
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
		Alive:   s.Counters.Alive.Load(),
		Ports:   s.Counters.Ports.Load(),
		Results: s.Counters.Results.Load(),
		Creds:   s.Counters.Creds.Load(),
		Errors:  s.Counters.Errors.Load(),
	}
}

// SetPaused toggles pause state for the producer.
// SetPaused 切换 producer 的暂停状态。
func (s *State) SetPaused(p bool) {
	s.pauseMu.Lock()
	s.paused = p
	s.pauseMu.Unlock()
}

// IsPaused returns current pause state.
// IsPaused 返回当前暂停状态。
func (s *State) IsPaused() bool {
	s.pauseMu.RLock()
	defer s.pauseMu.RUnlock()
	return s.paused
}
