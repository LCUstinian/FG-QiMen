// Package workspace provides the project workspace abstraction that
// supports both ephemeral (oneshot) and persistent (project) modes
// through a single Project struct.
//
// Package workspace 提供支持即扫即走和增量扫描两种工作模式的项目工作区抽象，
// 两种模式通过统一的 Project 结构处理。
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/LCUstinian/FG-QiMen/common"
)

// Mode distinguishes the two work modes.
// Mode 区分两种工作模式。
type Mode int

const (
	// ModeEphemeral is the oneshot mode: current directory, no bbolt,
	// results in-place. Triggered by -p "" (default).
	// ModeEphemeral 是即扫即走模式：当前目录，无 bbolt，结果就地写出。
	// 由 -p "" 触发（默认行为）。
	ModeEphemeral Mode = iota
	// ModePersistent is the project mode: ./projects/<name>/ with
	// bbolt state, hash dedup, resume support. Triggered by -p <name>.
	// ModePersistent 是增量扫描模式：./projects/<name>/，含 bbolt 状态、
	// hash 去重、断点续传。由 -p <name> 触发。
	ModePersistent
)

// Project is the active workspace. It owns file handles and the bbolt DB
// (if any). Callers must defer proj.Close().
//
// Project 是当前激活的工作区。它持有文件句柄和 bbolt DB（如有）。
// 调用方必须 defer proj.Close()。
type Project struct {
	Name string
	Mode Mode
	Root string
	DB   *bolt.DB
	// DBPath is the bbolt file path (for projects info display).
	// DBPath 是 bbolt 文件路径（供 projects info 显示）。
	DBPath string
}

// Open creates a project workspace in the requested mode.
// name == "" → ModeEphemeral, ./projects/<name> → ModePersistent.
//
// Open 创建请求模式下的项目工作区。name == "" → 即扫即走，否则 → 增量扫描。
func Open(name string) (*Project, error) {
	if name == "" {
		return openEphemeral()
	}
	return openPersistent(name)
}

// openEphemeral constructs an ephemeral project: no DB, root = current
// working directory.
// openEphemeral 构造即扫即走项目：无 DB，根目录 = 当前工作目录。
func openEphemeral() (*Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	return &Project{
		Name: "",
		Mode: ModeEphemeral,
		Root: cwd,
	}, nil
}

// openPersistent creates ./projects/<name>/ if missing, opens bbolt at
// ./projects/<name>/fg.db, and returns the project.
// openPersistent 创建 ./projects/<name>/（如缺失），在 ./projects/<name>/fg.db
// 打开 bbolt，并返回 project。
func openPersistent(name string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("persistent project requires non-empty name")
	}
	dir := filepath.Join("runs", "projects", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	dbPath := filepath.Join(dir, "fg.db")
	db, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt %s: %w", dbPath, err)
	}
	// Ensure required buckets exist. / 确保必需的 bucket 存在。
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{[]byte("targets"), []byte("results"), []byte("creds")} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Project{
		Name:   name,
		Mode:   ModePersistent,
		Root:   dir,
		DB:     db,
		DBPath: dbPath,
	}, nil
}

// Close releases the bbolt DB (if any). Always safe to call.
// Close 释放 bbolt DB（如有）。任何时候调用都安全。
func (p *Project) Close() error {
	if p == nil || p.DB == nil {
		return nil
	}
	return p.DB.Close()
}

// Stats returns human-readable statistics about the project.
// Stats 返回项目的可读统计信息。
func (p *Project) Stats() (string, error) {
	if p == nil || p.Mode == ModeEphemeral {
		return "(ephemeral: no persistent state)", nil
	}
	if p.DB == nil {
		return "", nil
	}
	var t, r, c int
	err := p.DB.View(func(tx *bolt.Tx) error {
		for _, b := range []string{"targets", "results", "creds"} {
			bk := tx.Bucket([]byte(b))
			if bk == nil {
				continue
			}
			n := bk.Stats().KeyN
			switch b {
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

// AsStore wraps p.DB into a common.Store (for incremental state).
// AsStore 把 p.DB 包装为 common.Store（用于增量状态）。
func (p *Project) AsStore() *common.Store {
	if p == nil || p.DB == nil {
		return nil
	}
	return common.NewStore(p.DB)
}
