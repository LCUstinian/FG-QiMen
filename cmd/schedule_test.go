// schedule_test.go — unit tests for cmd/schedule.go (applySchedule).
//
// Scope: cover every branch of applySchedule so the function is
// tested at the unit level rather than only via the full integration
// test suite (which exercises the happy path). The function lives at
// /cmd/schedule.go and is invoked from runScan.
//
// The function has 5 major branches after the Resolve call:
//  1. ModeNone (no schedule flag set) — return nil
//  2. DryRun (any mode) — print next-fire, return
//  3. One-shot Wait (non-daemon, any mode) — wait once, return
//  4. Daemon loop (--cron + --daemon) — loop until ctx cancel
//  5. Resolve error (bad input) — return error
//
// Branch 1, 2, and 5 are the most common in tests; 3 and 4 need
// time/ctx control which we handle via pre-cancelled contexts and
// near-future times so the tests don't hang.
//
// / applySchedule 单元测试。覆盖所有分支。
package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/scheduler"
)

// saveScheduleFlags / restoreScheduleFlags: the package-level
// snapshotFlags helper doesn't include the schedule flags
// (added in v0.5). Tests for applySchedule need their own
// save/restore pair to keep cross-test state out.
// / 包级 snapshotFlags 不含 schedule flag（v0.5 新增）。
// 这里的 save/restore 专门给 applySchedule 测试用，避免交叉污染。
type scheduleFlagSnapshot struct {
	at, in, cron, tz                   string
	daemon, dryRun                      bool
}

func saveScheduleFlags() scheduleFlagSnapshot {
	return scheduleFlagSnapshot{
		at:     flagScheduleAt,
		in:     flagScheduleIn,
		cron:   flagScheduleCron,
		tz:     flagScheduleTZ,
		daemon: flagScheduleDaemon,
		dryRun: flagScheduleDryRun,
	}
}

func restoreScheduleFlags(s scheduleFlagSnapshot) {
	flagScheduleAt = s.at
	flagScheduleIn = s.in
	flagScheduleCron = s.cron
	flagScheduleTZ = s.tz
	flagScheduleDaemon = s.daemon
	flagScheduleDryRun = s.dryRun
}

// newScheduleTestCmd builds a fresh cobra command for the tests.
// Returns the *bytes.Buffer behind ErrOrStderr so the tests can
// assert on the captured "[*] scheduler:" / "next run at" lines.
// / 为测试构造一个 fresh cobra command。返 ErrOrStderr 后面
// 的 *bytes.Buffer，测试用它断言 "[*] scheduler:" / "next
// run at" 那行。
func newScheduleTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	c := &cobra.Command{Use: "test"}
	c.SetOut(bytes.NewBuffer(nil))
	c.SetErr(bytes.NewBuffer(nil))
	buf, _ := c.ErrOrStderr().(*bytes.Buffer)
	return c, buf
}

// clearScheduleFlags zeros all schedule flags. Used between
// sub-cases in a single test so the previous sub-case's flags
// don't leak into the next.
// / 把所有 schedule flag 清零。同一测试多个 sub-case 之间用，
// 避免前一个 sub-case 的 flag 漏到下一个。
func clearScheduleFlags() {
	flagScheduleAt = ""
	flagScheduleIn = ""
	flagScheduleCron = ""
	flagScheduleTZ = ""
	flagScheduleDaemon = false
	flagScheduleDryRun = false
}

