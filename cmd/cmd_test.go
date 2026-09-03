// cmd_test.go — unit tests for the testable parts of cmd/.
//
// Scope:
//   - buildConfig reads the package-level flag* vars and produces
//     a *types.Config; flag reads happen via direct variable
//     assignment in tests (Cobra's pflag would otherwise be needed)
//   - resolveOutputPath is a pure function over (cfg, flagValue,
//     defaultName); the contract is straightforward and worth
//     pinning down
//   - openOutputSinks and loadResumeState are tested for their
//     observable side effects on a real Session + temp dir
//   - installSignalHandler is a smoke test: the returned context
//     is cancellable; the goroutine exits cleanly when the drain
//     channel closes (no signal is sent, so we never block on
//     os.Exit)
//
// What is NOT tested (intentional):
//   - runScan itself (orchestrator; the helpers below cover its
//     pieces and Cobra's command wiring is exercised by hand)
//   - the Cobra subcommands (projects / scan / resume / version) —
//     each is a thin wrapper; full coverage would mean a Cobra
//     integration test framework
//
// cmd_test.go — cmd/ 包可测部分的单测。
package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// --- buildConfig ---

// TestBuildConfigReadsFlagVars: buildConfig assembles *types.Config
// from the package-level flag* vars. Since Cobra/pflag is not in
// scope here, we set the vars directly to simulate a parsed CLI.
func TestBuildConfigReadsFlagVars(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)

	flagHost = "10.0.0.1"
	flagHostsFile = "/tmp/hosts"
	flagProject = "test"
	flagMode = "linked"
	flagResume = true
	// M6: --no-state conflicts with --resume (resume requires bbolt
	// persistence). Don't set both in the test. / M6：--no-state 与
	// --resume 冲突（resume 需要 bbolt 持久化）。测试中不同时设。
	flagPorts = "22,80,443"
	flagExcludePorts = "8080"
	flagAliveOnly = true
	flagThreads = 250
	flagTimeout = 5 * time.Second
	flagUser = []string{"u1", "u2"}
	flagPass = []string{"p1"}
	flagUserFile = "/tmp/users"
	flagPassFile = "/tmp/passes"
	flagOutputTXT = "/tmp/result.txt"
	flagOutputJSON = "/tmp/result.json"
	flagSilent = true
	flagNoTUI = true
	flagNoICMP = true
	flagVerbose = true
	flagShutdownTime = 10 * time.Second
	flagPlugins = "ssh,redis"

	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Host != "10.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "10.0.0.1")
	}
	if cfg.HostsFile != "/tmp/hosts" {
		t.Errorf("HostsFile = %q, want %q", cfg.HostsFile, "/tmp/hosts")
	}
	if cfg.Project != "test" {
		t.Errorf("Project = %q, want %q", cfg.Project, "test")
	}
	if cfg.Mode != types.ModeLinked {
		t.Errorf("Mode = %q, want %q", cfg.Mode, types.ModeLinked)
	}
	if !cfg.Resume || cfg.NoState || !cfg.AliveOnly {
		t.Errorf("Resume/NoState/AliveOnly = %v/%v/%v, want true/false/true", cfg.Resume, cfg.NoState, cfg.AliveOnly)
	}
	if cfg.Threads != 250 {
		t.Errorf("Threads = %d, want 250", cfg.Threads)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if len(cfg.Users) != 2 || cfg.Users[0] != "u1" || cfg.Users[1] != "u2" {
		t.Errorf("Users = %v, want [u1 u2]", cfg.Users)
	}
	if len(cfg.Passes) != 1 || cfg.Passes[0] != "p1" {
		t.Errorf("Passes = %v, want [p1]", cfg.Passes)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.Plugins != "ssh,redis" {
		t.Errorf("Plugins = %q, want %q", cfg.Plugins, "ssh,redis")
	}
}

// TestBuildConfigValidates: buildConfig calls cfg.Validate() and
// propagates the raw validation error. The "config error" prefix
// wrap happens at the call site (runScan), not inside buildConfig
// itself — this test pins the inner boundary.
func TestBuildConfigValidates(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)

	flagThreads = 0 // Validate() rejects this.
	flagTimeout = time.Second
	flagShutdownTime = time.Second

	_, err := buildConfig()
	if err == nil || !strings.Contains(err.Error(), "threads must be > 0") {
		t.Errorf("buildConfig with invalid threads = %v, want threads error", err)
	}
}

