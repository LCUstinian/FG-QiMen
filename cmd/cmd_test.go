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

	"github.com/LCUstinian/FG-QiMen/internal/session"
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
	flagNoState = true
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
	if !cfg.Resume || !cfg.NoState || !cfg.AliveOnly {
		t.Errorf("Resume/NoState/AliveOnly = %v/%v/%v, want all true", cfg.Resume, cfg.NoState, cfg.AliveOnly)
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

// TestResolveOutputPathFlagValue: an explicit -o/-j value wins over
// both project mode and ephemeral mode, AS LONG AS the resolved
// path stays under the cwd (Stage 18 / P1#18 / F-05 fix).
// We use a cwd-relative path here.
func TestResolveOutputPathFlagValue(t *testing.T) {
	c := &types.Config{Project: "anything"}
	got, err := resolveOutputPath(c, "custom/path.txt", "default.txt")
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
	_, err := resolveOutputPath(c, "../../../../../../etc/passwd", "default.txt")
	if err == nil {
		t.Error("resolveOutputPath(../../etc/passwd) returned nil err; want containment error")
	}
	if !strings.Contains(err.Error(), "outside the current working directory") {
		t.Errorf("error message %q should mention 'outside the current working directory'", err.Error())
	}
}

// TestResolveOutputPathProjectMode: in project mode, the path
// falls back to runs/projects/<name>/<default>.
func TestResolveOutputPathProjectMode(t *testing.T) {
	c := &types.Config{Project: "corp"}
	want := filepath.Join("runs", "projects", "corp", "result.txt")
	got, err := resolveOutputPath(c, "", "result.txt")
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	if got != want {
		t.Errorf("project default = %q, want %q", got, want)
	}
}

// TestResolveOutputPathEphemeralMode: in ephemeral mode (Project ==
// ""), the path falls back to runs/default/<default>.
func TestResolveOutputPathEphemeralMode(t *testing.T) {
	c := &types.Config{Project: ""}
	want := filepath.Join("runs", "default", "creds.txt")
	got, err := resolveOutputPath(c, "", "creds.txt")
	if err != nil {
		t.Fatalf("resolveOutputPath: %v", err)
	}
	if got != want {
		t.Errorf("ephemeral default = %q, want %q", got, want)
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
	wantTXT := filepath.Join("runs", "default", "result.txt")
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
	host, hostsFile, project, mode, ports, excludePorts                     string
	userFile, passFile, outputTXT, outputJSON, plugins                     string
	resume, noState, aliveOnly, silent, noTUI, noICMP, verbose             bool
	threads                                                              int
	timeout, shutdownTime                                                time.Duration
	user, pass                                                           []string
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
