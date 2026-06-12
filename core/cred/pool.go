// Package cred: credential pool with dedup + loader.
// Package cred: 凭据池（去重 + 加载器）。
package cred

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Pool is a deduplicated, in-memory credential set.
// Pool 是去重后的内存凭据集合。
type Pool struct {
	creds []Cred
	// index by (user|pass|key|method) for fast dedup. / 按 (user|pass|key|method) 索引以快去重。
	index map[string]struct{}
}

// NewPool constructs an empty Pool. / NewPool 构造空 Pool。
func NewPool() *Pool {
	return &Pool{index: make(map[string]struct{})}
}

// Add inserts cred into the pool if it is not already present.
// Returns true if cred was added, false if it was a duplicate.
//
// Add 把 cred 插入池中（若不存在）。新增返回 true，重复返回 false。
func (p *Pool) Add(c Cred) bool {
	key := dedupKey(c)
	if _, ok := p.index[key]; ok {
		return false
	}
	p.index[key] = struct{}{}
	p.creds = append(p.creds, c)
	return true
}

// AddAll adds every cred from src, returning the count of new (non-dup) ones.
// AddAll 添加 src 中每个 cred，返回新增（去重后）的数量。
func (p *Pool) AddAll(src []Cred) int {
	n := 0
	for _, c := range src {
		if p.Add(c) {
			n++
		}
	}
	return n
}

// All returns a copy of the credential slice. Order is insertion order.
// All 返回凭据切片的副本；顺序为插入顺序。
func (p *Pool) All() []Cred {
	out := make([]Cred, len(p.creds))
	copy(out, p.creds)
	return out
}

// Len returns the number of unique credentials. / Len 返回去重后凭据数。
func (p *Pool) Len() int { return len(p.creds) }

// dedupKey returns the map key used for dedup. / dedupKey 返回用于去重的 map key。
func dedupKey(c Cred) string {
	return string(c.Method) + "|" + c.User + "|" + c.Pass + "|" + c.KeyPath
}

// LoadOptions configures file loading. / LoadOptions 配置文件加载。
type LoadOptions struct {
	// Users / Passes are inline values from -u / -P flags. / Users / Passes
	// 来自 -u / -P 的内联值。
	Users  []string
	Passes []string

	// UserFile / PassFile are paths from -user-file / -pass-file.
	// UserFile / PassFile 来自 -user-file / -pass-file 的路径。
	UserFile string
	PassFile string

	// DefaultMethod selects the AuthMethod for loaded credentials.
	// DefaultMethod 默认 AuthPassword。/ DefaultMethod 选择加载凭据的 AuthMethod。
	DefaultMethod AuthMethod
}

// LoadInto populates pool from opts. Returns the number of unique
// credentials added. / LoadInto 从 opts 填充 pool，返回新增的（去重后）数量。
//
// Layout: each user is paired with each pass (Cartesian product).
// If only users are provided, each user is paired with an empty pass.
// If only passes are provided, "anonymous" is paired with each pass.
//
// 布局：每个 user 与每个 pass 笛卡尔配对。只给 user 时，每个 user 与空 pass
// 配对；只给 pass 时，"anonymous" 与每个 pass 配对。
func LoadInto(pool *Pool, opts LoadOptions) (int, error) {
	method := opts.DefaultMethod
	if method == "" {
		method = AuthPassword
	}

	users, err := readLines(opts.Users, opts.UserFile)
	if err != nil {
		return 0, fmt.Errorf("read users: %w", err)
	}
	passes, err := readLines(opts.Passes, opts.PassFile)
	if err != nil {
		return 0, fmt.Errorf("read passes: %w", err)
	}

	// If neither users nor passes provided, pool is empty.
	// 都没给：池为空。
	if len(users) == 0 && len(passes) == 0 {
		return 0, nil
	}

	// Normalize: ensure we have at least one of each.
	// 归一化：保证两边都至少有一个。
	if len(users) == 0 {
		users = []string{""} // anonymous / 匿名
	}
	if len(passes) == 0 {
		passes = []string{""}
	}

	added := 0
	for _, u := range users {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		for _, p := range passes {
			p = strings.TrimSpace(p)
			// (empty pass is allowed; auth methods may treat it
			// as "no password" or fail — depends on protocol)
			// (允许空 pass；不同协议可能视为"无密码"或直接失败)
			if pool.Add(Cred{User: u, Pass: p, Method: method}) {
				added++
			}
		}
	}
	return added, nil
}

// readLines returns the union of inline values and file lines (one
// per line, '#' comments and blank lines stripped). / readLines 返回
// 内联值和文件行的并集（每行一个，'#' 注释和空行去除）。
func readLines(inline []string, filePath string) ([]string, error) {
	var out []string
	out = append(out, inline...)
	if filePath == "" {
		return out, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Allow long lines (some key-file paths are long).
	// 允许长行（部分 key 文件路径很长）。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, scanner.Err()
}