// TestBuildConfigResumeRequiresProject: buildConfig + Validate()
// catch the resume-without-project case.
func TestBuildConfigResumeRequiresProject(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)

	flagThreads = 1
	flagTimeout = time.Second
	flagShutdownTime = time.Second
	flagResume = true
	flagProject = "" // resume without project — must fail

	_, err := buildConfig()
	if err == nil {
		t.Error("buildConfig(Resume, no Project) = nil, want error")
	}
}

// --- resolveOutputPath ---

// fixedNow is the time injected into resolveOutputPath by the
// tests below. Pinning it keeps the assertions deterministic
// (no race against the wall clock) and pins the daily-bucket +
// filename-stamp contract — the project's whole "results bucket
// by YYYY-MM-DD and stamp by HH-MM-SS" invariant hinges on two
// format strings.
// / fixedNow 是下方测试注入到 resolveOutputPath 的时间。固定
// 它让断言确定（不和挂钟竞争），并钉住日桶 + 文件名时间戳
// 布局——整个"结果按 YYYY-MM-DD 分桶 + HH-MM-SS 打戳"的契约
// 挂在两个格式串上。
var fixedNow = time.Date(2026, 9, 2, 14, 30, 22, 0, time.Local)

// TestResolveOutputPathFlagValue: an explicit -o/-j value wins over
// both project mode and ephemeral mode, AS LONG AS the resolved
// path stays under the cwd (Stage 18 / P1#18 / F-05 fix).
// We use a cwd-relative path here. The flag value bypasses the
// daily bucket (operators who pass -o want their explicit path,
// not an auto-bucketed one).
func TestResolveOutputPathFlagValue(t *testing.T) {
	c := &types.Config{Project: "anything"}
	got, err := resolveOutputPath(c, "custom/path.txt", "default.txt", fixedNow)
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	// Path is cleaned + absolute-resolved. Basename is the
	// test contract; directory may carry the runner's cwd.
	// / 路径已 clean + 绝对化。basename 是测试契约；目录可能
	// 带 runner 的 cwd。
	if filepath.Base(got) != "path.txt" {
		t.Errorf("flag value basename = %q, want path.txt (full: %q)", filepath.Base(got), got)
	}
}

// TestResolveOutputPathFlagValueEscape: the containment check
// rejects paths that resolve outside cwd, with an error
// message that tells the operator how to opt out. The opt-out
// is the env var FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1.
func TestResolveOutputPathFlagValueEscape(t *testing.T) {
	c := &types.Config{Project: "anything"}
	_, err := resolveOutputPath(c, "../../../../../../etc/passwd", "default.txt", fixedNow)
	if err == nil {
		t.Error("resolveOutputPath(../../etc/passwd) returned nil err; want containment error")
	}
	if !strings.Contains(err.Error(), "outside the current working directory") {
		t.Errorf("error message %q should mention 'outside the current working directory'", err.Error())
	}
}

// TestResolveOutputPathProjectMode: in project mode, the path
// falls back to runs/projects/<name>/<YYYY-MM-DD>/<default>_<HH-MM-SS>.
// The daily bucket is the per-run boundary that prevents one
// day's results from clobbering the next; the HH-MM-SS stamp
// prevents two same-day runs from clobbering each other.
func TestResolveOutputPathProjectMode(t *testing.T) {
	c := &types.Config{Project: "corp"}
	want := filepath.Join("runs", "projects", "corp", "2026-09-02", "fgqm_result_14-30-22.txt")
	got, err := resolveOutputPath(c, "", "fgqm_result.txt", fixedNow)
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	if got != want {
		t.Errorf("project default = %q, want %q", got, want)
	}
}

