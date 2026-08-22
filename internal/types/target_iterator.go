// target_iterator.go — streaming Host Iterator (v0.4).
// target_iterator.go — 流式主机迭代器（v0.4）。
//
// ExpandTargets (network.go) materializes the full target list up-front
// and caps it at MaxTargets (M5 audit fix). That cap is correct for
// preventing OOM, but it rejects /8-class scans before they can run.
//
// ExpandTargets（network.go）一次性物化全部目标并由 MaxTasks 限幅（M5 审
// 计修法）。该限幅对防 OOM 是必要的，但会在扫描前就拒绝 /8 级输入。
//
// ExpandTargetsStream streams targets lazily so the full list never
// exists in memory. The dedup map still grows with the number of unique
// IPs (unavoidable), but the slice does not. The MaxTargets cap is
// preserved as a streaming guard — iteration stops with an error once
// the cap is exceeded, which surfaces the same "too many targets"
// diagnostic the eager form does.
//
// ExpandTargetsStream 懒产出目标，使全表从不会同时驻留内存。dedup
// 映射仍会随唯一 IP 数增长（不可避免），但 slice 不会。MaxTargets 上限
// 保留为流式护栏——迭代在超限时以错误中止，与 eager 版本抛出相同的"too
// many targets"诊断。
package types

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// emitFunc is the streaming callback. / emitFunc 是流式回调。
type emitFunc func(Target) error

// ExpandTargetsStream returns a TargetIterator that yields targets
// from spec and/or hostsFile without materializing the full list.
// / ExpandTargetsStream 返回从 spec 和/或 hostsFile 流式产出目标的
// TargetIterator，不物化全表。
func ExpandTargetsStream(spec, hostsFile string) (TargetIterator, error) {
	it := &targetIter{
		spec:      spec,
		hostsFile: hostsFile,
		seen:      make(map[string]struct{}),
		cap:       MaxTargets,
	}
	if err := it.init(); err != nil {
		return nil, err
	}
	return it, nil
}

// TargetIterator yields Target values lazily. / TargetIterator 懒产出 Target 值。
type TargetIterator interface {
	// Next returns the next target, or (zero, false) when exhausted or
	// when an error occurred (call Err after exhaustion to check).
	// Next 返回下一个目标；耗尽或出错时返回 (zero, false)（耗尽后调
	// Err 检查）。
	Next() (Target, bool)
	// Estimated returns the rough total count, or -1 if unknown. For
	// a pure CIDR input this is exact; for a hosts file or comma list
	// the iterator returns -1. / Estimated 返回大致总数，未知返 -1。
	Estimated() int
	// Err returns the first error encountered during iteration, or
	// nil. / Err 返回迭代过程中遇到的第一个错误，nil 表示无错。
	Err() error
}

// targetIter is the default TargetIterator implementation. / targetIter
// 是默认 TargetIterator 实现。
type targetIter struct {
	spec      string
	hostsFile string
	seen      map[string]struct{}
	cap       int

	// estimate: -1 for unknown, exact count for pure-CIDR/range,
	// 1 for single IP / hostname. / estimate：-1 未知；纯 CIDR/范围
	// 是准确值；单 IP / 主机名是 1。
	estimate int

	// step advances state to the next target and emits it via the
	// emit callback. Returns false when exhausted or on error.
	// step 推进状态到下一个目标并通过 emit 回调产出。耗尽或出错返
	// false。
	step func(emit emitFunc) (more bool, err error)

	// Hosts-file scanner state (only used when hostsFile is set).
	// hosts 文件 scanner 状态（仅当 hostsFile 设置时使用）。
	fileScanner *bufio.Scanner
	fileOpen    *os.File

	lastErr error
	done    bool
}

// init parses spec/hostsFile to (a) surface syntax errors early and
// (b) set estimate for inputs whose size is computable. / init 解析
// spec/hostsFile 以（a）让语法错误早期浮出，（b）为可计算大小的输入设
// 置 estimate。
func (t *targetIter) init() error {
	if t.spec == "" && t.hostsFile == "" {
		t.estimate = 0
		t.step = func(emit emitFunc) (bool, error) { return false, nil }
		return nil
	}
	if t.spec != "" && t.hostsFile == "" {
		return t.initSpec(t.spec)
	}
	// hosts file or spec+file: estimate unknown up-front. / hosts 文
	// 件或 spec+文件：estimate 事前未知。
	t.estimate = -1
	t.step = t.stepFileOrSpec
	if t.hostsFile != "" {
		f, err := os.Open(t.hostsFile)
		if err != nil {
			return fmt.Errorf("open hosts file: %w", err)
		}
		t.fileOpen = f
		t.fileScanner = bufio.NewScanner(f)
	}
	return nil
}

