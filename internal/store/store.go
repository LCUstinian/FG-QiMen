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
package store

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
//
// When enc is non-nil, PutResult / PutCred encrypt their JSON payloads
// with AES-256-GCM (see crypto.go). The seen-set bucket is always
// plaintext (it stores only non-secret hashes). When enc is nil, all
// writes are plaintext (v0.2.x on-disk format) so legacy DBs remain
// readable without any migration step.
//
// 当 enc 非 nil 时,PutResult / PutCred 用 AES-256-GCM 加密 JSON 负载
// (见 crypto.go)。seen-set bucket 始终明文(只存非机密 hash)。
// enc 为 nil 时所有写入为明文(v0.2.x 磁盘格式),旧 DB 无需迁移即可读取。
type Store struct {
	db  *bolt.DB
	enc *EncryptedValue
}

// NewStore wraps an existing *bolt.DB. Returns nil when db is nil.
// NewStore 包装一个现有 *bolt.DB。db 为 nil 时返回 nil。
func NewStore(db *bolt.DB) *Store {
	return NewStoreWithEnc(db, nil)
}

// NewStoreWithEnc wraps an existing *bolt.DB and an optional encryption
// layer. Pass nil enc to disable encryption (v0.2.x behavior).
//
// NewStoreWithEnc 包装现有 *bolt.DB 和可选加密层。
// 传 nil enc 禁用加密(v0.2.x 行为)。
func NewStoreWithEnc(db *bolt.DB, enc *EncryptedValue) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db, enc: enc}
}

// Encrypted reports whether the store encrypts PutResult/PutCred values.
// Encrypted 报告 store 是否加密 PutResult/PutCred 值。
func (s *Store) Encrypted() bool {
	return s != nil && s.enc != nil
}

// MarkSeenPersisted persists a "seen" hash to the targets bucket so
// -resume can pick it up on the next run.
//
// M4 audit fix: use CreateBucketIfNotExists instead of Bucket to avoid
// nil pointer dereference panic if the bucket is missing.
//
// MarkSeenPersisted 把"已见"hash 持久化到 targets bucket，下次 -resume 时可恢复。
//
// M4 审计修法：用 CreateBucketIfNotExists 替代 Bucket，避免 bucket 缺失时
// nil 指针解引用 panic。
func (s *Store) MarkSeenPersisted(hash string, when time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists(bucketTargets)
		if err != nil {
			return err
		}
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
		// Pre-allocate the slice using the bucket's KeyN stat so
		// ForEach doesn't trigger ~log2(N) reallocs. For a 100k-
		// hash resume this is the difference between 17 allocations
		// and 1. / 用 bucket 的 KeyN 统计预分配 slice，让 ForEach 不
		// 触发 ~log2(N) 次重分配。10 万 hash 的 resume 场景下，这是
		// 17 次分配和 1 次分配的区别。
		out = make([]string, 0, bk.Stats().KeyN)
		return bk.ForEach(func(k, _ []byte) error {
			out = append(out, string(k))
			return nil
		})
	})
	return out, err
}

// PutResult persists a structured result to the results bucket.
// PutResult 把结构化结果持久化到 results bucket。
//
// M4 audit fix: use CreateBucketIfNotExists to avoid nil panic.
// M4 审计修法：用 CreateBucketIfNotExists 避免 nil panic。
//
// When the store was constructed with NewStoreWithEnc(db, enc), the
// marshaled JSON is encrypted with AES-256-GCM before being written
// to bbolt. Plaintext writes remain the default when no key is set,
// so existing v0.2.x projects keep working without migration.
//
// 当 store 用 NewStoreWithEnc(db, enc) 构造时,JSON 在写入 bbolt 前
// 用 AES-256-GCM 加密。未设密钥时仍写明文,v0.2.x 项目无需迁移。
func (s *Store) PutResult(hash string, v any) error {
	if s == nil || s.db == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	stored, err := s.sealIfNeeded(data)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists(bucketResults)
		if err != nil {
			return err
		}
		return bk.Put([]byte(hash), stored)
	})
}

// PutOpKind identifies which bucket a PutOp targets. / PutOpKind 标识
// PutOp 写入的 bucket。
type PutOpKind int

const (
	// PutOpResult writes to the results bucket. / 写入 results bucket。
	PutOpResult PutOpKind = iota + 1
	// PutOpCred writes to the creds bucket. / 写入 creds bucket。
	PutOpCred
	// PutOpSeen writes to the targets (seen-hash) bucket. / 写入
	// targets（已见 hash）bucket。
	PutOpSeen
)

// PutOp is one element of a batched write. The Value is JSON-marshaled
// (and encrypted when the store has an encryption layer) at batch-flush
// time, not at construction time. / PutOp 是批量写入的一个元素。
// Value 在批量刷盘时（而非构造时）被 JSON 序列化（并按需加密）。
type PutOp struct {
	Kind  PutOpKind
	Hash  string
	Value any
}