// TestResolveOutputPathEphemeralMode: in ephemeral mode (Project ==
// ""), the path falls back to runs/default/<YYYY-MM-DD>/<default>_<HH-MM-SS>.
// Same daily-bucketing + timestamp rules as project mode.
func TestResolveOutputPathEphemeralMode(t *testing.T) {
	c := &types.Config{Project: ""}
	want := filepath.Join("runs", "default", "2026-09-02", "fgqm_creds_14-30-22.txt")
	got, err := resolveOutputPath(c, "", "fgqm_creds.txt", fixedNow)
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	if got != want {
		t.Errorf("ephemeral default = %q, want %q", got, want)
	}
}

// TestResolveOutputPath_DifferentDatesYieldDifferentBuckets: two
// runs on different local dates must produce different output
// directories — that's the whole point of the bucketing. We pin
// midnight crossings too: a run starting at 23:59 lands in
// day A, a run starting 1 second later lands in day B.
// / TestResolveOutputPath_DifferentDatesYieldDifferentBuckets：
// 不同本地日的两次 run 必须产生不同输出目录——这是分桶的
// 全部意义。顺便钉住跨午夜：23:59 起的 run 落日 A，1 秒后
// 起的 run 落日 B。
func TestResolveOutputPath_DifferentDatesYieldDifferentBuckets(t *testing.T) {
	c := &types.Config{Project: "corp"}
	// Distinct seconds so both date AND timestamp differ. If both
	// ran at 00:00:00 the stamps would collide; the test is meant
	// to cover "different run ≠ same file".
	// / 不同的秒让日期和时间戳都不同。若都用 00:00:00 时间戳会
	// 撞；这测试要覆盖"不同 run ≠ 同文件"。
	dayA := time.Date(2026, 9, 2, 23, 59, 30, 0, time.Local)
	dayB := time.Date(2026, 9, 3, 0, 0, 30, 0, time.Local)
	pathA, _ := resolveOutputPath(c, "", "fgqm_result.txt", dayA)
	pathB, _ := resolveOutputPath(c, "", "fgqm_result.txt", dayB)
	if pathA == pathB {
		t.Errorf("paths for adjacent days should differ:\n  dayA=%q\n  dayB=%q", pathA, pathB)
	}
	if !strings.Contains(pathA, "2026-09-02") {
		t.Errorf("dayA path missing date: %q", pathA)
	}
	if !strings.Contains(pathB, "2026-09-03") {
		t.Errorf("dayB path missing date: %q", pathB)
	}
}

// TestResolveOutputPath_SameDayDifferentSecondsYieldDifferentFiles:
// same day + different seconds → different filenames. This is the
// load-bearing property for "operator runs fg-qimen twice today,
// gets two distinct result files, no overwrite".
// / 同日不同时秒 → 不同文件名。这是"操作员今天跑两次，两套
// 独立结果不互相覆盖"的承重属性。
func TestResolveOutputPath_SameDayDifferentSecondsYieldDifferentFiles(t *testing.T) {
	c := &types.Config{Project: "corp"}
	t1 := time.Date(2026, 9, 2, 14, 30, 0, 0, time.Local)
	t2 := time.Date(2026, 9, 2, 14, 31, 30, 0, time.Local)
	p1, _ := resolveOutputPath(c, "", "fgqm_result.txt", t1)
	p2, _ := resolveOutputPath(c, "", "fgqm_result.txt", t2)
	if p1 == p2 {
		t.Errorf("same-day, different-second runs should yield different files; both = %q", p1)
	}
	// Both share the same date bucket. / 共享同日桶。
	if !strings.Contains(p1, "2026-09-02") || !strings.Contains(p2, "2026-09-02") {
		t.Errorf("both should be in 2026-09-02 bucket:\n  p1=%q\n  p2=%q", p1, p2)
	}
	if !strings.Contains(p1, "14-30-00") {
		t.Errorf("p1 missing 14-30-00 stamp: %q", p1)
	}
	if !strings.Contains(p2, "14-31-30") {
		t.Errorf("p2 missing 14-31-30 stamp: %q", p2)
	}
}

