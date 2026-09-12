// prune_test.go — retention tests for CountSeenBefore / PruneSeen
// (the M-5 audit fix).
//
// prune_test.go — CountSeenBefore / PruneSeen 的保留策略测试
// （M-5 审计修复）。
package store

import (
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// TestPruneSeen deletes only entries older than the cutoff, leaves
// results/creds untouched, and keeps unparsable timestamps (fail-open).
// / TestPruneSeen 只删除早于截止时间的条目，results/creds 不动，损坏
// 时间戳保留（fail-open）。
func TestPruneSeen(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)

	old := time.Now().Add(-48 * time.Hour).UTC()
	mid := time.Now().Add(-1 * time.Hour).UTC()
	newT := time.Now().UTC()

	for _, tc := range []struct {
		hash string
		when time.Time
	}{{"old-1", old}, {"old-2", old}, {"mid-1", mid}, {"new-1", newT}} {
		if err := s.MarkSeenPersisted(tc.hash, tc.when); err != nil {
			t.Fatalf("MarkSeenPersisted(%s): %v", tc.hash, err)
		}
	}
	// Findings must survive pruning. / 发现记录必须在 prune 后存活。
	if err := s.PutResult("result-1", map[string]string{"h": "10.0.0.1"}); err != nil {
		t.Fatalf("PutResult: %v", err)
	}
	if err := s.PutCred("cred-1", map[string]string{"u": "admin"}); err != nil {
		t.Fatalf("PutCred: %v", err)
	}
	// A corrupted timestamp must be kept, not deleted (fail-open).
	// / 损坏时间戳必须保留而非删除（fail-open）。
	if err := db.Update(func(tx *bolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists(bucketTargets)
		if err != nil {
			return err
		}
		return bk.Put([]byte("corrupt-1"), []byte("not-a-timestamp"))
	}); err != nil {
		t.Fatalf("write corrupt entry: %v", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour).UTC()

	got, err := s.CountSeenBefore(cutoff)
	if err != nil {
		t.Fatalf("CountSeenBefore: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountSeenBefore = %d, want 2 (old-1, old-2)", got)
	}

	deleted, err := s.PruneSeen(cutoff)
	if err != nil {
		t.Fatalf("PruneSeen: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("PruneSeen deleted %d, want 2", deleted)
	}

	hashes, err := s.LoadSeenHashes()
	if err != nil {
		t.Fatalf("LoadSeenHashes: %v", err)
	}
	want := map[string]bool{"mid-1": true, "new-1": true, "corrupt-1": true}
	if len(hashes) != len(want) {
		t.Fatalf("after prune got %d hashes, want %d: %v", len(hashes), len(want), hashes)
	}
	for _, h := range hashes {
		if !want[h] {
			t.Errorf("unexpected hash %q survived prune", h)
		}
	}

	// Results and creds buckets untouched. / results / creds 不动。
	if !s.IsSeenPersisted("mid-1") {
		t.Error("mid-1 should have survived")
	}
	var nResults, nCreds int
	_ = db.View(func(tx *bolt.Tx) error {
		nResults = tx.Bucket(bucketResults).Stats().KeyN
		nCreds = tx.Bucket(bucketCreds).Stats().KeyN
		return nil
	})
	if nResults != 1 {
		t.Errorf("results bucket has %d entries, want 1", nResults)
	}
	if nCreds != 1 {
		t.Errorf("creds bucket has %d entries, want 1", nCreds)
	}
}

// TestPruneSeen_Encrypted prunes a store with the encryption layer on.
// Prune works on plaintext keys only, so encryption must be irrelevant.
// / TestPruneSeen_Encrypted 对启用加密层的 store 做 prune。prune 只
// 操作明文 key，加密与否必须无关。
func TestPruneSeen_Encrypted(t *testing.T) {
	db := openTestDB(t)
	enc, err := NewEncryptedValue("test-passphrase")
	if err != nil {
		t.Fatalf("NewEncryptedValue: %v", err)
	}
	s := NewStoreWithEnc(db, enc)

	if err := s.MarkSeenPersisted("old", time.Now().Add(-72*time.Hour)); err != nil {
		t.Fatalf("MarkSeenPersisted: %v", err)
	}
	if err := s.MarkSeenPersisted("fresh", time.Now()); err != nil {
		t.Fatalf("MarkSeenPersisted: %v", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	deleted, err := s.PruneSeen(cutoff)
	if err != nil {
		t.Fatalf("PruneSeen: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PruneSeen deleted %d, want 1", deleted)
	}
	if s.IsSeenPersisted("old") {
		t.Error("old entry should be gone")
	}
	if !s.IsSeenPersisted("fresh") {
		t.Error("fresh entry should remain")
	}
}

// TestPruneSeen_EmptyBuckets verifies nil-safe behavior on a fresh DB
// where the targets bucket may not exist yet. / TestPruneSeen_Empty
// Buckets 验证新建 DB（targets bucket 可能尚不存在）时的 nil 安全。
func TestPruneSeen_EmptyBuckets(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	n, err := s.CountSeenBefore(time.Now())
	if err != nil || n != 0 {
		t.Fatalf("CountSeenBefore on fresh DB = %d, %v; want 0, nil", n, err)
	}
	deleted, err := s.PruneSeen(time.Now())
	if err != nil || deleted != 0 {
		t.Fatalf("PruneSeen on fresh DB = %d, %v; want 0, nil", deleted, err)
	}
}
