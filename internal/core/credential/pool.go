// Package cred: credential pool with dedup + loader.
// Package cred: 凭据池（去重 + 加载器）。
package credential

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"os"
	"strings"
)

// dedupHMACKey is a per-process random 32-byte key used to HMAC-hash
// the dedup map key. Without this the map key is a Go string
// containing the cleartext password — a process memory dump (core,
// /proc/<pid>/mem, panic trace) would leak every sprayed cred via
// the map. The HMAC makes the key a fixed-size opaque blob that
// can't be reversed without the per-process key (which is itself
// gone after the process exits).
//
// dedupHMACKey 是进程级随机 32 字节密钥，用于 HMAC 哈希去重 map
// 键。没有这个密钥时 map 键是含明文密码的 Go string——进程内存转
// 储（core、/proc/<pid>/mem、panic 栈）会通过 map 泄露所有喷洒过
// 的凭据。HMAC 让 key 变成定长不透明 blob，不持密钥不可逆。
var dedupHMACKey = randomBytes(32)

// randomBytes returns n bytes from crypto/rand. Used for the
// per-process HMAC key at package init; cannot fail in practice
// (Go's runtime reserves entropy before main()).
//
// randomBytes 从 crypto/rand 取 n 字节。供包 init 时的进程级 HMAC
// 密钥用；实际不会失败（Go runtime 在 main() 前保留了熵）。
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read on linux/darwin/windows reads from
		// getrandom(2) / getentropy(2) / CryptGenRandom,
		// which cannot fail under normal conditions. If we
		// somehow did, fall back to all-zeroes — the map
		// still works (we just lose the unlinkability from
		// any external observer).
		//
		// crypto/rand.Read 在 linux/darwin/windows 读
		// getrandom(2) / getentropy(2) / CryptGenRandom，正
		// 常情况下不会失败。若真失败，回退全零——map 仍能用
		// （只是失去与外部观察者的不可关联性）。
		return make([]byte, n)
	}
	return b
}

