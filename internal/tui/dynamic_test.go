// dynamic_test.go — T2 EVENTS dynamic-behavior tests (spec §4.1/§5.4):
// ×N render-layer merging (#6), follow/browse scrolling (#10), date
// separators (#11), column-width hysteresis (#12), the paused hidden
// gap, storm display, and the title scroll chip.
// / dynamic_test.go — T2 EVENTS 动态行为测试（spec §4.1/§5.4）：×N
// 渲染层合并（#6）、follow/browse 滚动（#10）、日期分隔行（#11）、
// 列宽滞回（#12）、暂停隐藏缺口、风暴显示、标题滚动芯片。
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// keyPress builds the tea.KeyMsg the Update switch matches on.
// / keyPress 构造 Update switch 能匹配的 tea.KeyMsg。
func keyPress(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// seedEvents feeds n same-source "hit" events through the dispatcher
// (the production ingestion path), spaced 200ms apart.
// / seedEvents 经 dispatcher（生产摄入路径）喂 n 条同源 "hit" 事件，
// 间隔 200ms。
func seedEvents(t *testing.T, d dispatcher, n int) {
	t.Helper()
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	entries := make([]eventEntry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, eventEntry{
			Host: "10.0.0.1", Port: 22, Service: "ssh",
			Kind: "hit", At: t0.Add(time.Duration(i) * 200 * time.Millisecond),
		})
	}
	d.Update(eventsBatchMsg{entries: entries, ingested: n})
}

// ── #6 Fold_RenderMerge ──

