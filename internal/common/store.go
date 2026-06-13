// store.go — bbolt-backed persistent state for incremental scans.
// store.go — bbolt 持久化状态，用于增量扫描。
//
// In ephemeral mode (no -p), this layer is skipped entirely — State uses
// only its in-memory sync.Map.
//
// 即扫即走模式（无 -p）下完全跳过本层——State 仅使用内存 sync.Map。
//
// Store is a thin wrapper over a *bolt.DB owned by workspace.Project;
// lifetime is managed by the project, not by the Store itself.
//
// Store 是对 workspace.Project 拥有的 *bolt.DB 的薄包装；生命周期由
// project 管理，不由 Store 自己管理。
package common

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Bucket names. / Bucket 名。
var (
	bucketTargets = []byte("targets")
	bucketResults = []byte("results")
	bucketCreds   = []byte("creds")
)

// Store wraps a bbolt database and exposes typed put/get helpers.
// Store 包装 bbolt 数据库，对外暴露类型化的 put/get 助手。
//
// The underlying *bolt.DB is NOT owned by Store; caller (workspace.Project)
// is responsible for opening/closing it. Callers should construct Store
// with NewStore() after Project.Open() and pass nil to disable persistence.
//
// 底层 *bolt.DB 不归 Store 所有；调用方（workspace.Project）负责开关。
// 应在 Project.Open() 之后用 NewStore() 构造 Store，传 nil 禁用持久化。
type Store struct {
	db *bolt.DB
}

// NewStore wraps an existing *bolt.DB. Returns nil when db is nil.
// NewStore 包装一个现有 *bolt.DB。db 为 nil 时返回 nil。
func NewStore(db *bolt.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// MarkSeenPersisted persists a "seen" hash to the targets bucket so
// -resume can pick it up on the next run.
//
// MarkSeenPersisted 把"已见"hash 持久化到 targets bucket，下次 -resume 时可恢复。
func (s *Store) MarkSeenPersisted(hash string, when time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketTargets)
		return bk.Put([]byte(hash), []byte(when.UTC().Format(time.RFC3339Nano)))
	})
}

// IsSeenPersisted reports whether a hash was previously persisted.
// IsSeenPersisted 报告某个 hash 是否此前被持久化。
func (s *Store) IsSeenPersisted(hash string) bool {
	if s == nil || s.db == nil {
		return false
	}
	var found bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketTargets)
		if bk == nil {
			return nil
		}
		found = bk.Get([]byte(hash)) != nil
		return nil
	})
	return found
}

// LoadSeenHashes returns all hashes from the targets bucket.
// LoadSeenHashes 返回 targets bucket 中的全部 hash。
func (s *Store) LoadSeenHashes() ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []string
	err := s.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketTargets)
		if bk == nil {
			return nil
		}
		return bk.ForEach(func(k, _ []byte) error {
			out = append(out, string(k))
			return nil
		})
	})
	return out, err
}

// PutResult persists a structured result to the results bucket.
// PutResult 把结构化结果持久化到 results bucket。
func (s *Store) PutResult(hash string, v any) error {
	if s == nil || s.db == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketResults)
		return bk.Put([]byte(hash), data)
	})
}

// PutCred persists a credential hit to the creds bucket.
// PutCred 把凭据命中持久化到 creds bucket。
func (s *Store) PutCred(hash string, v any) error {
	if s == nil || s.db == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketCreds)
		return bk.Put([]byte(hash), data)
	})
}

// Sync forces an fsync of the underlying bbolt mmap.
// Sync 强制将底层 bbolt mmap 写入磁盘。
func (s *Store) Sync() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Sync()
}

// Stats returns human-readable DB statistics for `projects info`.
// Stats 返回 `projects info` 用的可读 DB 统计信息。
func (s *Store) Stats() (string, error) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var t, r, c int
	err := s.db.View(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketTargets, bucketResults, bucketCreds} {
			bk := tx.Bucket(b)
			if bk == nil {
				continue
			}
			n := bk.Stats().KeyN
			switch string(b) {
			case "targets":
				t = n
			case "results":
				r = n
			case "creds":
				c = n
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("  seen hashes:  %d\n  results:      %d\n  creds:        %d", t, r, c), nil
}
