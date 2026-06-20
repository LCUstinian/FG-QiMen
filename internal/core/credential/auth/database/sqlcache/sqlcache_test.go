// sqlcache_test.go — unit tests for the LRU cache.
package sqlcache

import (
	"database/sql"
	"database/sql/driver"
	"testing"
)

// noopDriver is a minimal driver.Conn stub. We use it so the
// cache test doesn't need a real MySQL/Postgres server. / noopDriver
// 是最小的 driver.Conn 桩。缓存测试用它避免需要真 MySQL/Postgres。
type noopDriver struct{}

func (d noopDriver) Open(_ string) (driver.Conn, error) { return noopConn{}, nil }

type noopConn struct{}

func (c noopConn) Prepare(_ string) (driver.Stmt, error) { return nil, nil }
func (c noopConn) Close() error                          { return nil }
func (c noopConn) Begin() (driver.Tx, error)            { return nil, nil }

func init() {
	sql.Register("noop", noopDriver{})
}

func newTestDB() *sql.DB {
	db, _ := sql.Open("noop", "")
	return db
}

func TestCache_NewAndGet(t *testing.T) {
	c := NewCache(4)
	defer c.Close()

	openCount := 0
	get := func() (*sql.DB, error) {
		openCount++
		return newTestDB(), nil
	}
	_, created, err := c.GetOrCreate("k1", get)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if !created {
		t.Error("expected created=true on first call")
	}
	if openCount != 1 {
		t.Errorf("open count = %d, want 1", openCount)
	}
	// Second call should NOT call open. / 第二次调用不应触发 open。
	_, created, err = c.GetOrCreate("k1", get)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if created {
		t.Error("expected created=false on cached call")
	}
	if openCount != 1 {
		t.Errorf("open count = %d, want 1 (cached)", openCount)
	}
	// Caller must NOT close the *sql.DB — cache owns it. / 调用
	// 方不能关 *sql.DB——缓存拥有。
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache(2) // cap=2
	defer c.Close()

	get := func() (*sql.DB, error) { return newTestDB(), nil }
	for _, k := range []string{"a", "b"} {
		_, _, _ = c.GetOrCreate(k, get)
	}
	if c.Len() != 2 {
		t.Errorf("len = %d, want 2", c.Len())
	}
	// Add 'c' — should evict 'a' (oldest). / 加 'c'——应淘汰 'a'。
	_, _, _ = c.GetOrCreate("c", get)
	if c.Len() != 2 {
		t.Errorf("len = %d, want 2 after eviction", c.Len())
	}
	// Touch 'b' so 'c' is now the oldest. Then add 'd' to evict 'c'. /
	// 访问 'b' 让 'c' 变最老。加 'd' 淘汰 'c'。
	_, _, _ = c.GetOrCreate("b", get)
	_, _, _ = c.GetOrCreate("d", get)
	if c.Len() != 2 {
		t.Errorf("len = %d after second eviction", c.Len())
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache(4)
	defer c.Close()

	get := func() (*sql.DB, error) { return newTestDB(), nil }
	_, _, _ = c.GetOrCreate("k", get)
	if c.Len() != 1 {
		t.Errorf("len before invalidate = %d, want 1", c.Len())
	}
	c.Invalidate("k")
	if c.Len() != 0 {
		t.Errorf("len after invalidate = %d, want 0", c.Len())
	}
}

func TestCache_Close(t *testing.T) {
	c := NewCache(4)
	get := func() (*sql.DB, error) { return newTestDB(), nil }
	for _, k := range []string{"a", "b", "c"} {
		_, _, _ = c.GetOrCreate(k, get)
	}
	if c.Len() != 3 {
		t.Errorf("len = %d, want 3", c.Len())
	}
	c.Close()
	if c.Len() != 0 {
		t.Errorf("len after Close = %d, want 0", c.Len())
	}
}
