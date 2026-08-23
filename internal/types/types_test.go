// types_test.go — focused unit tests for internal/types, the leaf
// package of the cmd/ + internal/* layout. The package is the root
// of the runtime state (Config, State, Result, Cred, Target) and
// every other package imports it, so the test surface is the
// cheapest safety net for the refactor.
//
// types_test.go — internal/types（cmd/ + internal/* 布局中的叶子包）
// 的单测。本包是运行时状态的根（Config、State、Result、Cred、Target），
// 其他包都 import 它，所以测试覆盖面就是重构最便宜的安全网。
package types

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/config"
)

// --- Config validation ---

// TestConfigValidateRejectsZeroThreads / Timeout / ShutdownTimeout.
func TestConfigValidateRejectsZeroOrNegative(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"threads=0", func(c *Config) { c.Threads = 0 }, "threads"},
		{"threads=-1", func(c *Config) { c.Threads = -5 }, "threads"},
		{"timeout=0", func(c *Config) { c.Timeout = 0 }, "timeout"},
		{"shutdown-timeout=0", func(c *Config) { c.ShutdownTimeout = 0 }, "shutdown-timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second}
			tc.mut(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

// TestConfigValidateAcceptsValidMode: the three documented run modes
// are accepted; anything else is rejected.
func TestConfigValidateAcceptsValidMode(t *testing.T) {
	for _, m := range []RunMode{ModeScan, ModeCrack, ModeLinked} {
		c := &Config{
			Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second,
			Mode: m,
		}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(mode=%q) = %v, want nil", m, err)
		}
	}
	// Empty mode defaults to ModeScan (documented behaviour).
	t.Run("empty defaults to scan", func(t *testing.T) {
		c := &Config{Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(empty mode): %v", err)
		}
		if c.Mode != ModeScan {
			t.Errorf("Mode = %q, want %q (default)", c.Mode, ModeScan)
		}
	})
	t.Run("invalid mode rejected", func(t *testing.T) {
		c := &Config{
			Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second,
			Mode: "bogus",
		}
		if err := c.Validate(); err == nil {
			t.Error("Validate(bogus mode) = nil, want error")
		}
	})
}

// TestConfigValidateResumeRequiresProject: -resume without -p is
// rejected so users don't silently restart a scan that has no
// persistent state.
func TestConfigValidateResumeRequiresProject(t *testing.T) {
	c := &Config{
		Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second,
		Resume: true, Project: "",
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate(Resume w/o Project) = nil, want error")
	}
	c.Project = "corp"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate(Resume w/ Project): %v", err)
	}
}

// --- ParsePorts / ParseExcludePorts ---

