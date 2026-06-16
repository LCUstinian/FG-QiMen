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

	"github.com/LCUstinian/FG-QiMen/internal/store"
)

// Project is the active workspace. It owns file handles and the bbolt DB
// (if any). Callers must defer proj.Close().
//
// The two workspace shapes (ephemeral vs persistent) are distinguished
// by the empty Name: name == "" → ephemeral (no DB, results in cwd),
// name != "" → persistent (bbolt in runs/projects/<name>). We don't
// keep a separate Mode enum on the struct — it's redundant with the
// Name check and the only consumer of the audit's v0.2-flagged Mode
// was the Stats() helper, which now reads Name directly.
//
// Project 是当前激活的工作区。它持有文件句柄和 bbolt DB（如有）。
// 调用方必须 defer proj.Close()。
//
// 两种工作区形态（即扫即走 vs 增量）通过空 Name 区分：name=="" → 即扫
// 即走（无 DB，结果在 cwd）；name!="" → 增量（bbolt 在 runs/projects/
// <name>）。不再在结构体上保留独立的 Mode enum——和 Name 检查重复，
// v0.2 审计时 Mode 唯一消费者是 Stats()，现在 Stats() 直接读 Name。
type Project struct {
	Name string
	Root string
	DB   *bolt.DB
	// DBPath is the bbolt file path (for projects info display).
	// DBPath 是 bbolt 文件路径（供 projects info 显示）。
	DBPath string
}

// Open creates a project workspace.
// name == "" → ephemeral; name != "" → persistent.
//
// Open 创建项目工作区。name=="" → 即扫即走；name!="" → 增量。
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
		Root: cwd,
	}, nil
}

// openPersistent creates ./runs/projects/<name>/ if missing, opens bbolt
// at ./runs/projects/<name>/fg.db, and returns the project.
// openPersistent 创建 ./runs/projects/<name>/（如缺失），在
// ./runs/projects/<name>/fg.db 打开 bbolt，并返回 project。
func openPersistent(name string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("persistent project requires non-empty name")
	}
	dir := filepath.Join(ProjectsRoot(), name)
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
	if p == nil || p.Name == "" {
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

// AsStore wraps p.DB into a store.Store (for incremental state).
// AsStore 把 p.DB 包装为 store.Store（用于增量状态）。
func (p *Project) AsStore() *store.Store {
	if p == nil || p.DB == nil {
		return nil
	}
	return store.NewStore(p.DB)
}

// ProjectsRoot returns the directory under which persistent projects
// live. It is a single source of truth shared by Open / List / Delete
// so that all three agree on the on-disk layout.
//
// ProjectsRoot 返回持久化项目所在的根目录。Open / List / Delete 共享
// 该函数，保证三者对磁盘布局的认知一致。
func ProjectsRoot() string {
	return filepath.Join("runs", "projects")
}

// List returns the names of all persistent project directories that
// currently exist under ProjectsRoot. Missing root → empty list (not
// an error: a fresh checkout has no projects yet).
//
// List 返回 ProjectsRoot 下当前存在的所有持久化项目名。根目录不存在 →
// 返回空列表（不算错误：全新 checkout 还没有任何项目）。
func List() ([]string, error) {
	root := ProjectsRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// Delete removes a persistent project directory and all its contents
// (bbolt DB, results, creds). Refuses to operate on ephemeral mode
// (name == "") to prevent accidentally rm -rf of the cwd.
//
// Delete 删除一个持久化项目目录及其所有内容（bbolt DB、结果、凭据）。
// 拒绝在即扫即走模式（name == ""）下操作，避免误删当前工作目录。
func Delete(name string) error {
	if name == "" {
		return fmt.Errorf("refuse to delete ephemeral project (empty name)")
	}
	dir := filepath.Join(ProjectsRoot(), name)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a project directory", dir)
	}
	return os.RemoveAll(dir)
}
