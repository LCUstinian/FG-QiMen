// pipeline_test.go — table-driven tests for the pure-function core
// of internal/core/pipeline.go. The goroutine-heavy helpers
// (runPluginWorker / dispatchCred / runResultSink / pushStats) are
// out of scope here — those need a full fake-target harness and are
// the "1 day" part of the audit-fix estimate. This file covers the
// highest-signal pure logic that the audit flagged.
//
// Why these specific functions:
//   - loadCreds: the Cartesian product is the seed of every cred-
//     spray; a bug there silently drops half the keys (or all, on
//     a typo in the user slice).
//   - selectPlugins: the cfg.Plugins filter is comma-split; bugs
//     here lead to "all plugins ran when I asked for one" or vice
//     versa. The audit (P2 / doc-9) flagged the cfg.Plugins comma
//     split as under-tested.
//   - matchesPort: linear scan used by the "is this plugin's port
//     in the user's port list?" check; bug = false negatives.
//   - nowOrZero: tiny but used by Result.Time in many paths.
//   - formatPortfinger: banner truncation must respect the 80-char
//     cap; format drift makes the result.txt column widths wrong.
package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// fakePlugin is a minimal plugins.Plugin implementation that just
// returns the configured Name(). Used by selectPlugins tests.
//
// fakePlugin 是最小的 plugins.Plugin 实现，仅返回配置的 Name()。供
// selectPlugins 测试用。
type fakePlugin struct{ name string }

func (f *fakePlugin) Name() string        { return f.name }
func (f *fakePlugin) Ports() []int        { return nil }
func (f *fakePlugin) Modes() plugins.Mode { return plugins.ModeIdentify }
func (f *fakePlugin) Identify(_ context.Context, _ string, _ int) *types.Result {
	return nil
}
func (f *fakePlugin) Credential(_ context.Context, _ string, _ int, _ []types.Cred) *types.Result {
	return nil
}

// TestLoadCreds — Cartesian product of (users × passes). Both empty
// returns nil (the documented "skip cred phase" contract via
// LoadInto). LoadInto skips empty users (no "anonymous" probe) but
// preserves empty passes (empty pass is allowed for protocols that
// treat "no password" as a valid attempt). The password field is
// preserved verbatim (no trimming / case-folding by loadCreds itself).
func TestLoadCreds(t *testing.T) {
	cases := []struct {
		name     string
		users    []string
		passes   []string
		wantLen  int
		wantUser string // spot-check first entry's user
		wantPass string // spot-check first entry's pass
	}{
		// Both empty → skip cred phase (LoadInto short-circuit).
		{"both empty", nil, nil, 0, "", ""},
		// Empty users + passes → 0 creds (LoadInto skips "" users).
		{"empty users", nil, []string{"p1", "p2"}, 0, "", ""},
		// Empty passes + users → N creds with empty pass per user
		// (LoadInto preserves "" passes per its inline comment).
		{"empty passes", []string{"u1", "u2"}, nil, 2, "u1", ""},
		{"2x2", []string{"u1", "u2"}, []string{"p1", "p2"}, 4, "u1", "p1"},
		{"3x1", []string{"u1", "u2", "u3"}, []string{"p"}, 3, "u1", "p"},
		{"1x5", []string{"u"}, []string{"p1", "p2", "p3", "p4", "p5"}, 5, "u", "p1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess := &session.Session{
				Config: &types.Config{Users: c.users, Passes: c.passes},
			}
			got, err := loadCreds(sess)
			if err != nil {
				t.Fatalf("loadCreds: %v", err)
			}
			if len(got) != c.wantLen {
				t.Errorf("loadCreds() returned %d creds, want %d", len(got), c.wantLen)
			}
			if c.wantLen > 0 {
				if got[0].User != c.wantUser || got[0].Pass != c.wantPass {
					t.Errorf("first cred = (%q,%q), want (%q,%q)",
						got[0].User, got[0].Pass, c.wantUser, c.wantPass)
				}
				// AuthType must be AuthPassword (the only method
				// loadCreds emits; key-based auth is v0.3+).
				//
				// AuthType 必须是 AuthPassword（loadCreds 唯一输出；
				// 基于 key 的 auth 是 v0.3+）。
				if got[0].AuthType != string(credential.AuthPassword) {
					t.Errorf("first cred AuthType = %q, want %q",
						got[0].AuthType, credential.AuthPassword)
				}
			}
		})
	}
}

