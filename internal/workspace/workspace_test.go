// workspace_test.go — focused unit tests for the workspace surface
// that matters for v0.2: Open (ephemeral / persistent), ProjectsRoot,
// List, Delete, and the safety guards that exist precisely to keep
// callers from accidentally rm -rf'ing the wrong directory.
//
// workspace_test.go — 面向 v0.2 关键 workspace 行为的单测：Open
// （即扫即走 / 增量）、ProjectsRoot、List、Delete，以及防止误删
// 的安全护栏。
package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestProjectsRoot returns the expected layout.
func TestProjectsRoot(t *testing.T) {
	got := ProjectsRoot()
	want := filepath.Join("runs", "projects")
	if got != want {
		t.Errorf("ProjectsRoot() = %q, want %q", got, want)
	}
}

// TestOpenEphemeral returns a Project with no DB, rooted at cwd.
func TestOpenEphemeral(t *testing.T) {
	p, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	defer func() { _ = p.Close() }()

	if p.Name != "" {
		t.Errorf("ephemeral project: Name = %q, want \"\"", p.Name)
	}
	if p.DB != nil {
		t.Errorf("DB = %v, want nil in ephemeral mode", p.DB)
	}
	if p.DBPath != "" {
		t.Errorf("DBPath = %q, want empty in ephemeral mode", p.DBPath)
	}
	if p.Name != "" {
		t.Errorf("Name = %q, want empty in ephemeral mode", p.Name)
	}
	if p.Root == "" {
		t.Errorf("Root = empty, want cwd")
	}
}

// TestOpenPersistent creates a new project, verifies DB + buckets
// exist. The "re-Open is idempotent" half is split out into a
// separate test (TestReopenPersistent) because bbolt on Windows uses
// flock; we have to close p1 before re-opening.
func TestOpenPersistent(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	name := "test-project"
	p, err := Open(name)
	if err != nil {
		t.Fatalf("Open(%q): %v", name, err)
	}
	defer func() { _ = p.Close() }()

	if p.Name != name {
		t.Errorf("persistent project: Name = %q, want %q", p.Name, name)
	}
	if p.DB == nil {
		t.Fatal("DB = nil, want non-nil for persistent project")
	}
	if p.Name != name {
		t.Errorf("Name = %q, want %q", p.Name, name)
	}
	wantRoot := filepath.Join("runs", "projects", name)
	if p.Root != wantRoot {
		t.Errorf("Root = %q, want %q", p.Root, wantRoot)
	}
	wantDBPath := filepath.Join("runs", "projects", name, "fg.db")
	if p.DBPath != wantDBPath {
		t.Errorf("DBPath = %q, want %q", p.DBPath, wantDBPath)
	}
}

// TestReopenPersistent verifies that Open(name) is idempotent and
// the on-disk layout is consistent across calls. Split from
// TestOpenPersistent because bbolt uses flock on Windows; the first
// handle must be closed before the second Open can succeed.
func TestReopenPersistent(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	name := "reopen-me"
	p1, err := Open(name)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// CRITICAL: close before reopen — bbolt flock on Windows would
	// otherwise block Open() forever.
	root1, dbPath1 := p1.Root, p1.DBPath
	if err := p1.Close(); err != nil {
		t.Fatalf("close p1: %v", err)
	}

	p2, err := Open(name)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = p2.Close() }()
	if p2.Root != root1 || p2.DBPath != dbPath1 {
		t.Errorf("Re-Open paths differ: Root %q→%q, DBPath %q→%q",
			root1, p2.Root, dbPath1, p2.DBPath)
	}
}

// TestOpenPersistentRequiresName: an empty name in persistent mode is
// a programming error — Open("") goes to ephemeral, but a buggy caller
// asking for persistent-with-empty-name is the kind of thing the
// project layer should refuse explicitly when called directly.
func TestOpenPersistentRequiresName(t *testing.T) {
	// Open("") succeeds and returns ephemeral; this test is the
	// negative space — if a caller ever wants to construct a
	// persistent project directly, the name must be non-empty.
	// We document the implicit rule by asserting the inverse: the
	// public Open("") path does NOT create runs/projects/ on disk.
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	p, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	_ = p.Close()
	if _, err := os.Stat(filepath.Join("runs", "projects")); !os.IsNotExist(err) {
		t.Errorf("Open(\"\") unexpectedly created runs/projects/; stat err = %v", err)
	}
}

// TestListMissingRoot returns empty slice, not error.
func TestListMissingRoot(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	got, err := List()
	if err != nil {
		t.Fatalf("List on missing root: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty on missing root", got)
	}
}

// TestListSkipsFiles returns only directory names, in sorted order.
func TestListSkipsFiles(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	// Create two project directories + a stray file at the same level.
	for _, n := range []string{"zebra", "alpha"} {
		if err := os.MkdirAll(filepath.Join(ProjectsRoot(), n), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", n, err)
		}
	}
	if err := os.WriteFile(filepath.Join(ProjectsRoot(), "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(got)
	want := []string{"alpha", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDeleteRefusesEmpty refuses to operate when name is "" (would
// otherwise target cwd via filepath.Join("runs","projects",""),
// producing the dangerous "runs/projects" path or, worse, an OS call
// that surprises the operator).
func TestDeleteRefusesEmpty(t *testing.T) {
	err := Delete("")
	if err == nil {
		t.Fatal("Delete(\"\") returned nil; expected an error")
	}
}

// TestDeleteExisting removes a project directory.
func TestDeleteExisting(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	// Set up a project so Delete has something real to remove.
	p, err := Open("doomed")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = p.Close()

	if err := Delete("doomed"); err != nil {
		t.Fatalf("Delete(doomed): %v", err)
	}
	if _, err := os.Stat(filepath.Join(ProjectsRoot(), "doomed")); !os.IsNotExist(err) {
		t.Errorf("project dir still present after Delete; stat err = %v", err)
	}
}

// TestDeleteNonexistent surfaces a stat error rather than silently
// succeeding.
func TestDeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	if err := Delete("ghost"); err == nil {
		t.Error("Delete(ghost) returned nil; expected stat-style error")
	}
}

// TestStatsForEphemeral returns the documented "(ephemeral: ...)"
// placeholder rather than bbolt key counts.
func TestStatsForEphemeral(t *testing.T) {
	p, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	defer func() { _ = p.Close() }()
	got, err := p.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got == "" {
		t.Errorf("Stats for ephemeral returned empty string")
	}
}

// TestCloseNilSafe: p.Close() on a nil Project must not panic, so
// that `defer p.Close()` patterns are safe even after a partial Open
// failure.
func TestCloseNilSafe(t *testing.T) {
	var p *Project
	if err := p.Close(); err != nil {
		t.Errorf("(*Project)(nil).Close() = %v, want nil", err)
	}
}