// Pool is a deduplicated, in-memory credential set.
//
// Security: the dedup index uses HMAC-hashed keys (not the cleartext)
// so a heap dump / panic trace doesn't surface every sprayed
// cred. The creds slice itself still holds cleartext — that's
// required because the actual spray needs User/Pass to wire the
// protocol. Call Clear() after the scan to zero the slice.
//
// Pool 是去重后的内存凭据集合。
//
// 安全：去重索引用 HMAC 哈希键（不是明文），让堆 dump / panic 栈
// 不暴露所有喷洒过的凭据。creds 切片本身仍持明文——实际喷洒需要
// User/Pass 来接线协议。扫完后调 Clear() 清零切片。
type Pool struct {
	creds []Cred
	// index by HMAC(dedupHMACKey, method|user|pass|keypath) for
	// fast dedup without leaking cleartext on the heap.
	// / 按 HMAC(dedupHMACKey, method|user|pass|keypath) 索引
	// 以快去重，且不在堆上泄露明文。
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
//
// The copy is a shallow copy: the returned Cred structs contain
// pointers to the same backing strings (User / Pass / KeyPath) as
// the pool. The strings themselves are NOT copied — that's fine
// because the strings are immutable in Go, but it means a heap
// dump after All() can still see cleartext. Call Clear() when
// done with the slice to wipe the pool's strings.
//
// All 返回凭据切片的副本；顺序为插入顺序。
//
// 副本是浅拷贝：返回的 Cred 结构里的 User / Pass / KeyPath 仍
// 指向池的同一份 backing string。string 本身未拷贝——Go 里 string
// 不可变，所以问题不大；但 All() 之后的堆 dump 仍能看到明文。用
// 完切片后调 Clear() 来清零池的 string。
func (p *Pool) All() []Cred {
	out := make([]Cred, len(p.creds))
	copy(out, p.creds)
	return out
}

// Len returns the number of unique credentials. / Len 返回去重后凭据数。
func (p *Pool) Len() int { return len(p.creds) }

// Clear wipes the pool's credential strings and drops the dedup
// index. After Clear, Len() == 0 and All() returns an empty slice.
// The pool can be re-used (Add() will start a fresh index).
//
// Clear is best-effort against heap dumps — it zeros the Cred
// struct fields in place. The backing string allocation may
// still hold the cleartext in freed memory until the GC reclaims
// it (Go's runtime doesn't guarantee immediate wipe on free), so
// this is a defense-in-depth measure, not a guarantee.
//
// Clear 清零池的凭据字符串并丢弃去重索引。Clear 后 Len()==0 且
// All() 返回空切片。池可继续用（Add() 会起新索引）。
//
// Clear 是堆 dump 防御——原地清零 Cred 结构字段。底层的 string 分
// 配在 GC 回收前仍可能保留明文（Go runtime 不保证 free 时立即清
// 零），所以是纵深防御，不是保证。
func (p *Pool) Clear() {
	for i := range p.creds {
		// Zeroing a string is a no-op (strings are immutable in
		// Go) — but we can overwrite the Cred struct's fields by
		// mutating the slice in place via a small dance. In
		// practice the runtime allocates Cred as a value type
		// (not a pointer), so p.creds[i] is mutable.
		//
		// 清零 string 是 no-op（Go 里 string 不可变）——但我们可
		// 以通过原地修改 Cred 结构字段。实际运行时把 Cred 当值
		// 类型（不是指针），所以 p.creds[i] 可变。
		p.creds[i] = Cred{}
	}
	p.creds = p.creds[:0]
	// Drop the index entirely; reuse requires a fresh map so
	// the runtime can GC the old string references in map
	// keys. (We can't iterate p.index to clear it — the keys
	// are HMAC hashes, but the map's hash slots still hold
	// references that prevent GC of the bucket array.)
	p.index = make(map[string]struct{})
}

// dedupKey returns the HMAC-SHA256(dedupHMACKey, ...) of the
// (method, user, pass, keypath) tuple, base32-encoded for a
// printable map key. Two identical creds produce the same key;
// two different creds produce different keys with collision
// probability 2^-128 (cryptographically negligible).
//
// dedupKey 返回 (method, user, pass, keypath) 四元组的
// HMAC-SHA256(dedupHMACKey, ...) 哈希，base32 编码得可打印的 map
// 键。两条相同 cred 出同一 key；不同 cred 出不同 key 的碰撞概率
// 2^-128（密码学上可忽略）。
func dedupKey(c Cred) string {
	// concat with a NUL separator to avoid "user='ab' pass='c' +
	// user='a' pass='bc'" collisions. / 用 NUL 分隔避免
	// "user='ab' pass='c' + user='a' pass='bc'" 碰撞。
	mac := hmac.New(sha256.New, dedupHMACKey)
	mac.Write([]byte(string(c.Method)))
	mac.Write([]byte{0})
	mac.Write([]byte(c.User))
	mac.Write([]byte{0})
	mac.Write([]byte(c.Pass))
	mac.Write([]byte{0})
	mac.Write([]byte(c.KeyPath))
	sum := mac.Sum(nil)
	// base32 (no padding) gives a 52-char ASCII key. Crockford
	// encoding is more human-readable but the standard library
	// only ships StdEncoding; we use it for simplicity.
	//
	// base32（无 padding）给 52 字符 ASCII key。Crockford 更人
	// 友好但标准库只有 StdEncoding；为简洁用之。
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum)
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
// M5 audit fix: enforce MaxUsers / MaxPasses / MaxCredPairs limits to
// prevent OOM from huge dictionaries (e.g. 1M users × 1M passes).
//
// 布局：每个 user 与每个 pass 笛卡尔配对。只给 user 时，每个 user 与空 pass
// 配对；只给 pass 时，"anonymous" 与每个 pass 配对。
//
// M5 审计修法：强制 MaxUsers / MaxPasses / MaxCredPairs 上限，防止
// 巨大字典（如 1M 用户 × 1M 密码）导致 OOM。
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

	// M5 audit fix: enforce upper bounds to prevent OOM.
	// M5 审计修法：强制上限以防 OOM。
	if len(users) > MaxUsers {
		return 0, fmt.Errorf("too many users: %d > MaxUsers=%d (split the file or raise the limit)", len(users), MaxUsers)
	}
	if len(passes) > MaxPasses {
		return 0, fmt.Errorf("too many passes: %d > MaxPasses=%d (split the file or raise the limit)", len(passes), MaxPasses)
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

	// M5 audit fix: check the cartesian product size before expanding.
	// M5 审计修法：展开前检查笛卡尔积大小。
	pairCount := int64(len(users)) * int64(len(passes))
	if pairCount > int64(MaxCredPairs) {
		return 0, fmt.Errorf("credential cartesian product too large: %d > MaxCredPairs=%d (reduce users or passes)", pairCount, MaxCredPairs)
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

// MaxUsers / MaxPasses / MaxCredPairs are upper bounds enforced by
// LoadInto to prevent OOM from huge dictionaries. M5 audit fix.
// Operators with legitimate larger dictionaries can raise these via
// environment variables in a future version; for now they are
// package-level constants.
//
// MaxUsers / MaxPasses / MaxCredPairs 是 LoadInto 强制的上限，防止
// 巨大字典导致 OOM。M5 审计修法。有合法更大字典的操作员可在未来版本
// 通过环境变量提升；目前是包级常量。
const (
	MaxUsers     = 100000
	MaxPasses    = 100000
	MaxCredPairs = 10000000 // 10M pairs max
)

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