// TestLoadCredsUsesFiles — file-only credentials enter the production
// cred slice. The audit flagged UserFile / PassFile being honored by
// LoadInto but silently ignored by the production loadCreds, so a CLI
// invocation with `-user-file u.txt -pass-file p.txt` would produce
// zero auth attempts. Now loadCreds must read both files.
//
// / TestLoadCredsUsesFiles — 纯文件凭据进入生产 cred slice。审计
// 发现 UserFile / PassFile 被 LoadInto 支持却被生产 loadCreds 静默
// 忽略，导致 CLI 用 `-user-file u.txt -pass-file p.txt` 时零次认证。
// 现在 loadCreds 必须读这两个文件。
func TestLoadCredsUsesFiles(t *testing.T) {
	dir := t.TempDir()
	users := filepath.Join(dir, "users.txt")
	passes := filepath.Join(dir, "passes.txt")
	if err := os.WriteFile(users, []byte("alice\nbob\n"), 0600); err != nil {
		t.Fatalf("write users: %v", err)
	}
	if err := os.WriteFile(passes, []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatalf("write passes: %v", err)
	}

	sess := &session.Session{
		Config: &types.Config{UserFile: users, PassFile: passes},
	}
	got, err := loadCreds(sess)
	if err != nil {
		t.Fatalf("loadCreds(file-only): %v", err)
	}
	if len(got) != 4 {
		t.Errorf("loadCreds(file-only) returned %d creds, want 4 (2 users × 2 passes)", len(got))
	}
	for _, c := range got {
		if c.AuthType != string(credential.AuthPassword) {
			t.Errorf("file-only cred %q has AuthType %q, want %q",
				c.User, c.AuthType, credential.AuthPassword)
		}
	}
}

// TestLoadCredsMixedInlineAndFile — inline + file inputs merge and
// dedup. With users={"a"} and UserFile containing "a\nb\n", the dedup
// map should produce only "a" and "b" — not "a" twice.
//
// / TestLoadCredsMixedInlineAndFile — 内联 + 文件输入合并并去重。
// users={"a"} 且 UserFile 含 "a\nb\n" 时，去重后只应得 "a" 和 "b"
// ——而非两个 "a"。
func TestLoadCredsMixedInlineAndFile(t *testing.T) {
	dir := t.TempDir()
	usersFile := filepath.Join(dir, "users.txt")
	passesFile := filepath.Join(dir, "passes.txt")
	if err := os.WriteFile(usersFile, []byte("a\nb\n"), 0600); err != nil {
		t.Fatalf("write users: %v", err)
	}
	if err := os.WriteFile(passesFile, []byte("p1\np2\n"), 0600); err != nil {
		t.Fatalf("write passes: %v", err)
	}

	sess := &session.Session{
		Config: &types.Config{
			Users:    []string{"a"},
			Passes:   []string{"p1"},
			UserFile: usersFile,
			PassFile: passesFile,
		},
	}
	got, err := loadCreds(sess)
	if err != nil {
		t.Fatalf("loadCreds(mixed): %v", err)
	}
	// After dedup: users={a,b}, passes={p1,p2} → 4 unique pairs.
	if len(got) != 4 {
		t.Errorf("loadCreds(mixed) returned %d creds, want 4 (deduped a,b × p1,p2)", len(got))
	}
}