// TestParsePorts: empty, single, multiple, out-of-range, malformed.
func TestParsePorts(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   []int
		errSub string // substring expected in error; "" = no error
	}{
		// Empty/whitespace input returns the default port list (not nil).
		// / 空/空白输入返回默认端口列表（不是 nil）。
		{"empty", "", config.DefaultPorts(), ""},
		{"whitespace only", "   ", config.DefaultPorts(), ""},
		{"single", "80", []int{80}, ""},
		{"multiple", "22,80,443", []int{22, 80, 443}, ""},
		{"with spaces", " 22 , 80 ", []int{22, 80}, ""},
		{"out of range high", "70000", nil, "out of range"},
		{"out of range low", "0", nil, "out of range"},
		{"non-numeric", "abc", nil, "invalid port"},
		{"empty segment", "22,,80", []int{22, 80}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Ports: tc.input}
			got, err := c.ParsePorts()
			if tc.errSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errSub) {
					t.Errorf("ParsePorts(%q) err = %v, want containing %q", tc.input, err, tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePorts(%q): %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParsePorts(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseExcludePorts: same contract as ParsePorts. The two methods
// are thin wrappers; testing one and asserting the other shares the
// same input handling catches future drift.
func TestParseExcludePorts(t *testing.T) {
	c := &Config{ExcludePorts: "22,80"}
	got, err := c.ParseExcludePorts()
	if err != nil {
		t.Fatalf("ParseExcludePorts: %v", err)
	}
	want := []int{22, 80}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseExcludePorts = %v, want %v", got, want)
	}
}

// --- ResolvePorts (Task 3) ---
//
// ResolvePorts is the single source of truth for the effective port
// list at scan start: it parses include + exclude, dedupes, sorts,
// and returns errors for invalid input. The previous behaviour was
// that ExcludePorts was stored in cfg but never read by RunScan, so
// `-exclude-ports 80` had no effect on the scan. ResolvePorts closes
// that gap and is called before any goroutine starts.
//
// / --- ResolvePorts（Task 3）---
//
// ResolvePorts 是扫描启动时有效端口列表的唯一真相源：解析 include +
// exclude、去重、排序，并对非法输入返回 error。旧行为是 ExcludePorts
// 存在 cfg 但 RunScan 从不读取，导致 `-exclude-ports 80` 对扫描完全
// 无效。ResolvePorts 修补此缺口，且在任何 goroutine 启动前调用。

// TestResolvePortsExcludesConfiguredPorts — the canonical case: a
// single include port is excluded; result is the remainder.
func TestResolvePortsExcludesConfiguredPorts(t *testing.T) {
	cfg := &Config{Ports: "22,80,443", ExcludePorts: "80"}
	got, err := cfg.ResolvePorts()
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	want := []int{22, 443}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolvePorts(22,80,443 - 80) = %v, want %v", got, want)
	}
}

// TestResolvePortsDedupesExclude — if the exclude list repeats a
// port, ResolvePorts must not error and must still exclude the
// single canonical entry.
func TestResolvePortsDedupesExclude(t *testing.T) {
	cfg := &Config{Ports: "22,80,443", ExcludePorts: "80,80,443"}
	got, err := cfg.ResolvePorts()
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	want := []int{22}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolvePorts(dedup exclude) = %v, want %v", got, want)
	}
}

// TestResolvePortsSortedAscending — output is sorted ascending
// regardless of input order, so downstream iterators and dedup
// checks behave deterministically.
func TestResolvePortsSortedAscending(t *testing.T) {
	cfg := &Config{Ports: "443,22,80"}
	got, err := cfg.ResolvePorts()
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	want := []int{22, 80, 443}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolvePorts(443,22,80) = %v, want %v (ascending)", got, want)
	}
}

// TestResolvePortsInvalidInclude — invalid include syntax surfaces
// as an error so RunScan aborts before starting workers. Scanning
// with 0 ports is meaningless and previously produced silent
// "no findings" results.
func TestResolvePortsInvalidInclude(t *testing.T) {
	cfg := &Config{Ports: "abc"}
	_, err := cfg.ResolvePorts()
	if err == nil {
		t.Fatal("ResolvePorts(Ports=abc) returned err=nil, want non-nil")
	}
}

// TestResolvePortsOutOfRange — out-of-range port surfaces as an
// error.
func TestResolvePortsOutOfRange(t *testing.T) {
	cfg := &Config{Ports: "99999"}
	_, err := cfg.ResolvePorts()
	if err == nil {
		t.Fatal("ResolvePorts(Ports=99999) returned err=nil, want non-nil")
	}
}

// TestResolvePortsInvalidExclude — invalid exclude syntax surfaces
// as an error.
func TestResolvePortsInvalidExclude(t *testing.T) {
	cfg := &Config{Ports: "22,80", ExcludePorts: "abc"}
	_, err := cfg.ResolvePorts()
	if err == nil {
		t.Fatal("ResolvePorts(ExcludePorts=abc) returned err=nil, want non-nil")
	}
}

// TestResolvePortsAllExcluded — if every port is excluded, the
// effective list is empty; ResolvePorts must return an error
// (running with 0 ports is meaningless and would silently produce
// "no findings").
func TestResolvePortsAllExcluded(t *testing.T) {
	cfg := &Config{Ports: "22,80", ExcludePorts: "22,80"}
	_, err := cfg.ResolvePorts()
	if err == nil {
		t.Fatal("ResolvePorts(all excluded) returned err=nil, want non-nil")
	}
}