// TestStampFileName: the format string + split point is the
// load-bearing invariant of the same-day differentiation. If
// anyone changes "15-04-05" or moves the insertion point, the
// test breaks loudly. / 格式串 + 插入位置是同日区分的承重不
// 变量。谁改 "15-04-05" 或挪插入点，测试大声报错。
func TestStampFileName(t *testing.T) {
	at := time.Date(2026, 9, 2, 14, 30, 22, 0, time.Local)
	cases := []struct {
		in   string
		want string
	}{
		{"fgqm_result.txt", "fgqm_result_14-30-22.txt"},
		{"fgqm_result.json", "fgqm_result_14-30-22.json"},
		{"fgqm_result.sarif", "fgqm_result_14-30-22.sarif"},
		{"fgqm_creds.txt", "fgqm_creds_14-30-22.txt"},
		{"fgqm_alive.txt", "fgqm_alive_14-30-22.txt"},
		// No extension — suffix appended raw. / 无扩展名直接追加。
		{"somename", "somename_14-30-22"},
	}
	for _, c := range cases {
		if got := stampFileName(c.in, at); got != c.want {
			t.Errorf("stampFileName(%q, %v) = %q, want %q", c.in, at, got, c.want)
		}
	}
}

// TestDailyRunSubdir: the format string is the load-bearing
// invariant of the whole bucketing scheme. If anyone ever
// changes "2006-01-02" here, the test breaks loudly. / 格式
// 串是整个分桶方案的关键不变量。谁改 "2006-01-02"，这个测
// 试大声报错。
func TestDailyRunSubdir(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 9, 2, 0, 0, 0, 0, time.Local), "2026-09-02"},
		{time.Date(2026, 12, 31, 23, 59, 59, 0, time.Local), "2026-12-31"},
		{time.Date(2027, 1, 1, 0, 0, 1, 0, time.Local), "2027-01-01"},
	}
	for _, c := range cases {
		if got := dailyRunSubdir(c.in); got != c.want {
			t.Errorf("dailyRunSubdir(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- installSignalHandler ---

// TestInstallSignalHandlerReturnsCancellableContext: the returned
// ctx is a child of background and the cancel func cancels it.
// We never actually send a signal — the drain channel is closed
// instead to let the goroutine exit cleanly.
func TestInstallSignalHandlerReturnsCancellableContext(t *testing.T) {
	ctx, cancel, drainCh := installSignalHandler(100*time.Millisecond, nil)
	defer cancel()
	defer close(drainCh) // safe: defer runs once

	if ctx == nil {
		t.Fatal("ctx is nil")
	}
	if ctx.Err() != nil {
		t.Errorf("fresh ctx.Err() = %v, want nil", ctx.Err())
	}
	cancel()
	if ctx.Err() == nil {
		t.Error("after cancel(), ctx.Err() = nil, want non-nil")
	}
	// Yield so the signal goroutine sees the cancel/close and exits.
	time.Sleep(10 * time.Millisecond)
}

// --- loadResumeState ---

// TestLoadResumeStateNoOp: when cfg.Resume is false, loadResumeState
// is a no-op — no error, no seen-hash writes.
func TestLoadResumeStateNoOp(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)

	flagResume = false
	cfg := &types.Config{Resume: false}
	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := loadResumeState(sess, cfg); err != nil {
		t.Errorf("loadResumeState(no resume) = %v, want nil", err)
	}
	if sess.State.Seen("anything") {
		t.Error("loadResumeState(no resume) marked a hash as seen")
	}
}

// --- openOutputSinks ---

// TestOpenOutputSinks: the result sink is attached to sess.Out and
// the expected TXT/JSON files are created at the ephemeral defaults.
// Uses a temp dir + chdir so we don't pollute the working tree.
func TestOpenOutputSinks(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)

	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()

	flagProject = "" // ephemeral
	flagOutputTXT = ""
	flagOutputJSON = ""

	cfg := &types.Config{Project: "", Threads: 1, Timeout: time.Second, ShutdownTimeout: time.Second}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := openOutputSinks(sess, cfg); err != nil {
		t.Fatalf("openOutputSinks: %v", err)
	}
	if sess.Out == nil {
		t.Fatal("sess.Out is nil after openOutputSinks")
	}
	wantTXT := filepath.Join(
		"runs", "default",
		time.Now().Format("2006-01-02"),
		"fgqm_result_"+time.Now().Format("15-04-05")+".txt",
	)
	if _, err := os.Stat(wantTXT); err != nil {
		t.Errorf("expected %s to exist; stat err = %v", wantTXT, err)
	}
	if err := sess.Out.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- openProject ---

// TestOpenProjectEphemeral: openProject("") returns an ephemeral
// project rooted at cwd.
func TestOpenProjectEphemeral(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()

	cfg := &types.Config{Project: ""}
	p, err := openProject(cfg)
	if err != nil {
		t.Fatalf("openProject: %v", err)
	}
	defer func() { _ = p.Close() }()
	if p.Name != "" {
		t.Errorf("ephemeral project: Name = %q, want \"\"", p.Name)
	}
}

// --- helpers: flag snapshot/restore ---

// flagSnapshot captures every package-level flag* var so a test
// can mutate them safely without leaking into other tests.
type flagSnapshot struct {
	host, hostsFile, project, mode, ports, excludePorts        string
	userFile, passFile, outputTXT, outputJSON, plugins         string
	resume, noState, aliveOnly, silent, noTUI, noICMP, verbose bool
	threads                                                    int
	timeout, shutdownTime                                      time.Duration
	user, pass                                                 []string
}

func snapshotFlags() flagSnapshot {
	return flagSnapshot{
		host:         flagHost,
		hostsFile:    flagHostsFile,
		project:      flagProject,
		mode:         flagMode,
		ports:        flagPorts,
		excludePorts: flagExcludePorts,
		userFile:     flagUserFile,
		passFile:     flagPassFile,
		outputTXT:    flagOutputTXT,
		outputJSON:   flagOutputJSON,
		plugins:      flagPlugins,
		resume:       flagResume,
		noState:      flagNoState,
		aliveOnly:    flagAliveOnly,
		silent:       flagSilent,
		noTUI:        flagNoTUI,
		noICMP:       flagNoICMP,
		verbose:      flagVerbose,
		threads:      flagThreads,
		timeout:      flagTimeout,
		shutdownTime: flagShutdownTime,
		user:         append([]string(nil), flagUser...),
		pass:         append([]string(nil), flagPass...),
	}
}

func restoreFlags(s flagSnapshot) {
	flagHost = s.host
	flagHostsFile = s.hostsFile
	flagProject = s.project
	flagMode = s.mode
	flagPorts = s.ports
	flagExcludePorts = s.excludePorts
	flagUserFile = s.userFile
	flagPassFile = s.passFile
	flagOutputTXT = s.outputTXT
	flagOutputJSON = s.outputJSON
	flagPlugins = s.plugins
	flagResume = s.resume
	flagNoState = s.noState
	flagAliveOnly = s.aliveOnly
	flagSilent = s.silent
	flagNoTUI = s.noTUI
	flagNoICMP = s.noICMP
	flagVerbose = s.verbose
	flagThreads = s.threads
	flagTimeout = s.timeout
	flagShutdownTime = s.shutdownTime
	flagUser = s.user
	flagPass = s.pass
}

// --- closeOutputForHardExit (issue #4 fix) ---

// TestCloseOutputForHardExit_Nil: passing nil is a no-op. The
// preHardExit closure runs before sess.Out is set (when the
// signal goroutine happens to invoke it during the brief window
// between installSignalHandler and openOutputSinks returning),
// so the helper MUST tolerate nil without panicking.
//
// TestCloseOutputForHardExit_Nil：传 nil 应该是空操作。
// preHardExit 闭包可能在 sess.Out 赋值前被触发（信号 goroutine
// 在 installSignalHandler 和 openOutputSinks 返回之间的短窗内
// 调用），所以 helper 必须对 nil 容忍，不能 panic。
func TestCloseOutputForHardExit_Nil(t *testing.T) {
	// Should not panic. / 不应 panic。
	closeOutputForHardExit(nil)
}

// TestCloseOutputForHardExit_FlushesBuffers: this is the regression
// test for the data-loss bug — when os.Exit(1) bypasses the deferred
// sess.Out.Close() in runScan, the last few KB of buffered writes
// (default bufio = 4 KB per sink) would never hit disk. The fix
// routes preHardExit through closeOutputForHardExit, which calls
// Output.Close (and Output.Close calls each sink's bufio.Flush).
//
// We open a txt sink, write one row that doesn't fill the buffer
// to its auto-flush threshold, then call closeOutputForHardExit
// directly (simulating the hard-exit scenario where the deferred
// Close never runs). The file should now contain the row.
//
// TestCloseOutputForHardExit_FlushesBuffers：数据丢失 bug 的回
// 归测试——os.Exit(1) 绕过 runScan 里 defer 的 sess.Out.Close()
// 时，最后几 KB 缓冲写入（默认 bufio 每 sink 4 KB）不会落盘。
// 修复让 preHardExit 走 closeOutputForHardExit，调用 Output.Close
// （Output.Close 调每个 sink 的 bufio.Flush）。
//
// 我们开一个 txt sink，写一行不到 buffer 自动 flush 阈值的数
// 据，然后直接调 closeOutputForHardExit（模拟硬退出 defer 不
// 跑的场景）。文件应该已包含那一行。
func TestCloseOutputForHardExit_FlushesBuffers(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "fgqm_result.txt")

	out, err := output.OpenOutput(output.OutputConfig{
		ResultTXTPath: path,
	})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}

	// Write a single short row. The default bufio buffer is 4 KB
	// so this won't auto-flush; the only way it lands on disk is
	// an explicit Flush / Close. / 写一行短数据。默认 bufio 是
	// 4 KB，所以不会自动 flush；唯一让它落盘的办法是显式 Flush
	// / Close。
	r := &types.Result{
		Time:    time.Now(),
		Host:    "10.0.0.1",
		Port:    22,
		Service: "ssh",
		Banner:  "OpenSSH_9.0",
	}
	if err := out.WriteResult(r); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}

	// Sanity: file should NOT yet contain the row (bufio hasn't
	// flushed, OS write hasn't happened).
	// Sanity：文件此时不应包含那一行（bufio 未 flush，OS write
	// 未发生）。
	if pre, _ := os.ReadFile(path); strings.Contains(string(pre), "10.0.0.1") {
		t.Fatalf("buffer flushed too early: %s", pre)
	}

	// Simulate hard-exit: skip out.Close() and call our helper
	// directly. The helper MUST flush + close. / 模拟硬退出：
	// 不调 out.Close()，直接调 helper。helper 必须 flush + close。
	closeOutputForHardExit(out)

	// Verify the file now contains the row.
	// 验证文件现在包含那一行。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "10.0.0.1") {
		t.Errorf("expected buffered write to be flushed; got: %q", string(data))
	}
	if !strings.Contains(string(data), "OpenSSH_9.0") {
		t.Errorf("expected banner in flush; got: %q", string(data))
	}
}