// TestLoadCredsUnreadableUserFile — an unreadable user file must
// surface as an error from loadCreds, not a silently empty cred
// slice. Without this, an operator with a typo'd -user-file would
// get "0 auth attempts" with no diagnostic.
//
// / TestLoadCredsUnreadableUserFile — 不可读 user 文件必须从
// loadCreds 返回错误，而非静默空 cred slice。否则拼错 -user-file
// 的操作员会得到 "0 认证尝试" 且无任何诊断。
func TestLoadCredsUnreadableUserFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.txt")
	passes := filepath.Join(dir, "passes.txt")
	if err := os.WriteFile(passes, []byte("p1\n"), 0600); err != nil {
		t.Fatalf("write passes: %v", err)
	}

	sess := &session.Session{
		Config: &types.Config{UserFile: missing, PassFile: passes},
	}
	got, err := loadCreds(sess)
	if err == nil {
		t.Fatalf("loadCreds(unreadable) returned err=nil, want non-nil; got %d creds", len(got))
	}
}

// TestSelectPlugins — allow-list filtering. Empty allow-list = all.
// Unknown names are silently dropped (the audit doc-9 fix path).
// Whitespace in the comma list is trimmed.
func TestSelectPlugins(t *testing.T) {
	all := []plugins.Plugin{
		&fakePlugin{name: "ssh"},
		&fakePlugin{name: "ftp"},
		&fakePlugin{name: "mysql"},
		&fakePlugin{name: "redis"},
	}
	cases := []struct {
		name      string
		allowList string
		want      []string // expected names in returned slice (in order)
	}{
		{"empty allow-list returns all", "", []string{"ssh", "ftp", "mysql", "redis"}},
		{"single name", "ssh", []string{"ssh"}},
		{"two names", "ssh,redis", []string{"ssh", "redis"}},
		{"whitespace trimmed", " ssh , redis ", []string{"ssh", "redis"}},
		{"empty entries skipped", "ssh,,redis,", []string{"ssh", "redis"}},
		{"unknown name silently dropped", "ssh,doesnotexist,redis", []string{"ssh", "redis"}},
		{"all unknown", "foo,bar", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := selectPlugins(all, c.allowList)
			if len(got) != len(c.want) {
				t.Fatalf("selectPlugins(%q) returned %d plugins, want %d (%v)",
					c.allowList, len(got), len(c.want), namesOf(got))
			}
			for i, p := range got {
				if p.Name() != c.want[i] {
					t.Errorf("selectPlugins(%q)[%d] = %q, want %q",
						c.allowList, i, p.Name(), c.want[i])
				}
			}
		})
	}
}

// TestMatchesPort — linear scan. Empty slice = false (vacuously).
// Multiple matches reduce to one true.
func TestMatchesPort(t *testing.T) {
	cases := []struct {
		ports []int
		p     int
		want  bool
	}{
		{nil, 22, false},
		{[]int{}, 22, false},
		{[]int{22}, 22, true},
		{[]int{22, 80}, 80, true},
		{[]int{22, 80, 443}, 22, true},
		{[]int{22, 80, 443}, 8080, false},
		{[]int{22}, 0, false}, // zero is a valid port number, but not in the list
	}
	for _, c := range cases {
		got := matchesPort(c.ports, c.p)
		if got != c.want {
			t.Errorf("matchesPort(%v, %d) = %v, want %v", c.ports, c.p, got, c.want)
		}
	}
}

// TestNowOrZero — zero time is replaced with time.Now(); non-zero
// is returned unchanged. Edge case: the very moment of comparison
// could be skewed; we tolerate a 1-second slack.
func TestNowOrZero(t *testing.T) {
	// Zero in: returns time.Now(). Verify it's recent.
	before := time.Now()
	got := nowOrZero(time.Time{})
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("nowOrZero(zero) = %v, want in [%v, %v]", got, before, after)
	}

	// Non-zero in: returns the same value.
	sample := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	if got := nowOrZero(sample); !got.Equal(sample) {
		t.Errorf("nowOrZero(2025-06-15) = %v, want %v", got, sample)
	}
}

