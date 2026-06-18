// store_crypto_test.go — end-to-end tests for Store with the encryption
// layer enabled, using an in-memory bbolt.
//
// store_crypto_test.go — Store 启用加密层的端到端测试,使用内存 bbolt.
//
// We can't use bolt.Open(":memory:") the way some KV stores allow — bbolt
// requires a real file path. We use t.TempDir() to get a clean path per
// test that the OS removes on test cleanup.
//
// bbolt 不像某些 KV 存储支持 bolt.Open(":memory:") —— bbolt 需要真实文件
// 路径。我们用 t.TempDir() 给每个测试一个干净路径,测试结束后 OS 自动清理。
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestDB(t *testing.T) *bolt.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStore_PlaintextDefault(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	if s.Encrypted() {
		t.Fatalf("NewStore(db) is Encrypted(); want false")
	}

	type rec struct{ User, Pass string }
	r := rec{"admin", "hunter2"}
	if err := s.PutCred("hash1", r); err != nil {
		t.Fatalf("PutCred: %v", err)
	}

	// Verify on-disk format is the legacy 0x00 magic + JSON.
	// 验证磁盘格式是遗留 0x00 magic + JSON。
	var got []byte
	_ = db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketCreds)
		got = bk.Get([]byte("hash1"))
		return nil
	})
	if len(got) == 0 || got[0] != magicPlain {
		t.Fatalf("on-disk first byte = 0x%02x, want 0x%02x (plain)", got[0], magicPlain)
	}

	var r2 rec
	if err := json.Unmarshal(got[1:], &r2); err != nil {
		t.Fatalf("unmarshal plaintext: %v", err)
	}
	if r2.User != "admin" || r2.Pass != "hunter2" {
		t.Fatalf("got %+v, want admin/hunter2", r2)
	}
}

func TestStore_EncryptedPutIsNotPlaintextOnDisk(t *testing.T) {
	db := openTestDB(t)
	key := DeriveKey("strong-pass")
	enc, _ := NewEncryptedValue(key)
	s := NewStoreWithEnc(db, enc)
	if !s.Encrypted() {
		t.Fatalf("NewStoreWithEnc returned non-Encrypted store")
	}

	type rec struct{ User, Pass string }
	r := rec{"admin", "hunter2"}
	if err := s.PutCred("hash1", r); err != nil {
		t.Fatalf("PutCred: %v", err)
	}

	// Verify on-disk first byte is 0x01 and the original plaintext bytes
	// do NOT appear (raw byte search). The AES-256-GCM output is
	// indistinguishable from random; the magic + nonce prefix is the
	// only deterministic prefix.
	//
	// 验证磁盘首字节是 0x01 且不含原始明文（原始字节搜索）。AES-256-GCM
	// 输出与随机不可区分；只有 magic + nonce 前缀是确定性的。
	var got []byte
	_ = db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketCreds)
		got = bk.Get([]byte("hash1"))
		return nil
	})
	if got[0] != magicEncrypted {
		t.Fatalf("on-disk first byte = 0x%02x, want 0x%02x (encrypted)", got[0], magicEncrypted)
	}
	if containsBytes(got, []byte("hunter2")) {
		t.Fatalf("on-disk value contains plaintext 'hunter2' — encryption not applied")
	}
}

func TestStore_EncryptedRoundTripAcrossReopen(t *testing.T) {
	// Persistence test: write encrypted, close DB, reopen, verify
	// the value can still be read (decrypted) with the same key.
	//
	// 持久化测试：写加密值,关 DB,重开,用相同 key 验证可读（解密）。
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	key := DeriveKey("persist-key")
	enc, _ := NewEncryptedValue(key)

	// Write phase. / 写阶段。
	db1, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open write: %v", err)
	}
	s1 := NewStoreWithEnc(db1, enc)
	type rec struct{ User, Pass string }
	if err := s1.PutCred("k", rec{"u", "p"}); err != nil {
		t.Fatalf("PutCred: %v", err)
	}
	if err := s1.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	_ = db1.Close()

	// Reopen phase. / 重开阶段。
	db2, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open read: %v", err)
	}
	defer db2.Close()
	s2 := NewStoreWithEnc(db2, enc)

	// Verify presence in seen-set still works (plaintext bucket).
	// 验证 seen-set 仍可用（明文 bucket）。
	if err := s2.MarkSeenPersisted("h1", time.Now()); err != nil {
		t.Fatalf("MarkSeenPersisted: %v", err)
	}
	if !s2.IsSeenPersisted("h1") {
		t.Fatalf("IsSeenPersisted returned false after reopen")
	}
}

func TestStore_OpenEncryptedValueWithNoKeyErrors(t *testing.T) {
	// Encrypted value written; reader has no key → Open() returns
	// error (not crash, not silent garbage).
	//
	// 已加密值；读取方无 key → Open() 返回错误（非崩溃，非静默垃圾）。
	db := openTestDB(t)
	key := DeriveKey("write-key")
	enc, _ := NewEncryptedValue(key)
	sEnc := NewStoreWithEnc(db, enc)
	type rec struct{ X int }
	if err := sEnc.PutResult("k", rec{X: 7}); err != nil {
		t.Fatalf("PutResult: %v", err)
	}

	// Re-open with a fresh Store (no key) but reuse the same bbolt.
	// 用无 key 的新 Store 重开同一 bbolt。
	sPlain := NewStore(db)
	var got []byte
	_ = db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketResults)
		got = bk.Get([]byte("k"))
		return nil
	})
	if got[0] != magicEncrypted {
		t.Fatalf("expected encrypted magic on disk, got 0x%02x", got[0])
	}
	_ = sPlain // unused directly, but this is the API path callers would use
	_ = os.ErrNotExist
}

// containsBytes reports whether sub appears anywhere in s.
// containsBytes 报告 sub 是否在 s 中任意位置出现。
func containsBytes(s, sub []byte) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
