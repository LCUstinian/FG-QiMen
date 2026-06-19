// batch_test.go — unit tests for the batched-write path.
//
// batch_test.go — 批量写路径的单元测试。
//
// We can't directly observe "fsyncs per second" in a unit test, so we
// verify the behavioural contract:
//   - Enqueue + Flush persist everything
//   - The count threshold triggers an automatic flush
//   - The time-based ticker triggers a flush
//   - Stop() blocks until the final flush completes
//   - PutOpSeen writes timestamp bytes (not JSON)
//
// 我们没法在单测里直接观察"每秒 fsync 数"，所以验证行为契约：
//   - Enqueue + Flush 持久化一切
//   - 数量阈值触发自动刷盘
//   - 时间 tick 触发刷盘
//   - Stop() 阻塞直到最后一次刷盘完成
//   - PutOpSeen 写时间戳字节（非 JSON）
package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// openTestStore opens a temporary bbolt DB for tests. / openTestStore
// 为测试打开一个临时 bbolt DB。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db)
}

// TestBatchWriter_FlushOnEnqueue verifies the count-threshold triggers
// a synchronous flush. / TestBatchWriter_FlushOnEnqueue 验证数量阈值
// 触发同步刷盘。
func TestBatchWriter_FlushOnEnqueue(t *testing.T) {
	s := openTestStore(t)
	bw := NewBatchWriter(s, 4, 1*time.Hour) // huge interval; rely on count
	defer bw.Stop()

	for i := 0; i < 4; i++ {
		bw.Enqueue(PutOp{
			Kind:  PutOpResult,
			Hash:  "h-" + string(rune('a'+i)),
			Value: map[string]any{"i": i},
		})
	}

	// After enqueueing 4 ops with size=4, the count threshold should
	// have triggered a flush. / 投入 4 个 op（size=4），数量阈值
	// 应已触发刷盘。
	if got := bw.Pending(); got != 0 {
		t.Errorf("Pending() = %d, want 0 after count-threshold flush", got)
	}

	// Verify the data is in the DB. / 验证数据在 DB 中。
	got := 0
	_ = s.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketResults)
		if bk == nil {
			return nil
		}
		return bk.ForEach(func(_, _ []byte) error { got++; return nil })
	})
	if got != 4 {
		t.Errorf("results bucket count = %d, want 4", got)
	}
}

// TestBatchWriter_FlushOnTicker verifies the time-based flush.
// / TestBatchWriter_FlushOnTicker 验证基于时间的刷盘。
func TestBatchWriter_FlushOnTicker(t *testing.T) {
	s := openTestStore(t)
	bw := NewBatchWriter(s, 1000, 50*time.Millisecond) // huge count; rely on ticker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bw.Run(ctx)
	}()

	// Enqueue fewer than the count threshold, then wait for the
	// ticker. / 投入少于数量阈值的 op，然后等 tick。
	for i := 0; i < 5; i++ {
		bw.Enqueue(PutOp{Kind: PutOpSeen, Hash: "s" + string(rune('a'+i))})
	}
	time.Sleep(150 * time.Millisecond)

	if got := bw.Pending(); got != 0 {
		t.Errorf("Pending() = %d, want 0 after ticker flush", got)
	}
	cancel()
	wg.Wait()
}

