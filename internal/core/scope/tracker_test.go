// tracker_test.go — discovery recording semantics: dedup by IP /
// hostname, in-scope sightings never recorded, bounded buffer with
// drop accounting, hostname cache (in AND out of scope), and the sink
// callback path.
//
// tracker_test.go — 发现记录语义：按 IP / 主机名去重、范围内目击不入
// 库、有界缓冲与丢弃计数、主机名缓存（范围内外都填）、sink 回调路径。
package scope

import (
	"fmt"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func TestTracker_InScopeNotRecorded(t *testing.T) {
	in, err := BuildInScopeMatcher("192.168.1.0/24", "")
	if err != nil {
		t.Fatalf("BuildInScopeMatcher: %v", err)
	}
	var sunk []types.DiscoveryEvent
	tr := NewTracker(in, func(e types.DiscoveryEvent) { sunk = append(sunk, e) })

	if tr.Record(types.DiscoveryEvent{IP: "192.168.1.5", SourceProtocol: "nbns"}) {
		t.Fatal("in-scope IP must not be recorded as a discovery")
	}
	if !tr.Record(types.DiscoveryEvent{IP: "10.9.0.6", Hostname: "nas", SourceProtocol: "nbns"}) {
		t.Fatal("out-of-scope IP must be recorded")
	}
	if len(sunk) != 1 || len(tr.Events()) != 1 {
		t.Fatalf("sink/events mismatch: sunk=%d events=%d", len(sunk), len(tr.Events()))
	}
}

func TestTracker_DedupByIPThenHostname(t *testing.T) {
	tr := NewTracker(nil, nil) // nil inScope = everything out of scope / 一切皆范围外

	if !tr.Record(types.DiscoveryEvent{IP: "10.0.0.9", SourceProtocol: "mdns"}) {
		t.Fatal("first sighting must return true")
	}
	if tr.Record(types.DiscoveryEvent{IP: "10.0.0.9", SourceProtocol: "tls-san"}) {
		t.Fatal("second sighting of same IP must return false (dedup)")
	}
	// Hostname-only events dedupe by hostname. / 仅主机名事件按主机名去重。
	if !tr.Record(types.DiscoveryEvent{Hostname: "printserver", SourceProtocol: "nbns"}) {
		t.Fatal("first hostname-only sighting must return true")
	}
	if tr.Record(types.DiscoveryEvent{Hostname: "printserver", SourceProtocol: "nbns"}) {
		t.Fatal("same hostname again must dedup")
	}
	// Neither IP nor hostname = nothing to key on. / 无 IP 无主机名无从谈起。
	if tr.Record(types.DiscoveryEvent{SourceProtocol: "x"}) {
		t.Fatal("empty key event must not be recorded")
	}
	if got := len(tr.Events()); got != 2 {
		t.Fatalf("want 2 events, got %d", got)
	}
	if got := tr.Discovered(); got != 2 {
		t.Fatalf("Discovered = %d, want 2", got)
	}
}

func TestTracker_HostnameCacheInAndOutOfScope(t *testing.T) {
	// In-scope 10.0.0.1: not an event, but the hostname MUST still be
	// cached — the servers inventory enriches via this path.
	// / 范围内 10.0.0.1：不是事件，但主机名必须仍进缓存——servers 清
	// 单走这条路径充实行。
	tr := NewTracker(func(addr string) bool { return addr == "10.0.0.1" }, nil)

	tr.Record(types.DiscoveryEvent{IP: "10.0.0.1", Hostname: "dc01.corp", SourceProtocol: "nbstat"})
	tr.Record(types.DiscoveryEvent{IP: "10.0.0.2", Hostname: "NAS-01", SourceProtocol: "nbstat"})
	tr.Record(types.DiscoveryEvent{IP: "10.0.0.2", Hostname: "IGNORED", SourceProtocol: "mdns"}) // first wins / 首次优先

	for ip, want := range map[string]string{
		"10.0.0.1": "dc01.corp", // in scope, cached / 范围内，已缓存
		"10.0.0.2": "NAS-01",    // out of scope, first wins / 范围外，首次优先
		"10.9.9.9": "",          // unknown / 未知
	} {
		if got := tr.Hostname(ip); got != want {
			t.Errorf("Hostname(%q) = %q, want %q", ip, got, want)
		}
	}
	// Only the out-of-scope one is an event. / 只有范围外那条算事件。
	if got := len(tr.Events()); got != 1 {
		t.Fatalf("want 1 event, got %d", got)
	}
}

func TestTracker_BoundedBufferDrops(t *testing.T) {
	tr := NewTracker(nil, nil)
	for i := 0; i < maxEvents+25; i++ {
		tr.Record(types.DiscoveryEvent{
			IP:             fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff),
			SourceProtocol: "fuzz",
		})
	}
	if got := len(tr.Events()); got != maxEvents {
		t.Fatalf("buffer must be capped at %d, got %d", maxEvents, got)
	}
	if got := tr.Dropped(); got != 25 {
		t.Fatalf("Dropped = %d, want 25", got)
	}
	if got := tr.Discovered(); got != maxEvents+25 {
		t.Fatalf("Discovered = %d, want %d", got, maxEvents+25)
	}
}

func TestTracker_TimeStampedWhenZero(t *testing.T) {
	tr := NewTracker(nil, nil)
	tr.Record(types.DiscoveryEvent{IP: "10.0.0.3", SourceProtocol: "arp"})
	e := tr.Events()[0]
	if e.Time.IsZero() {
		t.Fatal("Record must stamp zero Time")
	}
}
