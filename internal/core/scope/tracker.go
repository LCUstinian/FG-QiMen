// Package scope tracks network identities discovered OUTSIDE the
// requested scan scope ("Measure first, then scan" extended beyond the
// initial target list): who was seen (IP / MAC / hostname), from which
// protocol interaction, and when.
//
// The Tracker is the single point of truth for out-of-scope
// discoveries. Producers (alive probes, the plugin pipeline) call
// Record; consumers are the discovery sink (written through the
// injected callback) and — with --expand-scope auto — the bounded
// second scan round.
//
// Package scope 追踪在请求扫描范围之外发现的网络身份（"先测量再扫
// 描"延伸到初始目标清单之外）：看到了谁（IP/MAC/主机名）、来自哪个
// 协议交互、何时发现。
//
// Tracker 是范围外发现的唯一真相点。生产方（alive 探测、插件管线）
// 调 Record；消费方是发现 sink（经注入回调写出）和——在
// --expand-scope auto 下——有界二轮扫描。
package scope

import (
	"strings"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// maxEvents bounds the in-memory event list: a scan that meets a
// hostile network spraying fake NBNS responses must not balloon the
// tracker. / maxEvents 限制内存事件列表：遭遇用伪造 NBNS 响应轰炸的
// 恶意网络时 tracker 不会膨胀。
const maxEvents = 4096

// Tracker records out-of-scope discoveries, deduped by IP (hostname
// only events dedupe by hostname). In-scope sightings are not events,
// but their hostnames still land in the hostname cache so the servers
// inventory can enrich rows. All methods are safe for concurrent use.
// / Tracker 记录范围外发现，按 IP 去重（仅主机名的事件按主机名去
// 重）。范围内的目击不算事件，但其主机名仍进主机名缓存，供 servers
// 清单充实行。所有方法并发安全。
type Tracker struct {
	mu        sync.Mutex
	inScope   func(addr string) bool
	sink      func(types.DiscoveryEvent)
	seen      map[string]struct{}
	hostnames map[string]string
	events    []types.DiscoveryEvent
	dropped   int
}

// NewTracker wires an in-scope predicate (which addresses the operator
// already asked for) and an optional sink (the output discovery
// writer). Both may be nil: nil inScope treats everything as
// out-of-scope; nil sink only buffers events for later harvesting.
// / NewTracker 接入范围内判定谓词（操作员已要求哪些地址）与可选
// sink（输出发现写入器）。两者都可为 nil：inScope 为 nil 视一切为
// 范围外；sink 为 nil 只缓冲事件供后续取用。
func NewTracker(inScope func(addr string) bool, sink func(types.DiscoveryEvent)) *Tracker {
	return &Tracker{
		inScope:   inScope,
		sink:      sink,
		seen:      make(map[string]struct{}),
		hostnames: make(map[string]string),
	}
}

// Record stores one discovery event. Returns true when this is the
// first sighting (dedup key: IP, falling back to hostname). Events
// beyond maxEvents are counted and dropped — the sink still saw them
// (streaming), only the expansion buffer is bounded.
// / Record 存一条发现事件。首次见到时返回 true（去重键：IP，无 IP
// 时用主机名）。超出 maxEvents 的事件计数并丢弃——sink 已流式看到，
// 只有扩展缓冲是有界的。
func (t *Tracker) Record(ev types.DiscoveryEvent) bool {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	ev.IP = strings.TrimSpace(ev.IP)
	// Hostname cache: fill for every sighting with an IP, in or out of
	// scope — the servers inventory lookup path is scope-agnostic.
	// / 主机名缓存：凡带 IP 的目击都填，无论范围内外——servers 清单
	// 的查找路径不区分范围。
	if ev.Hostname != "" && ev.IP != "" {
		t.mu.Lock()
		if _, has := t.hostnames[strings.ToLower(ev.IP)]; !has {
			t.hostnames[strings.ToLower(ev.IP)] = ev.Hostname
		}
		t.mu.Unlock()
	}
	// In-scope sightings are not discoveries: the operator already
	// asked for them. Nothing to record beyond the hostname cache.
	// / 范围内的目击不算发现：操作员本来就扫它们。除主机名缓存外无
	// 可记录。
	if ev.IP != "" && t.IsInScope(ev.IP) {
		return false
	}
	key := ev.IP
	if key == "" {
		key = ev.Hostname
	}
	if key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, dup := t.seen[key]; dup {
		return false
	}
	t.seen[key] = struct{}{}
	if len(t.events) < maxEvents {
		t.events = append(t.events, ev)
	} else {
		t.dropped++
	}
	if t.sink != nil {
		t.sink(ev)
	}
	return true
}

// IsInScope reports whether addr is inside the operator's requested
// scope. Nil predicate = everything is out of scope.
// / IsInScope 报告 addr 是否在操作员请求的范围内。谓词为 nil = 一切
// 都在范围外。
func (t *Tracker) IsInScope(addr string) bool {
	if t.inScope == nil {
		return false
	}
	return t.inScope(addr)
}

// Events snapshots the buffered discovery events (for the expansion
// candidate filter). / Events 取缓冲发现事件的快照（供扩展候选过滤）。
func (t *Tracker) Events() []types.DiscoveryEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]types.DiscoveryEvent, len(t.events))
	copy(out, t.events)
	return out
}

// Dropped reports how many events fell off the bounded buffer.
// / Dropped 报告有多少事件从有界缓冲中掉落。
func (t *Tracker) Dropped() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

// Discovered reports how many out-of-scope NIC identities were seen
// (buffered + dropped) — the `discovered=N` summary tail.
// / Discovered 报告看到多少范围外网卡身份（缓冲 + 掉落）
// ——摘要的 discovered=N 尾巴。
func (t *Tracker) Discovered() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events) + t.dropped
}

// Hostname returns the best-known hostname for ip (first sighting
// wins), or "" when none was captured. Feeds the servers-inventory
// hostname enrichment. / Hostname 返回 ip 已知的最优主机名（首次目
// 击优先），未捕获时返回空串。喂给 servers 清单的主机名充实。
func (t *Tracker) Hostname(ip string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hostnames[strings.ToLower(strings.TrimSpace(ip))]
}