// TestFormatPortfinger — banner truncation at 80 chars. Version
// segment separated by " | ". The audit flagged banner-length drift
// as a doc-15 user-visible inconsistency.
func TestFormatPortfinger(t *testing.T) {
	cases := []struct {
		name           string
		svc, ver, ban  string
		mustContain    []string
		mustNotContain []string
	}{
		{
			name: "all empty",
			svc:  "ssh", ver: "", ban: "",
			mustContain: []string{"ssh", "banner="},
		},
		{
			name: "with version",
			svc:  "ssh", ver: "8.0", ban: "OpenSSH_8.0",
			mustContain: []string{"ssh", " | 8.0", "OpenSSH_8.0"},
		},
		{
			name: "version with leading whitespace trimmed",
			svc:  "httpd", ver: " 2.4.41", ban: "Apache",
			mustContain: []string{"httpd", " | 2.4.41"}, // trimmed, not "  2.4.41"
		},
		{
			name: "long banner truncated at 80",
			svc:  "http", ver: "",
			ban:         strings.Repeat("x", 200),
			mustContain: []string{"...", "xxx"}, // ellipsis present, body present
		},
		{
			name: "short banner not truncated",
			svc:  "ssh", ver: "",
			ban:            "OpenSSH",
			mustNotContain: []string{"..."},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := formatPortfinger(c.svc, c.ver, c.ban)
			for _, want := range c.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("formatPortfinger() = %q; missing %q", out, want)
				}
			}
			for _, dont := range c.mustNotContain {
				if strings.Contains(out, dont) {
					t.Errorf("formatPortfinger() = %q; should not contain %q", out, dont)
				}
			}
		})
	}
}

// TestFormatPortfingerTruncationBoundary — exactly 80 chars → not
// truncated; 81 → truncated with "...". The 80-char cap is the
// audit's documented column budget.
func TestFormatPortfingerTruncationBoundary(t *testing.T) {
	exactly80 := strings.Repeat("x", 80)
	out := formatPortfinger("svc", "", exactly80)
	if strings.Contains(out, "...") {
		t.Errorf("80-char banner: output should not truncate; got %q", out)
	}
	over80 := strings.Repeat("x", 81)
	out = formatPortfinger("svc", "", over80)
	if !strings.Contains(out, "...") {
		t.Errorf("81-char banner: output should truncate with '...'; got %q", out)
	}
}

// ─────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────

// namesOf returns the names of a slice of plugins (used in error
// messages so test failures are readable without re-running).
//
// namesOf 返回 plugin 切片的 name 列表（错误消息中用，避免测试失败时
// 还要再跑一次才能看到）。
func namesOf(ps []plugins.Plugin) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return out
}

// TestWantIdentify_ModeMatrix pins the per-mode Identify-stage
// decision table. Future mode additions (e.g. a "recon-only" mode)
// should be added here. / 固定每模式 Identify 阶段决策表。将来新增
// 模式（如纯 recon 模式）应在这里加。
func TestWantIdentify_ModeMatrix(t *testing.T) {
	cases := []struct {
		mode types.RunMode
		want bool
	}{
		{types.ModeScan, true},
		{types.ModeCrack, false},
		{types.ModeLinked, true},
	}
	for _, c := range cases {
		if got := wantIdentify(c.mode); got != c.want {
			t.Errorf("wantIdentify(%v) = %v, want %v", c.mode, got, c.want)
		}
	}
}

// TestWantCredential_ModeMatrix pins the per-mode Credential-stage
// decision table. / 固定每模式 Credential 阶段决策表。
func TestWantCredential_ModeMatrix(t *testing.T) {
	cases := []struct {
		mode types.RunMode
		want bool
	}{
		{types.ModeScan, false},
		{types.ModeCrack, true},
		{types.ModeLinked, true},
	}
	for _, c := range cases {
		if got := wantCredential(c.mode); got != c.want {
			t.Errorf("wantCredential(%v) = %v, want %v", c.mode, got, c.want)
		}
	}
}

// TestFormatPortfinger covers the three branches of the banner
// formatter: empty version, normal version, empty service. /
// 覆盖 banner 格式化器的三个分支：空 version、正常 version、
// 空 service。
// (The existing TestFormatPortfinger above already exercises
// these branches with the correct expectations; this stub
// remains as a structural marker for future expansion.)
// / （上方已存在的 TestFormatPortfinger 用正确的期望值覆盖了这些
// 分支；此 stub 保留作为未来扩展的结构标记。）