// TestResolvePortsEmptyExclude — empty exclude string is a no-op;
// ResolvePorts returns the include list (default ports if Ports
// is also empty).
func TestResolvePortsEmptyExclude(t *testing.T) {
	cfg := &Config{Ports: "22,80", ExcludePorts: ""}
	got, err := cfg.ResolvePorts()
	if err != nil {
		t.Fatalf("ResolvePorts: %v", err)
	}
	want := []int{22, 80}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolvePorts(Ports=22,80, no exclude) = %v, want %v", got, want)
	}
}

// --- HashKey + State ---

// TestHashKeyDeterminism: same inputs → same hash. The pipeline uses
// this as the dedup key for the (host, port, plugin, op) tuple.
func TestHashKeyDeterminism(t *testing.T) {
	a := HashKey("10.0.0.1", "22", "ssh", "identify")
	b := HashKey("10.0.0.1", "22", "ssh", "identify")
	if a != b {
		t.Errorf("HashKey not deterministic: %q vs %q", a, b)
	}
	if a == "" {
		t.Error("HashKey returned empty string")
	}
}

// TestHashKeyDifferentiatesInputs: a 1-bit change in any input
// produces a different hash. Tested via the first byte of each input.
func TestHashKeyDifferentiatesInputs(t *testing.T) {
	base := HashKey("10.0.0.1", "22", "ssh", "identify")
	cases := [][]string{
		{"10.0.0.2", "22", "ssh", "identify"},   // different host
		{"10.0.0.1", "80", "ssh", "identify"},   // different port
		{"10.0.0.1", "22", "ftp", "identify"},   // different plugin
		{"10.0.0.1", "22", "ssh", "credential"}, // different op
	}
	for _, parts := range cases {
		got := HashKey(parts...)
		if got == base {
			t.Errorf("HashKey(%v) == base %q; expected difference", parts, base)
		}
	}
}

// TestStateMarkSeenDedup: MarkSeen returns true the first time, false
// thereafter. The pipeline relies on this for "first-match wins" dedup.
func TestStateMarkSeenDedup(t *testing.T) {
	s := NewState()
	if !s.MarkSeen("h1") {
		t.Error("first MarkSeen(h1) returned false; want true")
	}
	if s.MarkSeen("h1") {
		t.Error("second MarkSeen(h1) returned true; want false")
	}
	if !s.MarkSeen("h2") {
		t.Error("MarkSeen(h2) returned false; want true")
	}
	if s.Seen("h1") != true || s.Seen("h3") != false {
		t.Errorf("Seen: h1=%v h3=%v; want true/false", s.Seen("h1"), s.Seen("h3"))
	}
}

// TestStateSnapshot: a freshly-created State has all counters at 0
// and the view reflects subsequent Add() operations.
func TestStateSnapshot(t *testing.T) {
	s := NewState()
	v := s.Snapshot()
	if v.Alive != 0 || v.Ports != 0 || v.Results != 0 || v.Creds != 0 || v.Errors != 0 {
		t.Errorf("fresh Snapshot = %+v, want all zero", v)
	}
	s.Counters.Alive.Add(3)
	s.Counters.Ports.Add(7)
	v = s.Snapshot()
	if v.Alive != 3 || v.Ports != 7 {
		t.Errorf("after Adds, Snapshot = %+v, want Alive=3 Ports=7", v)
	}
}

// TestStatePause removed in v0.2 audit (P2 dead-code purge — no
// consumer of SetPaused / IsPaused). If a real pause path is added
// later, restore this test alongside the production code.
//
// TestStatePause 在 v0.2 审计中移除（P2 死代码清理——无 SetPaused /
// IsPaused 的消费者）。将来若加真实暂停路径，把本测试连同生产代码
// 一起恢复。

// TestStateConcurrentMarkSeen: the docstring guarantees
// "concurrent-safe across goroutines". This test hammers
// MarkSeen/Seen from multiple goroutines to catch data races
// (run with -race to surface them).
func TestStateConcurrentMarkSeen(t *testing.T) {
	s := NewState()
	const goroutines = 16
	const perG = 1000
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				key := HashKey("h", "p", "n", string(rune('a'+id%26)), string(rune('a'+i%26)))
				s.MarkSeen(key)
				_ = s.Seen(key)
			}
		}(g)
	}
	wg.Wait()
}

