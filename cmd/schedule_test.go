// schedule_test.go — round-trip tests for the schedules
// subcommand (add / list / remove). / schedule_test.go —
// schedules 子命令（add / list / remove）往返测试。
package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestDetectScheduleMode(t *testing.T) {
	// Save + restore. / 保存 + 恢复。
	saveAt, saveIn, saveCron := flagScheduleAt, flagScheduleIn, flagScheduleCron
	defer func() {
		flagScheduleAt, flagScheduleIn, flagScheduleCron = saveAt, saveIn, saveCron
	}()

	cases := []struct {
		flag, val string
		wantMode  string
		wantVal   string
	}{
		{"at", "2026-12-25T09:00:00Z", "at", "2026-12-25T09:00:00Z"},
		{"in", "2h30m", "in", "2h30m"},
		{"cron", "0 9 * * *", "cron", "0 9 * * *"},
	}
	for _, c := range cases {
		flagScheduleAt, flagScheduleIn, flagScheduleCron = "", "", ""
		switch c.flag {
		case "at":
			flagScheduleAt = c.val
		case "in":
			flagScheduleIn = c.val
		case "cron":
			flagScheduleCron = c.val
		}
		mode, val := detectScheduleMode()
		if mode != c.wantMode || val != c.wantVal {
			t.Errorf("%s=%q: got (%q, %q), want (%q, %q)",
				c.flag, c.val, mode, val, c.wantMode, c.wantVal)
		}
	}

	// All empty. / 全空。
	flagScheduleAt, flagScheduleIn, flagScheduleCron = "", "", ""
	if mode, val := detectScheduleMode(); mode != "" || val != "" {
		t.Errorf("all empty: got (%q, %q), want (\"\", \"\")", mode, val)
	}
}

func TestLoadScheduleTZ(t *testing.T) {
	saveTZ := flagScheduleTZ
	defer func() { flagScheduleTZ = saveTZ }()

	flagScheduleTZ = ""
	loc := loadScheduleTZ()
	if loc != time.Local {
		t.Errorf("empty TZ: got %v, want time.Local (%v)", loc, time.Local)
	}

	flagScheduleTZ = "UTC"
	if loadScheduleTZ().String() != "UTC" {
		t.Errorf("UTC load: got %v", loadScheduleTZ())
	}

	flagScheduleTZ = "Mars/Olympus_Mons"
	// Invalid IANA names fall back to default (time.Local). /
	// 无效 IANA 名回退到默认（time.Local）。
	if loadScheduleTZ() != time.Local {
		t.Error("invalid TZ should fall back to time.Local")
	}
}

func TestSchedulesAddListRemove_RoundTrip(t *testing.T) {
	// Save + restore flag state. / 保存 + 恢复 flag 状态。
	saveProject, saveAt, saveIn, saveCron, saveTZ, saveDaemon :=
		flagProject, flagScheduleAt, flagScheduleIn, flagScheduleCron, flagScheduleTZ, flagScheduleDaemon
	defer func() {
		flagProject, flagScheduleAt, flagScheduleIn, flagScheduleCron = saveProject, saveAt, saveIn, saveCron
		flagScheduleTZ, flagScheduleDaemon = saveTZ, saveDaemon
	}()

	tmp := t.TempDir()
	t.Chdir(tmp)
	// Create the project. / 创建项目。
	if err := runProjectsCreate(newCmdForTest(t, runProjectsCreate), []string{"sched-test"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	flagProject = "sched-test"

	// Add a cron schedule. / 加 cron 调度。
	flagScheduleAt, flagScheduleIn = "", ""
	flagScheduleCron = "0 9 * * *"
	flagScheduleTZ = "UTC"
	flagScheduleDaemon = true
	if err := runSchedulesAdd(newCmdForTest(t, runSchedulesAdd), []string{"morning"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// List should show 1 row. / list 应显示 1 行。
	listCmd, buf := newCmdForTestCapture(t, runSchedulesList)
	if err := runSchedulesList(listCmd, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "morning") {
		t.Errorf("list output missing 'morning': %s", out)
	}
	if !strings.Contains(out, "cron") {
		t.Errorf("list output missing mode 'cron': %s", out)
	}
	if !strings.Contains(out, "0 9 * * *") {
		t.Errorf("list output missing cron value: %s", out)
	}

	// Add an --in schedule. / 加 --in 调度。
	flagScheduleCron, flagScheduleTZ, flagScheduleDaemon = "", "", false
	flagScheduleIn = "30m"
	if err := runSchedulesAdd(newCmdForTest(t, runSchedulesAdd), []string{"half-hour"}); err != nil {
		t.Fatalf("add --in: %v", err)
	}

	// List should show 2. / list 应显示 2。
	listCmd, buf = newCmdForTestCapture(t, runSchedulesList)
	if err := runSchedulesList(listCmd, nil); err != nil {
		t.Fatalf("list 2: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "morning") || !strings.Contains(out, "half-hour") {
		t.Errorf("list should have both schedules: %s", out)
	}

	// Remove one. / 删一个。
	if err := runSchedulesRemove(newCmdForTest(t, runSchedulesRemove), []string{"morning"}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// List should show 1. / list 应显示 1。
	listCmd, buf = newCmdForTestCapture(t, runSchedulesList)
	if err := runSchedulesList(listCmd, nil); err != nil {
		t.Fatalf("list 3: %v", err)
	}
	out = buf.String()
	if strings.Contains(out, "morning") {
		t.Errorf("morning should be gone: %s", out)
	}
	if !strings.Contains(out, "half-hour") {
		t.Errorf("half-hour should remain: %s", out)
	}

	// Remove idempotent. / remove 幂等。
	rmCmd, rmBuf := newCmdForTestCapture(t, runSchedulesRemove)
	if err := runSchedulesRemove(rmCmd, []string{"nonexistent"}); err != nil {
		t.Errorf("remove missing should be idempotent: %v", err)
	}
	if !strings.Contains(rmBuf.String(), "if it existed") {
		t.Errorf("remove output should mention 'if it existed': %s", rmBuf.String())
	}
}