// PutMany persists a batch of operations in a single bbolt transaction.
// This is significantly cheaper than calling PutResult / PutCred /
// MarkSeenPersisted in a loop because bbolt commits one fsync per
// Update(); at scan rates of ~50 results/sec the per-write pattern
// produces 50 fsyncs/sec on the DB file. PutMany amortises that to
// a single fsync per batch. / PutMany 在单次 bbolt 事务中批量写入。
// 这比循环调用 PutResult / PutCred / MarkSeenPersisted 便宜得多——
// bbolt 每次 Update() 触发一次 fsync；50 结果/秒的扫描速率下，
// per-write 模式每秒对 DB 文件 fsync 50 次。PutMany 把单次批量
// 摊销为 1 次 fsync。
//
// The on-disk format and encryption are unchanged — PutMany is a
// performance wrapper, not a format change. / 磁盘格式和加密不变
// —— PutMany 是性能包装，不是格式变更。
func (s *Store) PutMany(ops []PutOp) error {
	if s == nil || s.db == nil || len(ops) == 0 {
		return nil
	}
	// Pre-marshal + seal outside the transaction so JSON encoding
	// and AES-GCM work don't hold the bbolt write lock.
	// 在事务外预序列化+加密，避免 JSON 编码和 AES-GCM 工作持
	// bbolt 写锁。
	prepared := make([]PutOp, len(ops))
	for i, op := range ops {
		if op.Kind == PutOpSeen {
			// Seen writes are timestamp bytes, not JSON.
			// / Seen 写入是时间戳字节，不是 JSON。
			prepared[i] = PutOp{
				Kind:  op.Kind,
				Hash:  op.Hash,
				Value: []byte(time.Now().UTC().Format(time.RFC3339Nano)),
			}
			continue
		}
		data, err := json.Marshal(op.Value)
		if err != nil {
			return fmt.Errorf("store: marshal PutOp[%d]: %w", i, err)
		}
		stored, err := s.sealIfNeeded(data)
		if err != nil {
			return fmt.Errorf("store: seal PutOp[%d]: %w", i, err)
		}
		prepared[i] = PutOp{Kind: op.Kind, Hash: op.Hash, Value: stored}
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		// Resolve (or create) each bucket once, not per-op.
		// / 每种 bucket 只解析（或创建）一次。
		var resultsBk, credsBk, seenBk *bolt.Bucket
		getResults := func() (*bolt.Bucket, error) {
			if resultsBk != nil {
				return resultsBk, nil
			}
			bk, err := tx.CreateBucketIfNotExists(bucketResults)
			if err != nil {
				return nil, err
			}
			resultsBk = bk
			return bk, nil
		}
		getCreds := func() (*bolt.Bucket, error) {
			if credsBk != nil {
				return credsBk, nil
			}
			bk, err := tx.CreateBucketIfNotExists(bucketCreds)
			if err != nil {
				return nil, err
			}
			credsBk = bk
			return bk, nil
		}
		getSeen := func() (*bolt.Bucket, error) {
			if seenBk != nil {
				return seenBk, nil
			}
			bk, err := tx.CreateBucketIfNotExists(bucketTargets)
			if err != nil {
				return nil, err
			}
			seenBk = bk
			return bk, nil
		}
		for _, op := range prepared {
			var bk *bolt.Bucket
			switch op.Kind {
			case PutOpResult:
				var err error
				bk, err = getResults()
				if err != nil {
					return err
				}
			case PutOpCred:
				var err error
				bk, err = getCreds()
				if err != nil {
					return err
				}
			case PutOpSeen:
				var err error
				bk, err = getSeen()
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("store: unknown PutOpKind %d", op.Kind)
			}
			val, ok := op.Value.([]byte)
			if !ok {
				return fmt.Errorf("store: PutOp[%s] value not []byte", op.Hash)
			}
			if err := bk.Put([]byte(op.Hash), val); err != nil {
				return fmt.Errorf("store: put %s: %w", op.Hash, err)
			}
		}
		return nil
	})
}

// PutCred persists a credential hit to the creds bucket.
// PutCred 把凭据命中持久化到 creds bucket。
//
// M4 audit fix: use CreateBucketIfNotExists to avoid nil panic.
// M4 审计修法：用 CreateBucketIfNotExists 避免 nil panic。
//
// When encryption is configured, the marshaled JSON (which contains
// cleartext credentials) is encrypted at rest. This is the strongest
// reason to set FG_QIMEN_PROJECT_KEY — without it, an attacker who
// copies runs/projects/<name>/fg.db can read every password with a
// hex editor.
//
// 当启用加密时,序列化 JSON(含明文凭据)在落盘前加密。这是设置
// FG_QIMEN_PROJECT_KEY 的最强理由——不设则攻击者只需拷贝
// runs/projects/<name>/fg.db 即可用十六进制编辑器读出所有密码。
func (s *Store) PutCred(hash string, v any) error {
	if s == nil || s.db == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	stored, err := s.sealIfNeeded(data)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists(bucketCreds)
		if err != nil {
			return err
		}
		return bk.Put([]byte(hash), stored)
	})
}

// sealIfNeeded applies AES-256-GCM to plaintext when the store has an
// encryption layer; otherwise wraps with the 0x00 magic byte for forward
// compatibility with the Open() path.
//
// sealIfNeeded 在 store 有加密层时对明文施加 AES-256-GCM；否则用
// 0x00 magic 包装以便 Open() 正确处理。
func (s *Store) sealIfNeeded(plain []byte) ([]byte, error) {
	if s.enc == nil {
		return SealPlain(plain), nil
	}
	return s.enc.Seal(plain)
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