// --- Target / Result / Cred ---

// TestTargetKey: empty Addr returns ""; non-empty returns the Addr.
func TestTargetKey(t *testing.T) {
	if k := (Target{}).Key(); k != "" {
		t.Errorf("empty Target.Key() = %q, want \"\"", k)
	}
	if k := (Target{Addr: "10.0.0.1"}).Key(); k != "10.0.0.1" {
		t.Errorf("Target{10.0.0.1}.Key() = %q, want \"10.0.0.1\"", k)
	}
}

// TestResultZeroValue: zero-value Result is well-formed (no panics,
// fields are at their zero values).
func TestResultZeroValue(t *testing.T) {
	var r Result
	if r.Host != "" || r.Port != 0 || r.Banner != "" || r.Cred != nil || r.Plugin != "" {
		t.Errorf("zero Result = %+v, want all zero", r)
	}
}

// TestResultWithCred: setting Cred populates the indirection.
func TestResultWithCred(t *testing.T) {
	r := Result{Host: "10.0.0.1", Port: 22, Cred: &Cred{User: "u", Pass: "p"}}
	if r.Cred == nil || r.Cred.User != "u" || r.Cred.Pass != "p" {
		t.Errorf("Result.Cred = %+v, want {u p}", r.Cred)
	}
}

// --- Logger ---

// TestDiscardLoggerNoOp: every method returns without panic on a
// discard logger (the default in session.NewSession).
func TestDiscardLoggerNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("DiscardLogger panicked: %v", r)
		}
	}()
	l := DiscardLogger{}
	l.Info("hi %d", 1)
	l.Warn("hi %d", 2)
	l.Error("hi %d", 3)
	l.Debug("hi %d", 4)
	l.Success("hi %d", 5)
}

// TestStderrLoggerFormat: the NewLoggerTo writer receives lines in
// the documented "HH:MM:SS LEVEL message\n" shape. (Timestamp is
// always present; level tag is the documented "*[!.-]"; body is
// the format string with args applied.)
func TestStderrLoggerFormat(t *testing.T) {
	var buf bytes.Buffer
	l := NewLoggerTo(&buf)
	l.Info("hello %s", "world")
	out := buf.String()
	// Expect: "<5-digit-time> [*] hello world\n" — match the suffix,
	// not the exact time (we don't freeze the clock in this test).
	if !strings.HasSuffix(out, " [*] hello world\n") {
		t.Errorf("Info output missing expected suffix; got %q", out)
	}
	// Timestamp must be 8 chars (HH:MM:SS) + space.
	if len(out) < len("00:00:00 [*] hello world\n") {
		t.Errorf("Info output too short: %q", out)
	}
	l.Error("oops %d", 42)
	if !strings.Contains(buf.String(), "[-] oops 42") {
		t.Errorf("Error output missing; full buf = %q", buf.String())
	}
}

// TestLoggerInterfaceConformance: both concrete loggers satisfy
// the Logger interface. Compile-time guarantee via the var
// declarations; this test is a smoke for the reflection-based
// interface satisfaction check.
func TestLoggerInterfaceConformance(t *testing.T) {
	var _ Logger = DiscardLogger{}
	var _ Logger = NewStderrLogger()
	var _ Logger = NewLoggerTo(&bytes.Buffer{})
}

// --- ExpandTargets ---