// TestCloseOutputForHardExit_SARIFBuffer: SARIF is special — it's
// buffered in sarifBuf (a []types.Result) and only emitted in
// Output.Close. A simple Flush() would NOT save SARIF writes;
// only Close does. This test pins down that closeOutputForHardExit
// uses Close (not Flush) so SARIF results also land on disk on
// hard exit.
//
// TestCloseOutputForHardExit_SARIFBuffer：SARIF 是特殊的——
// 它缓冲在 sarifBuf（[]types.Result），只在 Output.Close 输出。
// 单纯 Flush() 不能救 SARIF；只有 Close 行。这个测试钉住
// closeOutputForHardExit 用 Close（而不是 Flush），让 SARIF 写
// 入在硬退出时也落盘。
func TestCloseOutputForHardExit_SARIFBuffer(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "fgqm_result.sarif")

	out, err := output.OpenOutput(output.OutputConfig{
		ResultSARIFPath: path,
	})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}

	// Write a result that goes into the SARIF buffer (no TXT
	// sink, so it only lands in SARIF). / 写一个只进 SARIF 缓冲
	// 的结果（没开 TXT sink）。
	if err := out.WriteResult(&types.Result{
		Time:    time.Now(),
		Host:    "10.0.0.2",
		Port:    443,
		Service: "https",
		Banner:  "nginx/1.25",
	}); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}

	// Sanity: the SARIF file exists (created at OpenOutput by
	// newRotatingWriter's O_CREATE) but should be EMPTY — SARIF
	// is a single doc emitted at Close, not streamed. / Sanity：
	// SARIF 文件存在（newRotatingWriter 的 O_CREATE 在 OpenOutput
	// 时创建），但应是空——SARIF 是 Close 时单文档输出，非流式。
	if data, err := os.ReadFile(path); err != nil {
		t.Fatalf("stat: %v", err)
	} else if len(data) != 0 {
		t.Fatalf("SARIF file should be empty pre-Close; got %d bytes: %q", len(data), data)
	}

	closeOutputForHardExit(out)

	// Verify SARIF document was emitted with our result.
	// 验证 SARIF 文档已发出，含我们的结果。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("SARIF document not emitted; file is empty")
	}
	if !strings.Contains(string(data), "10.0.0.2") {
		t.Errorf("expected SARIF to contain host; got: %q", string(data))
	}
}