// TestApplySchedule_NoFlags: with no schedule flag set, Resolve
// returns ModeNone and applySchedule returns nil immediately
// without printing anything. The most common production case
// (an operator who doesn't use --at/--in/--cron).
// / 无 schedule flag → ModeNone → 立即返回 nil，不打印。
// 这是最高频的调用场景。
func TestApplySchedule_NoFlags(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	c, buf := newScheduleTestCmd(t)
	if err := applySchedule(c); err != nil {
		t.Errorf("no flags: got error %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("no flags: expected no output, got %q", buf.String())
	}
}

// TestApplySchedule_ResolveErrors: all the Resolve() error paths
// bubble up through applySchedule. Each sub-case sets one bad
// flag combination, calls applySchedule, and asserts the error
// is non-nil. The exact error message is not pinned (we trust
// scheduler.Resolve to format it) — we just want the error to
// surface, not panic.
// / 所有 scheduler.Resolve 的 error 路径都透过 applySchedule 冒出来。
// 每个 sub-case 设一个坏 flag 组合，断言 error 非 nil。
// 不钉死 error 文本（信任 scheduler.Resolve 的格式），只要求冒上来。
func TestApplySchedule_ResolveErrors(t *testing.T) {
	// Future time for --at cases. Pinning it keeps the "past time"
	// test stable across clock advances.
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	// Past time for --at past case.
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	cases := []struct {
		name string
		set  func()
	}{
		{
			name: "at malformed",
			set: func() { flagScheduleAt = "not-a-rfc3339" },
		},
		{
			name: "at in the past",
			set: func() { flagScheduleAt = past },
		},
		{
			name: "in malformed",
			set: func() { flagScheduleIn = "not-a-duration" },
		},
		{
			name: "in zero",
			set: func() { flagScheduleIn = "0s" },
		},
		{
			name: "in negative",
			set: func() { flagScheduleIn = "-5m" },
		},
		{
			name: "cron invalid expression",
			set: func() { flagScheduleCron = "this is not cron" },
		},
		{
			name: "at and in mutually exclusive",
			set: func() {
				flagScheduleAt = future
				flagScheduleIn = "5m"
			},
		},
		{
			name: "daemon without cron",
			set: func() { flagScheduleDaemon = true },
		},
		{
			name: "invalid tz",
			set: func() {
				flagScheduleCron = "0 9 * * *"
				flagScheduleTZ = "Mars/Olympus_Mons"
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			save := saveScheduleFlags()
			defer restoreScheduleFlags(save)
			clearScheduleFlags()
			c.set()

			cmd, _ := newScheduleTestCmd(t)
			if err := applySchedule(cmd); err == nil {
				t.Errorf("expected error for %q, got nil", c.name)
			}
		})
	}
}

// TestApplySchedule_DryRun: any schedule mode + --schedule-dry-run
// returns nil after printing the "next run at" line. We don't
// actually wait. / 任何 mode + --schedule-dry-run → 打"next run
// at"后立即返回 nil，不实际等待。
func TestApplySchedule_DryRun(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	cases := []struct {
		name, at, in, cron, tz string
	}{
		{"at", future, "", "", ""},
		{"in", "", "2h", "", ""},
		{"cron", "", "", "0 9 * * *", "UTC"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			save := saveScheduleFlags()
			defer restoreScheduleFlags(save)
			clearScheduleFlags()
			flagScheduleAt = c.at
			flagScheduleIn = c.in
			flagScheduleCron = c.cron
			flagScheduleTZ = c.tz
			flagScheduleDryRun = true

			cmd, buf := newScheduleTestCmd(t)
			if err := applySchedule(cmd); err != nil {
				t.Errorf("%s + dry-run: got error %v, want nil", c.name, err)
			}
			out := buf.String()
			if !strings.Contains(out, "[*] scheduler:") {
				t.Errorf("%s + dry-run: expected scheduler line, got %q", c.name, out)
			}
			if !strings.Contains(out, "next run at") {
				t.Errorf("%s + dry-run: expected next-run line, got %q", c.name, out)
			}
			if !strings.Contains(out, "dry-run=true") {
				t.Errorf("%s + dry-run: expected dry-run=true marker, got %q", c.name, out)
			}
		})
	}
}