// TestExpandTargetsSingleIP: a bare IP expands to one Target.
func TestExpandTargetsSingleIP(t *testing.T) {
	got, err := ExpandTargets("10.0.0.1", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 1 || got[0].Addr != "10.0.0.1" {
		t.Errorf("got %v, want [{10.0.0.1}]", got)
	}
}

// TestExpandTargetsCIDR: a /30 expands to 4 addresses.
func TestExpandTargetsCIDR(t *testing.T) {
	got, err := ExpandTargets("10.0.0.0/30", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("got %d targets, want 4 (/30 = 4 addrs)", len(got))
	}
	if got[0].Addr != "10.0.0.0" || got[3].Addr != "10.0.0.3" {
		t.Errorf("got %v, want first=10.0.0.0 last=10.0.0.3", got)
	}
}

// TestExpandTargetsRange: "10.0.0.1-10.0.0.3" expands to 3 addrs.
func TestExpandTargetsRange(t *testing.T) {
	got, err := ExpandTargets("10.0.0.1-10.0.0.3", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d targets, want 3", len(got))
	}
}

// TestExpandTargetsIPv6Single: a bare IPv6 literal expands to one
// Target. Phase B (audit roadmap): IPv6 first-class support.
// TestExpandTargetsIPv6Single：裸 IPv6 字面量展开为一个 Target。
// Phase B（审计路线图）：IPv6 一等公民。
func TestExpandTargetsIPv6Single(t *testing.T) {
	got, err := ExpandTargets("::1", "")
	if err != nil {
		t.Fatalf("ExpandTargets(::1): %v", err)
	}
	if len(got) != 1 || got[0].Addr != "::1" {
		t.Errorf("got %v, want [{::1}]", got)
	}
}

// TestExpandTargetsIPv6CIDR: a /124 IPv6 CIDR expands to 16 addrs.
// TestExpandTargetsIPv6CIDR：IPv6 /124 CIDR 展开为 16 地址。
func TestExpandTargetsIPv6CIDR(t *testing.T) {
	got, err := ExpandTargets("2001:db8::/124", "")
	if err != nil {
		t.Fatalf("ExpandTargets(2001:db8::/124): %v", err)
	}
	if len(got) != 16 {
		t.Errorf("got %d targets, want 16 (/124 = 16 addrs)", len(got))
	}
	if got[0].Addr != "2001:db8::" {
		t.Errorf("got[0] = %q, want 2001:db8::", got[0].Addr)
	}
	if got[15].Addr != "2001:db8::f" {
		t.Errorf("got[15] = %q, want 2001:db8::f", got[15].Addr)
	}
}

// TestExpandTargetsIPv6CommaList: comma-separated IPv6 list expands
// to N targets. Phase B (audit roadmap). / Phase B：逗号分隔 IPv6
// 列表展开为 N 个目标。
func TestExpandTargetsIPv6CommaList(t *testing.T) {
	got, err := ExpandTargets("::1,fe80::1,2001:db8::1", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d targets, want 3", len(got))
	}
	// Verify all three addrs are present (order may vary due to
	// map-based dedup). / 验证三个地址都在（顺序可能因 map 去
	// 重而变）。
	want := map[string]bool{"::1": true, "fe80::1": true, "2001:db8::1": true}
	for _, t := range got {
		delete(want, t.Addr)
	}
	if len(want) > 0 {
		t.Errorf("missing addrs: %v", want)
	}
}

// TestExpandTargetsDedupe: repeated spec (or a spec that produces
// duplicates) is de-duplicated in the output.
func TestExpandTargetsDedupe(t *testing.T) {
	got, err := ExpandTargets("10.0.0.1,10.0.0.1,10.0.0.1", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1 (deduped)", len(got))
	}
}

// TestExpandTargetsHostsFile: a hosts file with one IP per line is
// parsed; blank lines and `#` comments are skipped.
func TestExpandTargetsHostsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.txt")
	body := "# this is a comment\n\n10.0.0.5\n10.0.0.6  # inline comment\n# another\n10.0.0.7\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write hosts: %v", err)
	}
	got, err := ExpandTargets("", path)
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	want := []string{"10.0.0.5", "10.0.0.6", "10.0.0.7"}
	gotAddrs := make([]string, len(got))
	for i, t := range got {
		gotAddrs[i] = t.Addr
	}
	if !reflect.DeepEqual(gotAddrs, want) {
		t.Errorf("got %v, want %v", gotAddrs, want)
	}
}

// TestExpandTargetsHostsFileMissing: a missing hosts file returns
// a clear error rather than a confusing os.IsNotExist wrap.
func TestExpandTargetsHostsFileMissing(t *testing.T) {
	_, err := ExpandTargets("", filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Error("ExpandTargets on missing file = nil, want error")
	}
}