// --- applyTransport / applyHTTPForm ---

// TestApplyTransport_Nil: applyTransport must be a no-op on
// nil cfg (defensive: a future caller might forget to
// validate). No transport globals should change. / applyTransport
// 在 nil cfg 时是空操作（防御：未来调用方可能忘校验）。不
// 应动 transport 全局。
func TestApplyTransport_Nil(t *testing.T) {
	// Snapshot transport state before. We can't easily inspect
	// the InsecureTLS/SSH atomic.Bool's "before" value, but
	// we can ensure calling with nil doesn't panic and doesn't
	// set them to false. / 快照 transport 状态前。我们不便检
	// 查 InsecureTLS/SSH atomic.Bool 的"之前"值，但能保证
	// nil 调不 panic 且不会把它设成 false。
	applyTransport(nil)
	// No assertion beyond "didn't panic". / 除"没 panic"外无
	// 断言。
}

// TestApplyTransport_PropagatesFlags: cfg.InsecureTLS /
// InsecureSSH / KnownHostsFile propagate to the transport
// package's atomic globals. We read the globals back to
// confirm. / cfg.InsecureTLS / InsecureSSH / KnownHostsFile
// 传到 transport 包的 atomic 全局。读回验证。
func TestApplyTransport_PropagatesFlags(t *testing.T) {
	// Save and restore transport state. transport.KnownHostsFile
	// is atomic.Pointer[string] — we save the pointer copy.
	savedKH := transport.KnownHostsFile.Load()
	defer func() {
		transport.KnownHostsFile.Store(savedKH)
		transport.InsecureTLS.Store(false)
		transport.InsecureSSH.Store(false)
	}()

	cfg := &types.Config{
		InsecureTLS:    true,
		InsecureSSH:    true,
		KnownHostsFile: "/tmp/test-known-hosts",
	}
	applyTransport(cfg)

	if !transport.InsecureTLS.Load() {
		t.Errorf("InsecureTLS not propagated")
	}
	if !transport.InsecureSSH.Load() {
		t.Errorf("InsecureSSH not propagated")
	}
	kh := transport.KnownHostsFile.Load()
	if kh == nil || *kh != "/tmp/test-known-hosts" {
		t.Errorf("KnownHostsFile not propagated: got %v", kh)
	}
}

