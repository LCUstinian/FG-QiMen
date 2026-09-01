// scheduler_test.go — unit tests for the cron parser + wait
// logic. / scheduler_test.go — cron 解析器 + 等待逻辑的单元
// 测试。
package scheduler

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// utc returns a UTC location for tests. / utc 返测试用 UTC
// 时区。
func utc() *time.Location { return time.UTC }

func TestParseCron_Valid(t *testing.T) {
	// 9am UTC daily. / 每天 UTC 9 点。
	spec, err := ParseCron("0 9 * * *", utc())
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	from := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	next := spec.Next(from)
	if next.Hour() != 9 || next.Day() != 1 {
		t.Errorf("Next = %v, want 2026-01-01 09:00", next)
	}
}

func TestParseCron_Descriptor(t *testing.T) {
	// @hourly / @daily are robfig's descriptor syntax.
	// / @hourly / @daily 是 robfig 的 descriptor 语法。
	for _, expr := range []string{"@hourly", "@daily", "@midnight"} {
		spec, err := ParseCron(expr, utc())
		if err != nil {
			t.Errorf("ParseCron(%q): %v", expr, err)
		}
		_ = spec
	}
}

func TestParseCron_Invalid(t *testing.T) {
	cases := []string{
		"",                  // empty
		"* * * *",          // only 4 fields
		"* * * * * *",      // 6 fields without Second option
		"60 0 * * *",        // minute out of range
		"0 24 * * *",        // hour out of range
		"0 0 32 * *",        // day out of range
		"0 0 0 * *",         // day 0
		"0 0 * 13 *",        // month 13
		"0 0 * 0 *",         // month 0
		"abc 0 * * *",       // not a number
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, err := ParseCron(c, utc()); err == nil {
				t.Errorf("ParseCron(%q) succeeded, want error", c)
			}
		})
	}
}

func TestResolve_Exclusive(t *testing.T) {
	_, err := Resolve(Options{At: "2026-12-25T09:00:00Z", In: "2h"})
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("err = %v, want ErrInvalidCombination", err)
	}
}

func TestResolve_DaemonRequiresCron(t *testing.T) {
	_, err := Resolve(Options{Daemon: true, In: "2h"})
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("err = %v, want ErrInvalidCombination", err)
	}
}

func TestResolve_AtPastFails(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, err := Resolve(Options{At: "2025-12-25T09:00:00Z", Now: now})
	if err == nil {
		t.Error("expected error for past --at, got nil")
	}
}

func TestResolve_AtFutureOK(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in, err := Resolve(Options{At: "2026-12-25T09:00:00Z", Now: now})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if in.Mode != ModeAt {
		t.Errorf("Mode = %v, want ModeAt", in.Mode)
	}
	if !in.At.Equal(time.Date(2026, 12, 25, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("At = %v, want 2026-12-25T09:00:00Z", in.At)
	}
}

func TestResolve_TimeZone(t *testing.T) {
	in, err := Resolve(Options{At: "2026-12-25T09:00:00+08:00"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	_, off := in.At.Zone()
	if off != 8*3600 {
		t.Errorf("At zone offset = %d, want 28800", off)
	}
}

func TestResolve_TZBad(t *testing.T) {
	_, err := Resolve(Options{TZ: "Mars/Olympus_Mons"})
	if err == nil {
		t.Error("expected error for bad --tz")
	}
}

func TestNextFire_ModeIn(t *testing.T) {
	// ModeIn is a relative delay — NextFire is now + In, in
	// the input's time zone (which is what time.Now() returned
	// in, so it just equals now + In).
	// / ModeIn 是相对延迟——NextFire = now + In。
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in, err := Resolve(Options{In: "2h30m", Now: now, TZ: "UTC"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	next := in.NextFire()
	want := now.Add(2*time.Hour + 30*time.Minute)
	if !next.Equal(want) {
		t.Errorf("NextFire = %v, want %v", next, want)
	}
}

func TestNextFire_ModeCron(t *testing.T) {
	// 9am daily. Now is 2026-01-01 06:00 UTC, next fire is
	// 2026-01-01 09:00 UTC. / 每天 9 点。Now 是 06:00，下一次
	// fire 是 09:00。
	now := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	in, err := Resolve(Options{Cron: "0 9 * * *", Now: now, TZ: "UTC"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	next := in.NextFire()
	if next.Hour() != 9 || next.Day() != 1 || next.Month() != 1 {
		t.Errorf("NextFire = %v, want 2026-01-01 09:00", next)
	}
}

func TestWait_NoOp(t *testing.T) {
	in := &Input{Mode: ModeNone}
	if err := in.Wait(context.Background()); err != nil {
		t.Errorf("Wait on ModeNone = %v, want nil", err)
	}
}

func TestWait_ReachesTarget(t *testing.T) {
	now := time.Now()
	in, err := Resolve(Options{In: "200ms", Now: now})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	in.Output = &bytes.Buffer{}
	if err := in.Wait(context.Background()); err != nil {
		t.Errorf("Wait = %v, want nil", err)
	}
	if !strings.Contains(in.Output.(*bytes.Buffer).String(), "next run at") {
		t.Error("expected countdown output")
	}
}

func TestWait_CtxCancel(t *testing.T) {
	now := time.Now()
	in, err := Resolve(Options{In: "1h", Now: now})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	in.Output = &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := in.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait on cancel = %v, want context.Canceled", err)
	}
}

func TestIsCronDaemon(t *testing.T) {
	spec, _ := ParseCron("0 9 * * *", utc())
	if (&Input{Mode: ModeCron, Daemon: true, Cron: spec}).IsCronDaemon() != true {
		t.Error("cron+daemon should be IsCronDaemon")
	}
	if (&Input{Mode: ModeCron, Cron: spec}).IsCronDaemon() != false {
		t.Error("cron without daemon should not be IsCronDaemon")
	}
	if (&Input{Mode: ModeIn}).IsCronDaemon() != false {
		t.Error("in should not be IsCronDaemon")
	}
}

func TestModeString(t *testing.T) {
	cases := []struct {
		m    Mode
		want string
	}{
		{ModeAt, "at"},
		{ModeIn, "in"},
		{ModeCron, "cron"},
		{ModeNone, "none"},
		{Mode(99), "none"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q, want %q", int(c.m), got, c.want)
		}
	}
}

func TestDefaultOutput(t *testing.T) {
	if defaultOutput() == nil {
		t.Error("defaultOutput() returned nil")
	}
}