// TestBatchWriter_StopBlocksUntilFlush verifies Stop() waits for the
// final flush. / TestBatchWriter_StopBlocksUntilFlush 验证 Stop() 等
// 待最后一次刷盘。
func TestBatchWriter_StopBlocksUntilFlush(t *testing.T) {
	s := openTestStore(t)
	bw := NewBatchWriter(s, 1000, 1*time.Hour) // never auto-flush on ticker

	// Run() must be in a goroutine — the test main needs to keep
	// going to call Stop() and check the result. / Run() 必须在
	// goroutine 里跑——测试主线程要继续走才能调 Stop() 并检查
	// 结果。
	var runDone sync.WaitGroup
	runDone.Add(1)
	go func() {
		defer runDone.Done()
		bw.Run(context.Background())
	}()

	// Give Run() time to start its select loop. / 给 Run() 时间
	// 进入 select 循环。
	time.Sleep(20 * time.Millisecond)

	// Enqueue 3 ops then call Stop(). Stop() should wait for the
	// final flush before returning. / 入队 3 个 op 然后调 Stop()。
	// Stop() 应在返回前等待最后一次刷盘。
	for i := 0; i < 3; i++ {
		bw.Enqueue(PutOp{Kind: PutOpResult, Hash: "stop-" + string(rune('a'+i)), Value: map[string]int{"x": i}})
	}
	stopReturned := make(chan struct{})
	go func() {
		bw.Stop()
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
		// good — Stop returned
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s")
	}
	runDone.Wait()

	// Verify the 3 enqueued ops were persisted. / 验证 3 个入队
	// 的 op 已被持久化。
	got := 0
	_ = s.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketResults)
		if bk == nil {
			return nil
		}
		return bk.ForEach(func(_, _ []byte) error { got++; return nil })
	})
	if got != 3 {
		t.Errorf("results bucket count = %d, want 3", got)
	}
}

// TestBatchWriter_PutOpSeenWritesTimestamp verifies PutOpSeen values
// are time.RFC3339Nano strings (not JSON). / TestBatchWriter_PutOpSeenWritesTimestamp
// 验证 PutOpSeen 值是 time.RFC3339Nano 字符串（非 JSON）。
func TestBatchWriter_PutOpSeenWritesTimestamp(t *testing.T) {
	s := openTestStore(t)
	bw := NewBatchWriter(s, 1, 1*time.Hour) // size=1 forces immediate flush
	defer bw.Stop()

	bw.Enqueue(PutOp{Kind: PutOpSeen, Hash: "seen-1"})
	bw.Flush() // safety

	var raw []byte
	_ = s.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketTargets)
		if bk == nil {
			return nil
		}
		raw = bk.Get([]byte("seen-1"))
		return nil
	})
	if len(raw) == 0 {
		t.Fatal("seen-1 not persisted")
	}
	// The value should NOT be JSON (i.e. must not start with '{').
	// It should be parseable as time.RFC3339Nano. / 值不应是
	// JSON（即不应以 '{' 开头）。应能被 time.RFC3339Nano 解析。
	if _, err := time.Parse(time.RFC3339Nano, string(raw)); err != nil {
		t.Errorf("seen value %q is not RFC3339Nano: %v", raw, err)
	}
	// And explicitly: it should not be valid JSON. / 明确说：不
	// 应该是合法 JSON。
	if json.Valid(raw) {
		t.Errorf("seen value %q unexpectedly parses as JSON", raw)
	}
}

// TestPutMany_PreservesEncryption verifies PutMany applies the
// encryption layer (if configured) to every op. / TestPutMany_PreservesEncryption
// 验证 PutMany 对每个 op 应用加密层（如果配置了）。
func TestPutMany_PreservesEncryption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enc.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	defer db.Close()
	enc, err := NewEncryptedValue(DeriveKey("batch-enc-test"))
	if err != nil {
		t.Fatalf("NewEncryptedValue: %v", err)
	}
	s := NewStoreWithEnc(db, enc)

	if err := s.PutMany([]PutOp{
		{Kind: PutOpResult, Hash: "r1", Value: map[string]string{"k": "v"}},
		{Kind: PutOpCred, Hash: "c1", Value: map[string]string{"u": "admin"}},
		{Kind: PutOpSeen, Hash: "s1"},
	}); err != nil {
		t.Fatalf("PutMany: %v", err)
	}

	// The result row on disk should start with magicEncrypted (0x02).
	// / 磁盘上的 result 行应以 magicEncrypted (0x02) 开头。
	_ = db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketResults)
		if bk == nil {
			t.Fatal("results bucket missing")
		}
		raw := bk.Get([]byte("r1"))
		if len(raw) == 0 || raw[0] != magicEncrypted {
			t.Errorf("results row magic = 0x%02x, want 0x%02x", raw[0], magicEncrypted)
		}
		// Roundtrip via Open. / 经 Open 往返。
		opened, err := enc.Open(raw)
		if err != nil {
			t.Errorf("Open: %v", err)
		}
		if !json.Valid(opened) {
			t.Errorf("opened value not valid JSON: %q", opened)
		}
		return nil
	})
}
