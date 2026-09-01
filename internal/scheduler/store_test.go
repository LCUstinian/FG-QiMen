// store_test.go — round-trip tests for the bbolt-backed
// schedule store. / store_test.go — bbolt 调度存储的往返测
// 试。
package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func newTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := bolt.Open(filepath.Join(dir, "test.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	return NewStore(db), func() { _ = db.Close() }
}

func TestStore_AddGet(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	rec := Record{
		Name:   "daily-9am",
		Mode:   "cron",
		Value:  "0 9 * * *",
		TZ:     "UTC",
		Daemon: true,
	}
	if err := s.Add(rec); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := s.Get("daily-9am")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil after Add")
	}
	if got.Value != rec.Value || got.TZ != rec.TZ || got.Daemon != rec.Daemon {
		t.Errorf("Get = %+v, want %+v", got, rec)
	}
}

func TestStore_GetMissing(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	got, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get = %+v, want nil for missing key", got)
	}
}

func TestStore_List(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := s.Add(Record{Name: name, Mode: "in", Value: "5m"}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List returned %d records, want 3", len(all))
	}
}

func TestStore_RemoveIdempotent(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	if err := s.Remove("nonexistent"); err != nil {
		t.Errorf("Remove on missing key = %v, want nil", err)
	}
	if err := s.Add(Record{Name: "x", Mode: "in", Value: "1m"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Remove("x"); err != nil {
		t.Errorf("Remove: %v", err)
	}
	all, _ := s.List()
	if len(all) != 0 {
		t.Errorf("List after Remove = %d, want 0", len(all))
	}
}

func TestStore_Overwrite(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	_ = s.Add(Record{Name: "k", Mode: "in", Value: "1m"})
	_ = s.Add(Record{Name: "k", Mode: "cron", Value: "0 0 * * *"})
	got, _ := s.Get("k")
	if got == nil || got.Mode != "cron" {
		t.Errorf("Add should overwrite; got mode = %v", got)
	}
}

func TestStore_PreservesCreatedAt(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = s.Add(Record{Name: "k", Mode: "in", Value: "1m", CreatedAt: want})
	_ = s.Add(Record{Name: "k", Mode: "in", Value: "2m"}) // no CreatedAt
	got, _ := s.Get("k")
	if !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.CreatedAt, want)
	}
}