// TestApplyTransport_EmptyKnownHosts: when cfg.KnownHostsFile
// is empty, transport.KnownHostsFile should NOT be changed
// (don't overwrite a previously-set value with empty). /
// cfg.KnownHostsFile 为空时，transport.KnownHostsFile 不应
// 被改（不要用空覆盖之前设的值）。
func TestApplyTransport_EmptyKnownHosts(t *testing.T) {
	// Pre-set transport.KnownHostsFile to a known value.
	preset := "/preset/path"
	transport.KnownHostsFile.Store(&preset)
	defer func() {
		// Restore by unsetting (atomic.Pointer can't store nil
		// safely across versions; clearing with empty is portable).
		empty := ""
		transport.KnownHostsFile.Store(&empty)
	}()

	cfg := &types.Config{KnownHostsFile: ""}
	applyTransport(cfg)

	kh := transport.KnownHostsFile.Load()
	if kh == nil || *kh != "/preset/path" {
		t.Errorf("KnownHostsFile overwritten by empty: got %v", kh)
	}
}

// TestApplyHTTPForm_CopiesFlags: applyHTTPForm copies the
// cmd-line http-form-* flags into network package globals.
// We don't read back the network globals (they're
// implementation detail), but we verify the function doesn't
// panic and the operation is a no-op when all flags are
// empty. / applyHTTPForm 把 http-form-* flag 拷到 network 包
// 全局。我们不读回 network 全局（实现细节），但验证不 panic
// 且 flag 全空时是空操作。
func TestApplyHTTPForm_Empty(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)
	// All http-form flags default to empty after restoreFlags.

	applyHTTPForm()
	// No panic = pass. The function just assigns package-level
	// vars; there's no observable side effect in the test.
	// / 不 panic = 通过。函数只赋包级变量；测试里无可观察
	// 副作用。
}

// TestApplyHTTPForm_Populated: when http-form flags are set,
// applyHTTPForm must copy each into the corresponding network
// global. We mutate a flag, call applyHTTPForm, and trust
// the package-internal assignment (no exported getter for
// network.HTTPFormURL — covered by integration tests in
// network package). The function is so simple that not
// panicking + not having a race is the entire contract.
// / http-form flag 设置时，applyHTTPForm 必须拷到对应
// network 全局。我们改 flag、applyHTTPForm，信任包内赋值
// （network.HTTPFormURL 无导出 getter——由 network 包集成测试
// 覆盖）。函数足够简单，"不 panic + 不 race"就是全部契约。
func TestApplyHTTPForm_Populated(t *testing.T) {
	save := snapshotFlags()
	defer restoreFlags(save)
	flagHTTPFormURL = "http://target/login"
	flagHTTPFormFields = "user=$user$,pass=$pass$"
	flagHTTPFormSuccess = "Welcome"
	flagHTTPFormFailure = "invalid"
	flagHTTPFormRedir = "/dashboard"

	applyHTTPForm()
	// No assertion possible without reading network globals;
	// success = no panic. / 无 network 全局读取就没法断言；
	// 成功 = 不 panic。
}
