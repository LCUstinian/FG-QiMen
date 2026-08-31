// rotate.go — size-based file rotation for Output sinks.
//
// v0.4 Phase 2.3: each txt/json/csv/creds/rdp sink can be wrapped
// in a rotatingWriter that auto-rolls the file when it crosses
// the configured size cap. Old files are renamed
// <path>.1 .2 .3 ...; the cap is configurable.
//
// rotate.go — Output sink 的大小轮转。
//
// v0.4 Phase 2.3：每个 txt/json/csv/creds/rdp sink 可包到
// rotatingWriter 里，跨过配置的大小阈值时自动滚动。旧文件改名
// <path>.1 .2 .3 ...；上限可配置。

package output

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// rotatingWriter wraps an *os.File-backed bufio.Writer with
// size-based rotation. When the in-memory byte counter crosses
// `maxBytes` (checked on every Write), the active file is
// closed, files <path>.1 .2 .3 ... are shifted up by one
// position, and a fresh <path> is opened. The shift stops at
// `maxFiles-1`; files beyond the cap are dropped.
//
// rotatingWriter 给基于 *os.File 的 bufio.Writer 包一层大小轮转。
// 当内存字节计数跨过 `maxBytes`（每次 Write 检查），关当前文件，
// 把 <path>.1 .2 .3 ... 全部上移一位，开新 <path>。轮转在
// `maxFiles-1` 停；超出上限的文件被丢弃。
type rotatingWriter struct {
	mu sync.Mutex
	// path is the canonical (active) file path. .1 .2 .3 ...
	// are derived from path. / path 是现行文件路径。
	// .1 .2 .3 ... 由 path 衍生。
	path string
	// perm is the file mode for newly opened files. / perm 是
	// 新开文件的模式。
	perm os.FileMode
	// maxBytes is the per-file size cap. <=0 disables rotation
	// (degrades to a plain os.File wrapper). / maxBytes 是单文件
	// 大小上限。<=0 关闭轮转（退化为普通 os.File 包）。
	maxBytes int64
	// maxFiles is the total number of files to keep (active +
	// .1 .2 ...). <=0 disables rotation. / maxFiles 是保留的
	// 文件总数（现行 + .1 .2 ...）。<=0 关闭轮转。
	maxFiles int
	// current is the active bufio.Writer. nil after Close. / current
	// 是现行 bufio.Writer。Close 后为 nil。
	current *bufio.Writer
	// f is the active *os.File. / f 是现行 *os.File。
	f *os.File
	// written is the byte counter for the active file. Reset on
	// rotate. / written 是现行文件的字节计数。rotate 时重置。
	written int64
}

// newRotatingWriter opens path for write and returns a writer
// that auto-rotates. maxBytes <= 0 or maxFiles <= 0 returns
// a plain (non-rotating) writer. / newRotatingWriter 开 path
// 写并返回自动轮转的 writer。maxBytes <= 0 或 maxFiles <= 0
// 返回非轮转的普通 writer。
//
// Always MkdirAll on the parent directory before opening, so
// callers that pass a fresh <dir>/<file> path (no dir yet)
// get the same behavior as the previous os.OpenFile path.
// / 总是先 MkdirAll 父目录再开，让传 <dir>/<file>（dir 还不
// 存在）的调用方拿到与原 os.OpenFile 路径一致的行为。
func newRotatingWriter(path string, perm os.FileMode, maxBytes int64, maxFiles int) (*rotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perm)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &rotatingWriter{
		path:     path,
		perm:     perm,
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		current:  bufio.NewWriter(f),
		f:        f,
	}, nil
}

// Write buffers via bufio.Writer; checks the rotated byte
// counter after the write and rotates if the cap is crossed.
// / Write 走 bufio.Writer；写后检查轮转字节计数，跨过 cap
// 时轮转。
func (r *rotatingWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, err := r.current.Write(p)
	r.written += int64(n)
	if r.maxBytes > 0 && r.written >= r.maxBytes {
		if err := r.rotateLocked(); err != nil {
			return n, fmt.Errorf("rotate: %w", err)
		}
	}
	return n, err
}

// rotateLocked shifts files up by one position and opens a new
// active file. Caller must hold r.mu. / rotateLocked 把文件上
// 移一位并开新现行文件。调用方需持 r.mu。
func (r *rotatingWriter) rotateLocked() error {
	// Flush + sync + close the active file. / flush + sync + 关
	// 现行文件。
	if r.current != nil {
		_ = r.current.Flush()
	}
	if r.f != nil {
		_ = r.f.Sync()
		_ = r.f.Close()
	}
	r.current = nil
	r.f = nil
	// Shift .N → .(N+1), dropping the oldest. / 把 .N 移到 .(N+1)，
	// 丢掉最旧的。
	//
	// maxFiles is the total count (active + .1 .2 .(maxFiles-1)).
	// So the highest rotation index is maxFiles-2 (0-based).
	// We shift in reverse order so we don't overwrite. / maxFiles
	// 是总数（active + .1 .2 .(maxFiles-1)）。所以最高轮转索引
	// 是 maxFiles-2（0 基）。反向移动避免覆盖。
	for i := r.maxFiles - 2; i >= 0; i-- {
		from := r.rotatedName(i)
		to := r.rotatedName(i + 1)
		if _, err := os.Stat(from); err == nil {
			// Best-effort rename; if the target already exists
			// (e.g. an operator pre-empted us), overwrite it.
			// / 尽力 rename；如果目标已存在（如操作员抢跑）覆盖。
			_ = os.Rename(from, to)
		}
	}
	// Rename active → .1 and open fresh active. / 把 active 改名 .1
	// 并开新 active。
	_ = os.Rename(r.path, r.rotatedName(0))
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, r.perm)
	if err != nil {
		return fmt.Errorf("open fresh %s: %w", r.path, err)
	}
	r.f = f
	r.current = bufio.NewWriter(f)
	r.written = 0
	return nil
}

// rotatedName returns "<path>.<i>" for index i. / rotatedName
// 返索引 i 对应的 "<path>.<i>"。
func (r *rotatingWriter) rotatedName(i int) string {
	return r.path + "." + strconv.Itoa(i)
}

// Close flushes and syncs the active file. / Close 刷新并同
// 步当前文件。
func (r *rotatingWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		_ = r.current.Flush()
		r.current = nil
	}
	if r.f != nil {
		_ = r.f.Sync()
		err := r.f.Close()
		r.f = nil
		return err
	}
	return nil
}

// bw returns the current bufio.Writer so callers that need a
// *bufio.Writer (csv.Writer, json.Encoder) can wrap the
// rotating file. / bw 返当前 bufio.Writer，方便 csv.Writer /
// json.Encoder 这类需要 *bufio.Writer 的调用方包旋转文件。
// Not goroutine-safe; caller must serialize via Write/Close.
// / 非 goroutine 安全；调用方需通过 Write/Close 串行化。
func (r *rotatingWriter) bw() *bufio.Writer { return r.current }