// TestExpandTargetsCIDRInvalid: an invalid CIDR is surfaced as an
// error rather than silently producing 0 targets.
func TestExpandTargetsCIDRInvalid(t *testing.T) {
	_, err := ExpandTargets("not-a-cidr", "")
	if err == nil {
		t.Error("ExpandTargets(invalid CIDR) = nil, want error")
	}
}

// TestExpandTargetsRangeEndBeforeStart: a range whose end precedes
// the start (e.g. "0.0.0.1-0" — the bare-octet end expands below the
// start's last octet) must return an error rather than iterating
// through ~2^32 addresses before incIP wraps around. This is the
// regression for the FuzzExpandTargets hang the daily fuzz run hit.
// / TestExpandTargetsRangeEndBeforeStart：end 小于 start 的范围
// （如 "0.0.0.1-0" 单段 end 展开后小于 start）必须返回 error，
// 而不是遍历 ~2³² 个地址再让 incIP 绕回。这是对 FuzzExpandTargets
// 每日挂起的回归测试。
func TestExpandTargetsRangeEndBeforeStart(t *testing.T) {
	cases := []struct {
		name string
		spec string
	}{
		{"bare-octet-end-lower", "0.0.0.1-0"},
		{"bare-octet-end-lower-mid", "10.0.0.5-1"},
		{"full-form-end-lower", "10.0.0.5-10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Guard with a short deadline so a regression still
			// fails fast. / 配以短截止时间，回归时仍能快速失败。
			done := make(chan struct{})
			var err error
			go func() {
				_, err = ExpandTargets(tc.spec, "")
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("ExpandTargets(%q) did not return within 2s — infinite range expansion", tc.spec)
			}
			if err == nil {
				t.Errorf("ExpandTargets(%q) = nil err, want error (end precedes start)", tc.spec)
			}
		})
	}
}

// TestExpandTargetsRangeTooLarge: a syntactically valid range whose
// size exceeds MaxTargets must return an error rather than letting
// the expansion loop iterate an astronomical number of times before
// the streaming cap in tryEmit ever fires. Examples:
//   - "::-8::"                     → 2^123 addresses
//   - "0.0.0.0-255.255.255.255"    → ~2^32 addresses
//
// Regression for the second FuzzExpandTargets hang discovered during
// the first fix's regression sweep.
// / TestExpandTargetsRangeTooLarge：合法但超过 MaxTargets 的范围
// 必须返回 error，而不是在天文级数迭代后才触发 tryEmit 的流式上限。
// 例如 "::-8::"（2^123）、"0.0.0.0-255.255.255.255"（~2^32）。
// 这是首次修复回归扫描中发现的第二个 FuzzExpandTargets 挂起的回归。
func TestExpandTargetsRangeTooLarge(t *testing.T) {
	cases := []struct {
		name string
		spec string
	}{
		{"ipv6-mega-range", "::-8::"},
		{"ipv4-full-space", "0.0.0.0-255.255.255.255"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan struct{})
			var err error
			go func() {
				_, err = ExpandTargets(tc.spec, "")
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("ExpandTargets(%q) did not return within 2s — range size not bounded", tc.spec)
			}
			if err == nil {
				t.Errorf("ExpandTargets(%q) = nil err, want error (range too large)", tc.spec)
			}
		})
	}
}

// --- RunMode constants ---

// TestRunModeValues: the three documented run modes are scan / crack /
// linked; any other constant would indicate a straggler.
func TestRunModeValues(t *testing.T) {
	want := map[RunMode]string{
		ModeScan:   "scan",
		ModeCrack:  "crack",
		ModeLinked: "linked",
	}
	for m, v := range want {
		if string(m) != v {
			t.Errorf("RunMode %v = %q, want %q", m, string(m), v)
		}
	}
}

// TestConfigValidateProjectAndHostOptional: a config with no
// Project AND no Host is valid (e.g. for `projects list`).
func TestConfigValidateProjectAndHostOptional(t *testing.T) {
	c := &Config{Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate(no project, no host): %v", err)
	}
}

// TestCredentialContextCanceled: nothing in types depends on context,
// but importing it ensures the package compiles with the context
// import already in scope. (Compile-time smoke.)
func TestCredentialContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Error("canceled ctx should have non-nil Err")
	}
}