// initSpec initialises for a spec-only input. For single-piece specs
// (bare IP / CIDR / range / hostname) we pick the most precise step
// function so Estimated() is accurate. / initSpec 为纯 spec 输入初
// 始化。对单段 spec（裸 IP / CIDR / 范围 / 主机名）我们选最精确的 step
// 函数，使 Estimated() 准确。
func (t *targetIter) initSpec(spec string) error {
	pieces := strings.Split(spec, ",")
	if len(pieces) == 1 {
		return t.initOne(pieces[0])
	}
	t.estimate = -1
	// Stateful comma-list: track which piece index we're on. Each
	// step call advances i by 1, expands pieces[i] (which itself may
	// produce multiple targets), and emits ONE target per step.
	// / 状态化逗号列表：跟踪 piece 下标。每次 step 自增 i，展开
	// pieces[i]（可能产生多个目标），每次 emit 1 个。
	var i int
	t.step = func(emit emitFunc) (bool, error) {
		for i < len(pieces) {
			piece := strings.TrimSpace(pieces[i])
			i++ // consume this piece
			if piece == "" {
				continue
			}
			// emitOne may produce 0..N targets. We capture only the
			// first one per step call. / emitOne 可产生 0..N 目标。
			// 每次 step 仅捕获第一个。
			var emittedInPiece bool
			wrapped := func(target Target) error {
				if emittedInPiece {
					return nil
				}
				emittedInPiece = true
				return emit(target)
			}
			if err := t.emitOne(piece, wrapped); err != nil {
				return false, err
			}
			if emittedInPiece {
				return true, nil
			}
			// Piece produced nothing — try next. / 该 piece 没产物——试下一个。
		}
		return false, nil
	}
	return nil
}

// initOne handles single-piece spec: bare IP, CIDR, range, or hostname.
// For CIDR / range we compute the exact target count up-front.
// / initOne 处理单段 spec：裸 IP / CIDR / 范围 / 主机名。CIDR / 范围
// 提前算准目标数。
func (t *targetIter) initOne(s string) error {
	if ip := net.ParseIP(s); ip != nil {
		t.estimate = 1
		t.step = func(emit emitFunc) (bool, error) {
			if err := t.tryEmit(Target{Addr: s}, emit); err != nil {
				return false, err
			}
			return false, nil
		}
		return nil
	}
	if strings.Contains(s, "/") {
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		count, err := cidrCount(ipnet)
		if err != nil {
			return err
		}
		t.estimate = count
		// Stateful CIDR step: cur advances by incIP on each call. /
		// 状态化 CIDR step：cur 每次调用自增。
		var cur net.IP
		t.step = func(emit emitFunc) (bool, error) {
			if cur == nil {
				ip, ipnet2, _ := net.ParseCIDR(s)
				// Normalise IPv4 to 4-byte form for clean incIP,
				// but leave IPv6 untouched. / IPv4 归一为 4 字节形
				// 式以方便 incIP，IPv6 保留原状。
				if v4 := ip.To4(); v4 != nil {
					ipnet2.IP = v4
				}
				cur = make(net.IP, len(ipnet2.IP))
				copy(cur, ip.Mask(ipnet2.Mask))
			}
			if !ipnet.Contains(cur) {
				return false, nil
			}
			if err := t.tryEmit(Target{Addr: cur.String()}, emit); err != nil {
				return false, err
			}
			incIP(cur)
			return ipnet.Contains(cur), nil
		}
		return nil
	}
	if strings.Contains(s, "-") {
		start, end, err := parseRange(s)
		if err != nil {
			return err
		}
		count := 1
		for cur := append(net.IP(nil), start...); !cur.Equal(end); incIP(cur) {
			count++
		}
		t.estimate = count
		// Stateful range step: cur advances by incIP per call. / 状态
		// 化范围 step：cur 每次调用自增。
		var cur net.IP
		t.step = func(emit emitFunc) (bool, error) {
			if cur == nil {
				cur = make(net.IP, len(start))
				copy(cur, start)
			}
			if err := t.tryEmit(Target{Addr: cur.String()}, emit); err != nil {
				return false, err
			}
			if cur.Equal(end) {
				return false, nil
			}
			incIP(cur)
			return true, nil
		}
		return nil
	}
	// Fallback: hostname. / 回退：主机名。
	t.estimate = -1
	t.step = func(emit emitFunc) (bool, error) {
		if err := t.tryEmit(Target{Addr: s}, emit); err != nil {
			return false, err
		}
		return false, nil
	}
	return nil
}

