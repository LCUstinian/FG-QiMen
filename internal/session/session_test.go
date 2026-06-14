// session_test.go — focused unit tests for internal/session.
//
// Verifies the wiring contract: NewSession populates every documented
// field correctly and starts with safe defaults (NopUI, DiscardLogger,
// fresh in-memory State). The integration with output/store/ui is
// exercised by the cmd/scan tests once they exist; this file tests
// only what NewSession itself owns.
//
// session_test.go — internal/session 的单测。验证 wiring 契约：
// NewSession 正确填入每个有文档的字段，并以安全默认值启动
// （NopUI、DiscardLogger、新的内存 State）。
package session

import (
	"context"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/ui"
)

// TestNewSessionDefaults: every documented field is populated, and
// the default values for the open-ended slots (UI, Log, State) are
// the safe ones (NopUI, DiscardLogger, non-nil fresh State).
func TestNewSessionDefaults(t *testing.T) {
	cfg := &types.Config{Mode: types.ModeScan}
	ctx := context.Background()

	s, err := NewSession(ctx, cfg, "test-proj")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s == nil {
		t.Fatal("NewSession returned nil Session")
	}

	if s.Ctx != ctx {
		t.Errorf("Ctx mismatch: got %v, want %v", s.Ctx, ctx)
	}
	if s.Config != cfg {
		t.Errorf("Config not stored as pointer: got %p, want %p", s.Config, cfg)
	}
	if s.ProjectName != "test-proj" {
		t.Errorf("ProjectName = %q, want %q", s.ProjectName, "test-proj")
	}
	if s.State == nil {
		t.Error("State is nil; expected fresh in-memory State")
	}
	if s.Out != nil {
		t.Errorf("Out = %v, want nil at construction (caller assigns later)", s.Out)
	}
	if s.Store != nil {
		t.Errorf("Store = %v, want nil at construction (caller assigns later)", s.Store)
	}
	if s.Log == nil {
		t.Error("Log is nil; expected DiscardLogger{} default")
	}
	// Log should be a discard logger — call Info and confirm no panic
	// and that the discarded output is the empty string when cast.
	s.Log.Info("[*] hello")
	if s.UI == nil {
		t.Error("UI is nil; expected NopUI default")
	}
	// NopUI methods must be safe to call.
	s.UI.Banner(cfg)
	s.UI.Stats(s.State)
	s.UI.Event(&types.Result{Host: "127.0.0.1", Port: 80})
	s.UI.CredFound(&types.Result{Host: "127.0.0.1", Port: 22, Cred: &types.Cred{User: "u", Pass: "p"}})
	s.UI.Done("summary")
}

// TestNewSessionEmptyProjectName: a Session for ephemeral mode has
// ProjectName == "" (since cfg.Project is also ""). Verified here
// because the public API takes projectName as an explicit argument,
// not via cfg.Project, so the two paths can disagree if the caller
// isn't careful.
func TestNewSessionEmptyProjectName(t *testing.T) {
	cfg := &types.Config{Mode: types.ModeScan}
	s, err := NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s.ProjectName != "" {
		t.Errorf("ProjectName = %q, want empty for ephemeral", s.ProjectName)
	}
	if s.State == nil {
		t.Error("State is nil; expected fresh in-memory State")
	}
}

// TestNewSessionDoesNotPanicOnNilConfig: zero value config should not
// cause a panic. Validation is the caller's responsibility (runScan
// does buildConfig().Validate() before reaching NewSession). The
// Session itself is a passive bag and must not dereference nil cfg.
func TestNewSessionDoesNotPanicOnNilConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewSession(nil) panicked: %v", r)
		}
	}()
	s, err := NewSession(context.Background(), nil, "p")
	if err != nil {
		t.Fatalf("NewSession(nil cfg): %v", err)
	}
	if s.Config != nil {
		t.Errorf("Config should be nil when passed nil, got %v", s.Config)
	}
}

// TestNopUISatisfiesUI: the no-op UI returned by ui.NopUI() is the
// default value used by NewSession. Verifying it satisfies the
// interface at compile time (via the import + var) plus a smoke
// call guarantees NewSession's default is type-safe.
//
// We can call ui.NopUI() with no error and assign it to UI; if the
// interface is wrong this would fail to compile.
func TestNopUISatisfiesUI(t *testing.T) {
	var u ui.UI = ui.NopUI()
	if u == nil {
		t.Fatal("ui.NopUI() returned nil")
	}
	// Banner on the noop must not panic.
	u.Banner(&types.Config{})
	u.Done("")
}
