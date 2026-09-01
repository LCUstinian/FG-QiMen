// store.go — bbolt-backed persistence for scan schedules.
//
// v0.5: scheduled scans can be persisted to the project DB
// (the `schedules` bucket, created lazily) so a long-running
// deployment can list pending schedules + their next-fire
// times. The CLI exposes this via 'fg-qimen schedules add |
// list | remove'. / v0.5：定时扫描可持久化到项目 DB（`schedules`
// bucket，懒创建），让长期部署能列挂起的调度 + 下次 fire 时间。
// CLI 通过 'fg-qimen schedules add | list | remove' 暴露。
//
// The on-disk format is one JSON-encoded value per key
// (the schedule name is the key). Cron expressions are stored
// verbatim (string) and re-parsed on load — no need to
// serialize the parsed robfig Schedule. / 磁盘格式是每个 key
// 一条 JSON 编码 value（key 是调度名）。cron 表达式原样存
// （字符串），load 时重 parse——不需要序列化已 parse 的
// robfig Schedule。
package scheduler

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Store wraps a *bbolt.DB for schedule persistence. The caller
// owns the DB lifecycle (open/close); Store itself is a thin
// façade. / Store 给 *bbolt.DB 包一层调度持久化。调用方拥有
// DB 生命周期（open/close）；Store 本身是个薄 façade。
type Store struct {
	db *bolt.DB
}

// NewStore returns a Store bound to db. / NewStore 返绑 db 的
// Store。
func NewStore(db *bolt.DB) *Store { return &Store{db: db} }

// schedulesBucket is the bbolt bucket name. We use a const so
// tests + production code share the same string. / schedulesBucket
// 是 bbolt bucket 名。用 const 让测试 + 生产代码共享同一字符
// 串。
const schedulesBucket = "schedules"

// Record is one persisted schedule. / Record 是一条持久化的
// 调度。
type Record struct {
	// Name is the schedule's identifier (also the bbolt key).
	// / Name 是调度标识（也是 bbolt key）。
	Name string `json:"name"`
	// Project is the project this schedule is attached to.
	// / Project 是调度所属的项目。
	Project string `json:"project"`
	// Mode is "at" / "in" / "cron". / Mode 是 "at"/"in"/"cron"。
	Mode string `json:"mode"`
	// Value is the raw flag value (RFC3339 / Go duration / cron
	// expr). / Value 是原始 flag 值（RFC3339 / Go duration / cron
	// 表达式）。
	Value string `json:"value"`
	// TZ is the IANA zone (empty = local). / TZ 是 IANA 时区
	// （空 = 本地）。
	TZ string `json:"tz"`
	// Daemon mirrors the --daemon flag. / Daemon 对应 --daemon
	// flag。
	Daemon bool `json:"daemon"`
	// CreatedAt is when the schedule was added. / CreatedAt
	// 是调度被添加的时间。
	CreatedAt time.Time `json:"created_at"`
	// LastRun is the last time the schedule actually fired
	// (zero if never). / LastRun 是调度上次实际 fire 的时间
	// （从未 fire 则为零值）。
	LastRun time.Time `json:"last_run,omitzero"`
}

// ensureBucket creates the schedules bucket if missing. /
// ensureBucket 在缺失时创建 schedules bucket。
func (s *Store) ensureBucket(tx *bolt.Tx) error {
	_, err := tx.CreateBucketIfNotExists([]byte(schedulesBucket))
	return err
}

// Add inserts (or replaces) a schedule. If a record with the
// same name already exists, its CreatedAt is preserved (only
// operator-set fields are updated). / Add 插入（或替换）一条
// 调度。如果同名记录已存在，其 CreatedAt 保留（只更新操作
// 员设置的字段）。
func (s *Store) Add(rec Record) error {
	if rec.Name == "" {
		return fmt.Errorf("scheduler: record name is required")
	}
	if rec.CreatedAt.IsZero() {
		// Default the new record's CreatedAt to now, but if a
		// record with this name already exists, preserve the
		// original CreatedAt (only operator-set fields change on
		// overwrite). / 默认新记录的 CreatedAt 为 now，但如果同
		// 名记录已存在则保留原 CreatedAt（只覆盖操作员设置的
		// 字段）。
		now := time.Now()
		if existing, _ := s.Get(rec.Name); existing != nil {
			rec.CreatedAt = existing.CreatedAt
		} else {
			rec.CreatedAt = now
		}
	}
	data, err := json.Marshal(&rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := s.ensureBucket(tx); err != nil {
			return err
		}
		return tx.Bucket([]byte(schedulesBucket)).Put([]byte(rec.Name), data)
	})
}

// Get returns a single schedule by name, or (nil, nil) if not
// found. / Get 按 name 返单条调度；找不到返 (nil, nil)。
func (s *Store) Get(name string) (*Record, error) {
	var rec *Record
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(schedulesBucket))
		if b == nil {
			return nil
		}
		data := b.Get([]byte(name))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &rec)
	})
	return rec, err
}

// List returns all schedules in the DB, ordered by name. / List
// 返 DB 所有调度，按 name 排序。
func (s *Store) List() ([]Record, error) {
	var out []Record
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(schedulesBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var r Record
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			out = append(out, r)
			return nil
		})
	})
	return out, err
}

// Remove deletes a schedule by name. Returns nil whether the
// schedule existed or not (idempotent). / Remove 按 name 删调度。
// 不管调度存不存在都返 nil（幂等）。
func (s *Store) Remove(name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(schedulesBucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(name))
	})
}