// TestMergeable pins the same-source/≤1s/same-day contract.
// / TestMergeable 钉住同源/≤1s/同日契约。
func TestMergeable(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	a := eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit", At: t0}
	cases := []struct {
		name string
		b    eventEntry
		want bool
	}{
		{"same source 500ms", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit", At: t0.Add(500 * time.Millisecond)}, true},
		{"same source exactly 1s", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit", At: t0.Add(time.Second)}, true},
		{"gap > 1s", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit", At: t0.Add(1001 * time.Millisecond)}, false},
		{"different kind", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "miss", At: t0.Add(100 * time.Millisecond)}, false},
		{"different port", eventEntry{Host: "10.0.0.1", Port: 80, Kind: "hit", At: t0.Add(100 * time.Millisecond)}, false},
		{"different host", eventEntry{Host: "10.0.0.2", Port: 22, Kind: "hit", At: t0.Add(100 * time.Millisecond)}, false},
		{"out of order", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit", At: t0.Add(-100 * time.Millisecond)}, false},
		{"cross-day within window", eventEntry{Host: "10.0.0.1", Port: 22, Kind: "hit",
			At: time.Date(2026, 1, 2, 0, 0, 0, 200000000, time.Local)}, false},
	}
	for _, c := range cases {
		if got := mergeable(a, c.b); got != c.want {
			t.Errorf("mergeable(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRunBounds verifies run expansion around a break.
// / TestRunBounds 验证跨越断点的 run 扩展。
func TestRunBounds(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	// IDs 1-3: hit run; 4-5: miss run; 6: lone hit.
	events := []eventEntry{
		{Host: "h", Port: 1, Kind: "hit", At: t0, ID: 1},
		{Host: "h", Port: 1, Kind: "hit", At: t0.Add(200 * time.Millisecond), ID: 2},
		{Host: "h", Port: 1, Kind: "hit", At: t0.Add(400 * time.Millisecond), ID: 3},
		{Host: "h", Port: 1, Kind: "miss", At: t0.Add(600 * time.Millisecond), ID: 4},
		{Host: "h", Port: 1, Kind: "miss", At: t0.Add(800 * time.Millisecond), ID: 5},
		{Host: "h", Port: 1, Kind: "hit", At: t0.Add(time.Second), ID: 6},
	}
	if f, l, s := runBounds(events, 1); f != 0 || l != 2 || s != 3 {
		t.Errorf("runBounds(hit run middle) = (%d, %d, %d), want (0, 2, 3)", f, l, s)
	}
	if f, l, s := runBounds(events, 4); f != 3 || l != 4 || s != 2 {
		t.Errorf("runBounds(miss run tail) = (%d, %d, %d), want (3, 4, 2)", f, l, s)
	}
	if f, l, s := runBounds(events, 5); f != 5 || l != 5 || s != 1 {
		t.Errorf("runBounds(lone event) = (%d, %d, %d), want (5, 5, 1)", f, l, s)
	}
}

// TestFold_RenderMerge (spec test #6): adjacent same-source events
// collapse into one ×N row in the render layer; the ring keeps the
// originals; Enter replays ≤replayMax and the merged count adjusts.
// / TestFold_RenderMerge（spec 测试 #6）：相邻同源事件在渲染层折叠
// 成一行 ×N；ring 保留原文；Enter 重放 ≤replayMax 条且 merged 计数
// 相应调整。
func TestFold_RenderMerge(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	// 3-event hit run + 2-event miss run.
	events := []eventEntry{
		{Host: "h", Port: 1, Kind: "hit", At: t0, ID: 1},
		{Host: "h", Port: 1, Kind: "hit", At: t0.Add(200 * time.Millisecond), ID: 2},
		{Host: "h", Port: 1, Kind: "hit", At: t0.Add(400 * time.Millisecond), ID: 3},
		{Host: "h", Port: 1, Kind: "miss", At: t0.Add(600 * time.Millisecond), ID: 4},
		{Host: "h", Port: 1, Kind: "miss", At: t0.Add(800 * time.Millisecond), ID: 5},
	}
	rows, merged := decorateEvents(events, 0, 0, 0)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per run)", len(rows))
	}
	if rows[0].run != 3 || rows[0].firstID != 1 || rows[0].lastID != 3 {
		t.Errorf("row0 run/first/last = %d/%d/%d, want 3/1/3", rows[0].run, rows[0].firstID, rows[0].lastID)
	}
	if rows[1].run != 2 {
		t.Errorf("row1 run = %d, want 2", rows[1].run)
	}
	if merged != 3 {
		t.Errorf("merged = %d, want 3", merged)
	}
	if rows[0].replay != nil {
		t.Errorf("collapsed run must not carry a replay")
	}

	// Enter-expand the hit run: 3 originals replayed, merged drops to 1.
	rows, merged = decorateEvents(events, 1, 0, 0)
	if rows[0].replay == nil {
		t.Fatalf("expandedRun=1 must replay the hit run")
	}
	if len(rows[0].replay) != 3 || rows[0].replay[0].ID != 1 || rows[0].replay[2].ID != 3 {
		t.Errorf("replay = %d entries (first ID %d, last %d), want IDs 1..3",
			len(rows[0].replay), rows[0].replay[0].ID, rows[0].replay[len(rows[0].replay)-1].ID)
	}
	if merged != 1 {
		t.Errorf("merged after expand = %d, want 1 (miss run only)", merged)
	}

	// replayMax: a 12-event run replays only the newest 10.
	var big []eventEntry
	for i := 0; i < 12; i++ {
		big = append(big, eventEntry{Host: "h", Port: 1, Kind: "hit",
			At: t0.Add(time.Duration(i) * 200 * time.Millisecond), ID: uint64(i + 1)})
	}
	rows, _ = decorateEvents(big, 1, 0, 0)
	if len(rows) != 1 || rows[0].run != 12 {
		t.Fatalf("big run: rows=%d run=%d, want 1 row ×12", len(rows), rows[0].run)
	}
	if len(rows[0].replay) != replayMax {
		t.Fatalf("replay len = %d, want %d", len(rows[0].replay), replayMax)
	}
	if rows[0].replay[0].ID != 3 || rows[0].replay[replayMax-1].ID != 12 {
		t.Errorf("replay window = IDs %d..%d, want 3..12",
			rows[0].replay[0].ID, rows[0].replay[replayMax-1].ID)
	}

	// Storage never folds: the ring still holds every original.
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	seedEvents(t, d, 5)
	if got := len(m.eventsOrdered()); got != 5 {
		t.Errorf("ring len after merge-eligible batch = %d, want 5 (storage never folds)", got)
	}
	rows, merged = decorateEvents(m.eventsOrdered(), 0, 0, 0)
	if len(rows) != 1 || rows[0].run != 5 || merged != 4 {
		t.Errorf("seeded run: rows=%d run=%d merged=%d, want 1/5/4", len(rows), rows[0].run, merged)
	}
}

// ── #11 DateSeparator ──

// TestDateSeparator (spec test #11): a local midnight boundary inserts
// a date separator row and breaks the merge run even when the gap is
// inside the merge window.
// / TestDateSeparator（spec 测试 #11）：本地午夜边界插入日期分隔行，
// 即便间隔在合并窗口内也打断合并 run。
func TestDateSeparator(t *testing.T) {
	day1 := time.Date(2026, 1, 1, 23, 59, 59, 800000000, time.Local)
	day2 := time.Date(2026, 1, 2, 0, 0, 0, 300000000, time.Local) // 0.5s later, next day
	events := []eventEntry{
		{Host: "h", Port: 1, Kind: "hit", At: day1, ID: 1},
		{Host: "h", Port: 1, Kind: "hit", At: day2, ID: 2},
	}
	rows, merged := decorateEvents(events, 0, 0, 0)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (event, date sep, event)", len(rows))
	}
	if rows[0].typ != rowEvent || rows[2].typ != rowEvent {
		t.Errorf("outer rows must be events, got types %d/%d", rows[0].typ, rows[2].typ)
	}
	if rows[1].typ != rowDateSep || rows[1].label != "2026-01-02" {
		t.Errorf("middle row = type %d label %q, want dateSep 2026-01-02", rows[1].typ, rows[1].label)
	}
	if merged != 0 {
		t.Errorf("merged = %d, want 0 (day boundary breaks the run)", merged)
	}
	// viewLiveEvents renders the separator dimmed with dashes.
	m := NewModel(nil)
	m.eventsBuf = append([]eventEntry{}, events...)
	m.eventsCap = len(events)
	m.eventsHead = len(events)
	m.eventsFull = false
	view := m.viewLiveEvents(10, 80)
	if !strings.Contains(view, "── 2026-01-02 ──") {
		t.Errorf("viewLiveEvents missing date separator row:\n%s", view)
	}
}

// ── #10 Scroll_FollowBrowse ──

// TestScroll_FollowBrowse (spec test #10): follow by default; ↑ enters
// browse anchored one row older; new events only bump ↓N; ↓ at the
// bottom returns to follow; g jumps to the oldest; evicted anchors
// clamp.
// / TestScroll_FollowBrowse（spec 测试 #10）：默认 follow；↑ 进入
// browse 并锚定上一行；新事件只累计 ↓N；↓ 到底回 follow；g 跳最老；
// 被淘汰的锚点钳位。
func TestScroll_FollowBrowse(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	seedEvents(t, d, 5)

	if m.browse {
		t.Fatalf("fresh model must be in follow mode")
	}

	// ↑ enters browse anchored one row older than the newest.
	d.Update(keyPress("up"))
	if !m.browse {
		t.Fatalf("scrollUp did not enter browse mode")
	}
	events := m.eventsOrdered()
	if m.browseID != events[len(events)-2].ID {
		t.Errorf("browseID = %d, want %d (one older than newest)",
			m.browseID, events[len(events)-2].ID)
	}

	// New events during browse: ring grows, viewport lag counts.
	d.Update(eventsBatchMsg{ingested: 2, entries: []eventEntry{
		{Host: "10.0.0.9", Port: 80, Service: "http", Kind: "miss",
			At: time.Date(2026, 1, 1, 10, 1, 0, 0, time.Local)},
		{Host: "10.0.0.9", Port: 81, Service: "http", Kind: "miss",
			At: time.Date(2026, 1, 1, 10, 1, 0, 0, time.Local).Add(100 * time.Millisecond)},
	}})
	if got := len(m.eventsOrdered()); got != 7 {
		t.Errorf("ring len during browse = %d, want 7 (collection never stops)", got)
	}
	if m.browseLag != 2 {
		t.Errorf("browseLag = %d, want 2", m.browseLag)
	}
	if chip := m.eventsTitle(80, 0); !strings.Contains(chip, "↓2 new") {
		t.Errorf("title chip missing ↓2 new: %q", chip)
	}
	// The browse viewport must not include the two newest rows.
	view := m.viewLiveEvents(6, 80)
	if strings.Contains(view, "10.0.0.9") {
		t.Errorf("browse viewport leaked newer events:\n%s", view)
	}

	// ↓ to the bottom re-enters follow and clears the lag. The anchor
	// starts at ID 4 of 7; page down one row at a time until follow.
	for i := 0; i < 10 && m.browse; i++ {
		d.Update(keyPress("down"))
	}
	if m.browse {
		t.Errorf("scrollDown at bottom must return to follow")
	}
	if m.browseLag != 0 || m.browseID != 0 {
		t.Errorf("follow state = (lag %d, anchor %d), want (0, 0)", m.browseLag, m.browseID)
	}

	// g jumps to the oldest; lag keeps counting in browse.
	d.Update(keyPress("g"))
	if !m.browse || m.browseID != m.eventsOrdered()[0].ID {
		t.Errorf("scrollTop: browse=%v anchor=%d, want browse anchored at oldest",
			m.browse, m.browseID)
	}
	d.Update(eventsBatchMsg{ingested: 1, entries: []eventEntry{
		{Host: "10.0.0.8", Port: 445, Service: "smb", Kind: "miss",
			At: time.Date(2026, 1, 1, 10, 2, 0, 0, time.Local)},
	}})
	if m.browseLag != 1 {
		t.Errorf("browseLag after g + 1 event = %d, want 1", m.browseLag)
	}

	// G/f returns to follow; Enter expands the ×N run at the anchor.
	d.Update(keyPress("G"))
	if m.browse {
		t.Fatalf("G must return to follow")
	}
	// Jump to the top so the anchor sits inside the ×5 same-source run.
	d.Update(keyPress("g"))
	m.toggleExpand()
	events = m.eventsOrdered()
	anchor := m.anchorIndex(events)
	first, _, size := runBounds(events, anchor)
	if size < 2 {
		t.Fatalf("expected a merge run at anchor, got size %d", size)
	}
	if m.expandedRun != events[first].ID {
		t.Errorf("expandedRun = %d, want run first ID %d", m.expandedRun, events[first].ID)
	}
	m.toggleExpand()
	if m.expandedRun != 0 {
		t.Errorf("second Enter must collapse: expandedRun = %d", m.expandedRun)
	}

	// Ring eviction clamps an evicted anchor to the oldest row.
	for i := 0; i < eventCap; i++ { // wrap the ring (5 + 64 pushes)
		m.pushEvent(eventEntry{Host: "h", Port: i, Kind: "miss", At: time.Now()})
	}
	events = m.eventsOrdered()
	m.browseID = 1 // long evicted
	if idx := m.anchorIndex(events); events[idx].ID != events[0].ID {
		t.Errorf("evicted anchor resolved to ID %d, want clamp to oldest ID %d",
			events[idx].ID, events[0].ID)
	}
}

// TestScrollPage verifies PgUp/PgDn paging and the bottom→follow flip.
// The page size comes from the region budget (eventsPage), so the
// anchor must move exactly one page per keypress.
// / TestScrollPage 验证 PgUp/PgDn 翻页及底部翻页回 follow。页宽来自
// 区域预算（eventsPage），锚点每次按键必须恰好移动一页。
func TestScrollPage(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	seedEvents(t, d, 10)
	page := m.eventsPage()
	if page < 1 {
		t.Fatalf("eventsPage = %d, want ≥1", page)
	}
	d.Update(keyPress("pgup"))
	if !m.browse {
		t.Fatalf("PgUp did not enter browse mode")
	}
	events := m.eventsOrdered()
	wantIdx := len(events) - 1 - page // page events older than the newest
	if wantIdx < 0 {
		wantIdx = 0 // clamped to the oldest
	}
	if m.browseID != events[wantIdx].ID {
		t.Errorf("PgUp anchor = %d, want %d (one page = %d older)",
			m.browseID, events[wantIdx].ID, page)
	}
	// Page down until follow (10 events; page size ≥1 → ≤10 presses).
	for i := 0; i < 12 && m.browse; i++ {
		d.Update(keyPress("pgdown"))
	}
	if m.browse {
		t.Errorf("paging down past the bottom must return to follow")
	}
}

// ── #12 ColumnHysteresis ──

// TestColumnHysteresis (spec test #12): growth is immediate, shrink
// needs hostWHold consecutive narrow frames, equal need resets the
// streak.
// / TestColumnHysteresis（spec 测试 #12）：增长立即生效；收缩需连续
// hostWHold 帧窄值；相等重置连续计数。
func TestColumnHysteresis(t *testing.T) {
	// Growth applies on the first frame.
	if w, s := stepHostWidth(evHostW, hostWMax, 0); w != hostWMax || s != 0 {
		t.Errorf("growth = (%d, %d), want (%d, 0)", w, s, hostWMax)
	}
	// Shrink holds for hostWHold-1 frames, then applies on the 20th.
	w, streak := hostWMax, 0
	frames := 0
	for w != evHostW {
		w, streak = stepHostWidth(w, evHostW, streak)
		frames++
		if frames > hostWHold+1 {
			t.Fatalf("shrink never applied after %d frames (w=%d streak=%d)", frames, w, streak)
		}
	}
	if frames != hostWHold {
		t.Errorf("shrink took %d frames, want %d", frames, hostWHold)
	}
	// Equal need resets the streak (IPv6 gone for one frame then back
	// restarts the hold window). / 相等即重置（IPv6 短暂消失又回来会
	// 重新起算）。
	if w, s := stepHostWidth(hostWMax, hostWMax, 19); w != hostWMax || s != 0 {
		t.Errorf("equal need = (%d, %d), want (%d, 0) — streak reset", w, s, hostWMax)
	}
	// hostNeed scans the ring and clamps to [evHostW, hostWMax].
	m := NewModel(nil)
	if got := m.hostNeed(); got != evHostW {
		t.Errorf("empty ring hostNeed = %d, want %d", got, evHostW)
	}
	ipv6 := "2001:db8:aaaa:bbbb:cccc:dddd:eeee:ffff"
	m.pushEvent(eventEntry{Host: ipv6, Port: 443, Kind: "hit", At: time.Now()})
	need := hostPortNeed(ipv6, 443) // 38-char host + brackets + :443 → 44
	if got := m.hostNeed(); got != need {
		t.Errorf("IPv6 hostNeed = %d, want %d", got, need)
	}
	if got := hostPortNeed("2001:db8::1", 443); got != len("[2001:db8::1]")+1+3 {
		t.Errorf("hostPortNeed(IPv6) = %d, want %d (bare address gets RFC3986 brackets)",
			got, len("[2001:db8::1]")+1+3)
	}
	// Growth via the tick beat is immediate: the long IPv6 widens the
	// column beyond the spec default on the first frame.
	// / tick 拍上的增长立即生效：长 IPv6 首帧就把列撑过 spec 默认值。
	m2 := NewModel(nil)
	m2.pushEvent(eventEntry{Host: ipv6, Port: 443, Kind: "hit", At: time.Now()})
	m2.Update(tickMsg{})
	if m2.hostW != need {
		t.Errorf("after tick hostW = %d, want immediate growth to %d", m2.hostW, need)
	}
}

// ── paused hidden gap ──

// TestPausedHiddenGap drives the real key path: p freezes the viewport
// and snapshots the frontier; batches during pause count but drop
// entries; r derives the exact hidden count and marks the gap; the
// separator renders right after the pre-pause anchor.
// / TestPausedHiddenGap 走真实按键路径：p 冻结视口并快照前沿；暂停
// 期间批次只计数不存条目；r 导出精确隐藏数并标记缺口；分隔行渲染
// 在暂停前锚事件之后。
func TestPausedHiddenGap(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	seedEvents(t, d, 3) // IDs 1..3, nextEventID = 3

	d.Update(keyPress("p"))
	if m.uiMode != modePaused {
		t.Fatalf("p did not pause: uiMode = %v", m.uiMode)
	}
	if m.pauseAnchorID != 3 || m.pauseIngest0 != 3 {
		t.Errorf("pause snapshot = (anchor %d, ingest0 %d), want (3, 3)",
			m.pauseAnchorID, m.pauseIngest0)
	}
	if m.frozenView == "" {
		t.Errorf("pause must snapshot the viewport frame")
	}

	// Two batches land while paused: 5 ingested + 1 dropped, no ring
	// growth, storm mirror tracks the newest snapshot (each batch
	// carries the hub's current storm state).
	// / 暂停期间两批：5 in + 1 dropped，ring 不增长，风暴镜像跟随最新
	// 快照（每批携带 hub 当前风暴态）。
	d.Update(eventsBatchMsg{ingested: 3, dropped: 1, storm: true, stormRate: 480,
		entries: []eventEntry{{Host: "10.9.9.9", Port: 1, Kind: "miss", At: time.Now()}}})
	d.Update(eventsBatchMsg{ingested: 2, storm: true, stormRate: 480,
		entries: []eventEntry{{Host: "10.9.9.9", Port: 2, Kind: "miss", At: time.Now()}}})
	if m.ingested != 8 || m.dropped != 1 {
		t.Errorf("paused counters = (%d, %d), want (8, 1)", m.ingested, m.dropped)
	}
	if got := len(m.eventsOrdered()); got != 3 {
		t.Errorf("paused ring len = %d, want 3", got)
	}
	if !m.storm || m.stormRate != 480 {
		t.Errorf("paused storm mirror = (%v, %d), want (true, 480)", m.storm, m.stormRate)
	}

	d.Update(keyPress("r"))
	if m.uiMode != modeRun {
		t.Fatalf("r did not resume: uiMode = %v", m.uiMode)
	}
	if m.frozenView != "" {
		t.Errorf("resume must clear the frozen frame")
	}
	if m.gapAfterID != 3 || m.gapCount != 5 {
		t.Errorf("gap = (after %d, count %d), want (3, 5)", m.gapAfterID, m.gapCount)
	}

	// The separator renders right after the anchor event row. The 3
	// seeded same-source events merge into one ×3 row (render layer),
	// whose lastID == the pause anchor → the gap lands directly below.
	// / 分隔行渲染在锚事件行之后。3 条同源种子事件在渲染层合并为一行
	// ×3（lastID == 暂停锚）→ 缺口行紧贴其下。
	rows, _ := decorateEvents(m.eventsOrdered(), 0, m.gapAfterID, m.gapCount)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (×3 merged event + gap)", len(rows))
	}
	if rows[0].typ != rowEvent || rows[0].run != 3 || rows[0].lastID != 3 {
		t.Errorf("row0 = type %d run %d lastID %d, want event ×3 lastID 3",
			rows[0].typ, rows[0].run, rows[0].lastID)
	}
	if rows[1].typ != rowGapSep || !strings.Contains(rows[1].label, "5 hidden while paused") {
		t.Errorf("gap row = type %d label %q, want '··· 5 hidden while paused ···'",
			rows[1].typ, rows[1].label)
	}

	// Self-cleaning: once the anchor leaves the ring the gap goes too.
	for i := 0; i < eventCap; i++ { // wrap the ring → IDs 1..3 evicted
		m.pushEvent(eventEntry{Host: "h", Port: i, Kind: "miss", At: time.Now()})
	}
	rows, _ = decorateEvents(m.eventsOrdered(), 0, m.gapAfterID, m.gapCount)
	for _, r := range rows {
		if r.typ == rowGapSep {
			t.Errorf("evicted anchor must not render a gap separator")
		}
	}
}

// ── storm display ──

// TestStormDisplay verifies the storm view: summary line (rate ·
// hits% · critical only), the sidecar-evicted count, and sidecar rows;
// plus viewLiveEvents delegation while storm is active.
// / TestStormDisplay 验证风暴视图：摘要行（速率 · 命中率 ·
// critical only）、侧车淘汰计数、侧车行，以及风暴激活时
// viewLiveEvents 的委托。
func TestStormDisplay(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	// 10 hits + 2 misses + 11 credential successes (8 survive the
	// sidecar, 3 evicted). / 10 hit + 2 miss + 11 cred_success（侧车
	// 存活 8，淘汰 3）。
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	var entries []eventEntry
	for i := 0; i < 10; i++ {
		entries = append(entries, eventEntry{Host: "10.0.0.1", Port: 22,
			Service: "ssh", Kind: "hit", At: t0.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	for i := 0; i < 2; i++ {
		entries = append(entries, eventEntry{Host: "10.0.0.2", Port: 80,
			Service: "http", Kind: "miss", At: t0.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	for i := 0; i < 11; i++ {
		entries = append(entries, eventEntry{Host: "10.0.0.3", Port: 445,
			Service: "smb", Kind: "cred_success", Text: "admin:admin",
			At: t0.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	d.Update(eventsBatchMsg{entries: entries, ingested: len(entries)})
	m.storm = true
	m.stormRate = 620

	// Ring window: 21 hits / 23 total → 91%.
	view := m.viewLiveEvents(14, 80) // storm=true delegates
	if !strings.Contains(view, "▲ 620 ev/s") {
		t.Errorf("storm summary missing rate:\n%s", view)
	}
	if !strings.Contains(view, "91% hits") {
		t.Errorf("storm summary missing hit pct (21/23 → 91%%):\n%s", view)
	}
	if !strings.Contains(view, "critical only") {
		t.Errorf("storm summary missing 'critical only':\n%s", view)
	}
	if !strings.Contains(view, "+ 3 more criticals") {
		t.Errorf("storm view missing evicted-critical count:\n%s", view)
	}
	if !strings.Contains(view, "10.0.0.3:445") {
		t.Errorf("storm view missing sidecar rows:\n%s", view)
	}

	// Height 2 → title + summary only, no sidecar rows.
	short := m.viewLiveEvents(2, 80)
	if strings.Contains(short, "10.0.0.3") {
		t.Errorf("height-2 storm view must not render sidecar rows:\n%s", short)
	}
}

// TestEventsTitle_Chip pins the scroll-state chip: follow ▼ by
// default, browse ▲ with ↓N new, and the 999+ lag cap.
// / TestEventsTitle_Chip 钉住滚动状态芯片：默认 follow ▼，browse ▲
// 带 ↓N new，以及 999+ 上限。
func TestEventsTitle_Chip(t *testing.T) {
	m := NewModel(nil)
	m.ingested = 100
	if chip := m.eventsTitle(80, 0); !strings.Contains(chip, "follow ▼") {
		t.Errorf("follow chip missing: %q", chip)
	}
	m.browse = true
	m.browseLag = 7
	if chip := m.eventsTitle(80, 0); !strings.Contains(chip, "browse ▲") ||
		!strings.Contains(chip, "↓7 new") {
		t.Errorf("browse chip wrong: %q", chip)
	}
	m.browseLag = 1500
	if chip := m.eventsTitle(80, 0); !strings.Contains(chip, "↓999+ new") {
		t.Errorf("lag cap missing: %q", chip)
	}
	// Hub counters surface on the left (spec §7.2).
	m.browse = false
	m.dropped = 4
	m.nextEventID = 0 // silence lint on unused field write path
	chip := m.eventsTitle(80, 2)
	for _, want := range []string{"100 in", "4 dropped", "2 merged"} {
		if !strings.Contains(chip, want) {
			t.Errorf("title missing %q: %q", want, chip)
		}
	}
}