// stepFileOrSpec handles the spec+hosts-file case. Drains the spec
// first (via the same stateful comma-list mechanism as initSpec), then
// the hosts file. / stepFileOrSpec 处理 spec+hosts 文件情形。先排
// spec（用与 initSpec 相同的状态化机制），再排 hosts 文件。
func (t *targetIter) stepFileOrSpec(emit emitFunc) (bool, error) {
	// Drain spec once via the same stateful pattern. / 用同一状态
	// 模式一次性排空 spec。
	if t.spec != "" {
		pieces := strings.Split(t.spec, ",")
		t.spec = ""
		var i int
		for {
			// Advance to next non-empty piece that produces a target.
			// / 推进到下一个非空且能产出目标的 piece。
			for i < len(pieces) {
				piece := strings.TrimSpace(pieces[i])
				i++
				if piece == "" {
					continue
				}
				var emittedInPiece bool
				wrapped := func(target Target) error {
					if emittedInPiece {
						return nil
					}
					emittedInPiece = true
					return emit(target)
				}
				if err := t.emitOne(piece, wrapped); err != nil {
					return false, err
				}
				if emittedInPiece {
					return true, nil
				}
			}
			break
		}
	}
	if t.fileScanner == nil {
		return false, nil
	}
	// Drain the file one line per step call. / hosts 文件每行一个 step。
	for t.fileScanner.Scan() {
		line := strings.TrimSpace(t.fileScanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		var emitted bool
		wrapped := func(target Target) error {
			if emitted {
				return nil
			}
			emitted = true
			return emit(target)
		}
		if err := t.emitOne(line, wrapped); err != nil {
			return false, err
		}
		if !emitted {
			continue // blank line / all dedup, try next
		}
		return true, nil
	}
	if err := t.fileScanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}

// emitOne is a one-shot helper for paths where the token shape is not
// known in advance (comma-list / hosts-file). / emitOne 是用于路径
// 未知 token 形状（逗号列表 / hosts 文件）的单次辅助。
func (t *targetIter) emitOne(s string, emit emitFunc) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if ip := net.ParseIP(s); ip != nil {
		return t.tryEmit(Target{Addr: s}, emit)
	}
	if strings.Contains(s, "/") {
		ip, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur); incIP(cur) {
			if err := t.tryEmit(Target{Addr: cur.String()}, emit); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.Contains(s, "-") {
		start, end, err := parseRange(s)
		if err != nil {
			return err
		}
		for cur := start; ; incIP(cur) {
			if err := t.tryEmit(Target{Addr: cur.String()}, emit); err != nil {
				return err
			}
			if cur.Equal(end) {
				break
			}
		}
		return nil
	}
	return t.tryEmit(Target{Addr: s}, emit)
}

// tryEmit dedups against the seen set and enforces the MaxTargets cap.
// / tryEmit 在 seen 集合上去重并强制 MaxTargets 上限。
func (t *targetIter) tryEmit(target Target, emit emitFunc) error {
	k := target.Key()
	if k == "" {
		return nil
	}
	if _, dup := t.seen[k]; dup {
		return nil
	}
	// M5: cap retained as a streaming guard. / M5：上限保留为流式护栏。
	if len(t.seen) >= t.cap {
		return fmt.Errorf("too many targets: exceeded MaxTargets=%d (use a smaller CIDR or split the scan)", t.cap)
	}
	t.seen[k] = struct{}{}
	return emit(target)
}

// Next implements TargetIterator. / Next 实现 TargetIterator。
func (t *targetIter) Next() (Target, bool) {
	if t.done {
		return Target{}, false
	}
	var (
		out      Target
		emitDone bool
	)
	emit := func(target Target) error {
		out = target
		emitDone = true
		return nil
	}
	for {
		more, err := t.step(emit)
		if err != nil {
			t.lastErr = err
			t.close()
			return Target{}, false
		}
		if emitDone {
			return out, true
		}
		if !more {
			t.close()
			return Target{}, false
		}
	}
}

// close releases the hosts-file handle if open. / close 释放已打开的
// hosts 文件句柄。
func (t *targetIter) close() {
	t.done = true
	if t.fileOpen != nil {
		_ = t.fileOpen.Close()
		t.fileOpen = nil
	}
}

// Estimated implements TargetIterator. / Estimated 实现 TargetIterator。
func (t *targetIter) Estimated() int { return t.estimate }

// Err implements TargetIterator. / Err 实现 TargetIterator。
func (t *targetIter) Err() error { return t.lastErr }

// cidrCount returns the exact number of IPs in a CIDR (handles both
// v4 and v6). / cidrCount 返回 CIDR 内 IP 准确数。
func cidrCount(ipnet *net.IPNet) (int, error) {
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits > 62 {
		return 0, fmt.Errorf("CIDR %s too large: %d host bits (max 62)", ipnet.String(), hostBits)
	}
	return 1 << uint(hostBits), nil
}