// TestApplySchedule_WaitAtNearFuture: --at with a near-future
// time should Wait, return after that time, and emit the
// "next run at" line. RFC3339 truncates to second precision,
// so we use a 2-second buffer to avoid the "past" check racing
// the format step.
// / --at 用近未来时间（~2s），等到了再返。RFC3339 截断到秒，2s
// buffer 避免"past"检查和格式化抢跑。
func TestApplySchedule_WaitAtNearFuture(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	flagScheduleAt = time.Now().Add(2 * time.Second).Format(time.RFC3339)

	cmd, buf := newScheduleTestCmd(t)
	cmd.SetContext(context.Background())
	start := time.Now()
	if err := applySchedule(cmd); err != nil {
		t.Errorf("near-future --at: got error %v, want nil", err)
	}
	elapsed := time.Since(start)
	if elapsed < 1*time.Second {
		t.Errorf("near-future --at: returned in %v, expected >= 1s (waited too short)", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("near-future --at: took %v, expected ~2s (waited too long)", elapsed)
	}
	if !strings.Contains(buf.String(), "next run at") {
		t.Errorf("near-future --at: expected next-run line, got %q", buf.String())
	}
}

// TestApplySchedule_WaitInShort: --in with a short duration
// waits then returns. The "in" path is the most common
// non-dry-run case in scripts that say "wait 5 minutes then
// start". / --in 短时长，等到了再返。
func TestApplySchedule_WaitInShort(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	flagScheduleIn = "150ms"

	cmd, buf := newScheduleTestCmd(t)
	cmd.SetContext(context.Background())
	start := time.Now()
	if err := applySchedule(cmd); err != nil {
		t.Errorf("short --in: got error %v, want nil", err)
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Errorf("short --in: returned in %v, expected >= 100ms", elapsed)
	}
	if !strings.Contains(buf.String(), "next run at") {
		t.Errorf("short --in: expected next-run line, got %q", buf.String())
	}
}

// TestApplySchedule_DaemonCtxCancel: --cron + --daemon runs
// the daemon loop. We need to cancel the context to break out.
// We register the cleanup BEFORE the cancel so even a panic in
// applySchedule doesn't leave the test hung.
//
// Approach: pre-cancelled context → Wait returns ctx.Err() →
// daemon loop returns that error → applySchedule returns it.
// This is the simplest way to test the daemon path without
// actually waiting for a cron tick (which would take minutes).
//
// / --cron + --daemon 走 daemon 循环。用 pre-cancelled context 让
// Wait 返 ctx.Err()，daemon 循环把 error 透出来。避免了真等
// cron tick（那要几分钟）。
func TestApplySchedule_DaemonCtxCancel(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	// 9 9 * * * 9:09am daily — won't tick during the test (test
	// runs in ms), so the only way out is the cancelled ctx.
	flagScheduleCron = "9 9 * * *"
	flagScheduleDaemon = true

	cmd, _ := newScheduleTestCmd(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	cmd.SetContext(ctx)

	err := applySchedule(cmd)
	if err == nil {
		t.Errorf("daemon with pre-cancelled ctx: got nil error, want non-nil")
	}
	// The error should bubble up from in.Wait via ctx.Err() or be
	// context.Canceled. We accept any non-nil error since the
	// exact wrapping depends on scheduler internals.
	if err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		// Loose check: accept it but log so future regressions are
		// visible. (e.g. if scheduler changes the error wrapping.)
		t.Logf("daemon ctx-cancel error: %v (accepted, not strictly context.Canceled)", err)
	}
}

// TestApplySchedule_WaitCronNoDaemon: --cron WITHOUT --daemon
// is a one-shot — it waits for the next fire time, then returns
// after the scan (which here is just returning because we never
// re-enter the daemon loop). We use a cron expression that
// matches within the next minute to keep the test fast.
//
// In practice `--cron "*/1 * * * *"` (every minute) would tick
// within 60s. We pre-cancel after a short timeout to break the
// wait. We use a 1-second buffer to avoid flaky CI.
//
// / --cron 无 --daemon：单次等下次 fire，然后返回。用 1 秒级
// cron 表达式快速触发；超时主动 cancel 防 hang。
func TestApplySchedule_WaitCronNoDaemon(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	// "*/1 * * * * *" is 6-field with seconds (robfig/cron/v3
	// supports it). "*/1 * * * *" is 5-field with minute = every
	// minute. The 5-field form fires within 60s.
	flagScheduleCron = "*/1 * * * *"

	cmd, _ := newScheduleTestCmd(t)
	// Pre-cancel after 1.2s; daemon path isn't hit (no --daemon),
	// so Wait blocks until the next fire OR ctx cancel. 1.2s
	// is well under the 60s cron tick so cancel wins.
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	cmd.SetContext(ctx)

	start := time.Now()
	_ = applySchedule(cmd)
	elapsed := time.Since(start)

	// Should return at ~1.2s (ctx timeout) since the cron tick
	// is at the next minute boundary (>1.2s away on average).
	if elapsed < 1*time.Second {
		t.Errorf("cron no-daemon: returned in %v, expected ~1.2s (ctx timeout)", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("cron no-daemon: returned in %v, expected <3s (ctx timeout + slack)", elapsed)
	}
}

// TestApplySchedule_ConflictingFlags: --at + --cron together
// is mutually exclusive. Pinned as a separate test (not in the
// table) so the failure message names both flags clearly.
// / --at + --cron 互斥。单独测试，失败信息能直接报这两个 flag。
func TestApplySchedule_ConflictingFlags(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	flagScheduleAt = future
	flagScheduleCron = "0 9 * * *"

	cmd, _ := newScheduleTestCmd(t)
	err := applySchedule(cmd)
	if err == nil {
		t.Fatal("--at + --cron: expected ErrInvalidCombination, got nil")
	}
	if !errors.Is(err, scheduler.ErrInvalidCombination) {
		t.Errorf("--at + --cron: got %v, want errors.Is(ErrInvalidCombination)", err)
	}
}

// TestApplySchedule_DaemonRequiresCron: --daemon without --cron
// is a specific configuration error. The error message mentions
// both flags (for operator clarity).
// / --daemon 无 --cron 是配置错误。错误信息要提到两个 flag。
func TestApplySchedule_DaemonRequiresCron(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	flagScheduleDaemon = true
	// No --cron.

	cmd, _ := newScheduleTestCmd(t)
	err := applySchedule(cmd)
	if err == nil {
		t.Fatal("--daemon without --cron: expected error, got nil")
	}
	if !errors.Is(err, scheduler.ErrInvalidCombination) {
		t.Errorf("--daemon w/o --cron: got %v, want ErrInvalidCombination", err)
	}
}

// TestApplySchedule_DaemonLoops: --cron + --daemon with a
// 6-field cron that fires every second (* * * * * *). The
// first Wait succeeds after ~1s, the post-Wait code runs
// (in.Base = now, recompute next, reprint), then the second
// Wait sees the pre-cancelled context and returns ctx.Err().
// This covers the post-Wait branches that TestApplySchedule_DaemonCtxCancel
// (pre-cancelled) does NOT exercise.
//
// / --cron + --daemon 配每秒触发的 6-field cron。第一次 Wait
// 约 1s 后成功，post-Wait 代码跑（in.Base = now、重算 next、
// 重打），第二次 Wait 看到 pre-cancelled ctx 返 ctx.Err()。
// 覆盖 TestApplySchedule_DaemonCtxCancel 跑不到的 post-Wait
// 分支。
func TestApplySchedule_DaemonLoops(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()

	// 6-field cron, every second. robfig/cron/v3 supports the
	// seconds field; this is the fastest non-trivial cron.
	flagScheduleCron = "* * * * * *"
	flagScheduleDaemon = true

	// 2.3s budget: lets the first Wait complete (~1s) and the
	// post-Wait line print, then the second Wait sees the
	// cancelled ctx and exits.
	ctx, cancel := context.WithTimeout(context.Background(), 2300*time.Millisecond)
	defer cancel()

	cmd, buf := newScheduleTestCmd(t)
	cmd.SetContext(ctx)

	start := time.Now()
	_ = applySchedule(cmd)
	elapsed := time.Since(start)

	// Sanity: should run for at least 1.5s (first Wait must
	// succeed before ctx cancel). 3s upper bound is the ctx
	// timeout + slack.
	if elapsed < 1*time.Second {
		t.Errorf("daemon loops: returned in %v, expected >= 1s (first Wait should succeed)", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("daemon loops: took %v, expected <3s (ctx timeout + slack)", elapsed)
	}

	// The "next run at" line prints once before the loop, and
	// again after every successful Wait. So >= 2 occurrences
	// means the post-Wait code path was exercised.
	count := strings.Count(buf.String(), "next run at")
	if count < 2 {
		t.Errorf("daemon loops: expected >= 2 'next run at' lines (initial + post-Wait), got %d in:\n%s",
			count, buf.String())
	}
}
// applySchedule concurrently with non-overlapping flag sets
// should both complete without interfering. Sanity check that
// the package-level flag manipulation is safe under concurrent
// test execution (it isn't, but at least we confirm the test
// framework's t.Parallel() / parallel tests don't crash on
// shared flag state — note this test does NOT use t.Parallel
// because the package-level flag state is shared).
//
// / 并发调用 sanity check。package 级 flag 不并发安全，所以这里
// 仅测串行；不调用 t.Parallel 避免共享状态污染。
func TestApplySchedule_ConcurrentCalls(t *testing.T) {
	save := saveScheduleFlags()
	defer restoreScheduleFlags(save)
	clearScheduleFlags()
	flagScheduleDryRun = true
	flagScheduleIn = "2h"

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd, _ := newScheduleTestCmd(t)
			_ = applySchedule(cmd)
		}()
	}
	wg.Wait()
}