// --- v0.4 streaming Host Iterator ---

// TestExpandTargetsStreamSingleIP: streaming iterator yields the same
// single target as ExpandTargets. / 流式迭代器产出与 ExpandTargets 相同的
// 单目标。
func TestExpandTargetsStreamSingleIP(t *testing.T) {
	it, err := ExpandTargetsStream("10.0.0.1", "")
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	t1, ok := it.Next()
	if !ok || t1.Addr != "10.0.0.1" {
		t.Errorf("got (%v, %v), want (10.0.0.1, true)", t1, ok)
	}
	if _, ok := it.Next(); ok {
		t.Error("expected exhausted after one target")
	}
}

// TestExpandTargetsStreamCIDR: a /30 expands to 4 addrs streamed.
// / 流式 /30 展开为 4 个地址。
func TestExpandTargetsStreamCIDR(t *testing.T) {
	it, err := ExpandTargetsStream("10.0.0.0/30", "")
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	count := 0
	var first, last Target
	for t, ok := it.Next(); ok; t, ok = it.Next() {
		if count == 0 {
			first = t
		}
		last = t
		count++
	}
	if count != 4 {
		t.Errorf("got %d, want 4", count)
	}
	if first.Addr != "10.0.0.0" || last.Addr != "10.0.0.3" {
		t.Errorf("got first=%v last=%v, want first=10.0.0.0 last=10.0.0.3", first, last)
	}
}

// TestExpandTargetsStreamEstimatedCIDR: a CIDR iterator reports an
// accurate Estimated(). / CIDR 迭代器报告准确的 Estimated()。
func TestExpandTargetsStreamEstimatedCIDR(t *testing.T) {
	it, err := ExpandTargetsStream("10.0.0.0/30", "")
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	if e := it.Estimated(); e != 4 {
		t.Errorf("Estimated() = %d, want 4", e)
	}
}

// TestExpandTargetsStreamEstimatedUnknown: an iterator with a hosts
// file or comma-list reports -1 (unknown). / hosts 文件或逗号列表
// 迭代器返回 -1（未知）。
func TestExpandTargetsStreamEstimatedUnknown(t *testing.T) {
	it, err := ExpandTargetsStream("10.0.0.1,10.0.0.2", "")
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	if e := it.Estimated(); e != -1 {
		t.Errorf("Estimated() = %d, want -1 (comma-list unknown)", e)
	}
}

// TestExpandTargetsStreamDedupe: dedup is honoured during streaming.
// / 流式过程中遵守去重。
func TestExpandTargetsStreamDedupe(t *testing.T) {
	it, err := ExpandTargetsStream("10.0.0.1,10.0.0.1,10.0.0.2", "")
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	seen := map[string]bool{}
	for t, ok := it.Next(); ok; t, ok = it.Next() {
		seen[t.Addr] = true
	}
	if len(seen) != 2 {
		t.Errorf("got %d unique, want 2", len(seen))
	}
}

// TestExpandTargetsStreamHostsFile: streaming reads from a hosts file.
// / 流式从 hosts 文件读取。
func TestExpandTargetsStreamHostsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.txt")
	body := "# comment\n10.0.0.5\n10.0.0.6\n10.0.0.7\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write hosts: %v", err)
	}
	it, err := ExpandTargetsStream("", path)
	if err != nil {
		t.Fatalf("ExpandTargetsStream: %v", err)
	}
	count := 0
	for t, ok := it.Next(); ok; t, ok = it.Next() {
		count++
		_ = t
	}
	if count != 3 {
		t.Errorf("got %d, want 3", count)
	}
}

// TestExpandTargetsStreamBackCompat: ExpandTargets still works (used as
// the slice-returning wrapper for callers that need the full list).
// / ExpandTargets 仍可用（作为返回 slice 的包装给需要全表的调用方）。
func TestExpandTargetsStreamBackCompat(t *testing.T) {
	got, err := ExpandTargets("10.0.0.0/30", "")
	if err != nil {
		t.Fatalf("ExpandTargets: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("got %d, want 4", len(got))
	}
}
