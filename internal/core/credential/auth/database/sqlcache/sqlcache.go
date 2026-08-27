// Package sqlcache: shared *sql.DB LRU cache for SQL authenticators.
// Phase 1.9 (audit roadmap): the legacy code path opened +
// closed a fresh *sql.DB for every credential attempt, which
// made each attempt cost a full TCP+auth handshake. The cache
// keys by (driver, host, port, user, pass) and reuses pools
// so consecutive attempts on the same DB hit a warm pool.
//
// sqlcache 包：SQL 认证器共享的 *sql.DB LRU 缓存。审计路线图
// Phase 1.9：旧代码每个凭据尝试都开+关一个新 *sql.DB，每次
// 都要完整 TCP+认证握手。缓存以 (driver, host, port, user,
// pass) 为 key，复用池让同一 DB 的连续尝试走暖池。
//
// HARD-rule compliance: we only return *sql.DB handles; we
// never use them to execute queries. The handle is for the
// Ping-equivalent (driver handshake + auth) that proves the
// credential. / HARD 规则合规：只返 *sql.DB 句柄；不执行
// 任何查询。句柄只用于 Ping 等价（驱动握手+认证）的凭据
// 验证。
package sqlcache

import (
	"container/list"
	"database/sql"
	"sync"
)

// Global is the process-wide cache. SQL authenticators share
// this single instance. / Global 是进程级缓存。SQL 认证器共享
// 这一个实例。
var Global = NewCache(256)

// Cache is an LRU cache of *sql.DB handles keyed by a string.
// Safe for concurrent use. / Cache 是以 string 为 key 的 *sql.DB
// LRU 缓存。并发安全。
type Cache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List               // front = most recent, back = oldest
	m        map[string]*list.Element // key → *list.Element holding *entry
}

type entry struct {
	key  string
	db   *sql.DB
	hits int
}

// NewCache returns a Cache that holds at most maxEntries entries;
// older entries are evicted when the cache is full. / NewCache 返回容
// 量 maxEntries 的 Cache；满了就淘汰最老。
func NewCache(maxEntries int) *Cache {
	if maxEntries <= 0 {
		maxEntries = 64
	}
	return &Cache{
		capacity: maxEntries,
		ll:       list.New(),
		m:        make(map[string]*list.Element, maxEntries),
	}
}

// GetOrCreate returns a cached *sql.DB for key, or runs open() and
// caches the result. The second return is true if the entry was
// newly created (caller should set its pool config). / GetOrCreate
// 返 key 对应的缓存 *sql.DB，或跑 open() 并缓存。第二返是 true
// 表示新建（调用方应设 pool config）。
func (c *Cache) GetOrCreate(key string, open func() (*sql.DB, error)) (*sql.DB, bool, error) {
	c.mu.Lock()
	if e, ok := c.m[key]; ok {
		c.ll.MoveToFront(e)
		ent := e.Value.(*entry)
		ent.hits++
		c.mu.Unlock()
		return ent.db, false, nil
	}
	c.mu.Unlock()

	// Open outside the lock — opening can take a while. / 解锁后
	// 开——open 可能慢。
	db, err := open()
	if err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check: another goroutine may have inserted. / 双检：
	// 其他 goroutine 可能已插。
	if e, ok := c.m[key]; ok {
		// We have a winner; close our redundant open. / 已
		// 有胜出者；关掉我们冗余的 open。
		_ = db.Close()
		c.ll.MoveToFront(e)
		return e.Value.(*entry).db, false, nil
	}
	if c.ll.Len() >= c.capacity {
		// Evict oldest. / 淘汰最老。
		oldest := c.ll.Back()
		if oldest != nil {
			ent := oldest.Value.(*entry)
			c.ll.Remove(oldest)
			delete(c.m, ent.key)
			_ = ent.db.Close()
		}
	}
	e := c.ll.PushFront(&entry{key: key, db: db})
	c.m[key] = e
	return db, true, nil
}

// Invalidate removes a single entry (e.g. after Ping failure with
// a stale connection). / Invalidate 移除一条目（如 Ping 失败后
// 移除陈连接）。
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		ent := e.Value.(*entry)
		c.ll.Remove(e)
		delete(c.m, ent.key)
		_ = ent.db.Close()
	}
}

// Close empties the cache and closes all connections. / Close 清
// 空缓存并关闭所有连接。
func (c *Cache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.m {
		_ = e.Value.(*entry).db.Close()
	}
	c.ll.Init()
	c.m = make(map[string]*list.Element, c.capacity)
}

// Len returns the current entry count. / Len 返当前条目数。
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
