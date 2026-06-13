// output.go — multi-format result sink (TXT, NDJSON, creds, RDP).
// output.go — 多格式结果汇（text / NDJSON / 凭据 / RDP）。
package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// flushCloser wraps a *bufio.Writer around an *os.File and provides a
// Close() that flushes the buffer then closes the file. This lets us
// keep the io.WriteCloser type used in Output.
//
// flushCloser 把 *bufio.Writer 包到 *os.File 外面，Close() 行为：先 flush
// buffer 再关文件。这样 Output 可以统一用 io.WriteCloser 类型。
type flushCloser struct {
	bw *bufio.Writer
	f  *os.File
}

func (fc *flushCloser) Write(p []byte) (int, error) { return fc.bw.Write(p) }
func (fc *flushCloser) Close() error {
	if fc.bw != nil {
		_ = fc.bw.Flush()
	}
	if fc.f != nil {
		return fc.f.Close()
	}
	return nil
}

// Output writes results to TXT, NDJSON, creds, and RDP files.
// Output 把结果写入 TXT、NDJSON、凭据、RDP 文件。
//
// All writes are serialized through an internal mutex so that multiple
// producer/consumer goroutines can call Write() / WriteCred() / WriteRDP()
// concurrently without interleaving bytes.
//
// 所有写入都通过内部 mutex 串行化，因此多个 producer/consumer 协程可并发
// 调用 Write() / WriteCred() / WriteRDP() 而不会出现字节交错。
type Output struct {
	mu sync.Mutex

	// txt  : one human-readable line per result
	// json : one JSON object per line (NDJSON)
	// creds: "host:port  plugin  user/pass  time" per hit
	// rdpjson / rdptxt: RDP deep fingerprint
	txt, jsn, creds, rdpjson, rdptxt *flushCloser
}

// OutputConfig configures which files Output should open.
// OutputConfig 配置 Output 应打开的文件。
type OutputConfig struct {
	ResultTXTPath  string // empty = no txt output
	ResultJSONPath string // empty = no json output
	CredsPath      string // empty = no creds output
	RDPJSONPath    string // empty = no rdp.json output
	RDPTXTPath     string // empty = no rdp.txt output
}

// OpenOutput opens (creates if needed) the configured output files and
// returns a writer that is safe for concurrent use.
//
// OpenOutput 打开（如不存在则创建）配置指定的输出文件，返回并发安全的 writer。
func OpenOutput(cfg OutputConfig) (*Output, error) {
	o := &Output{}
	type opener struct {
		path string
		set  func(*flushCloser)
	}
	openers := []opener{
		{cfg.ResultTXTPath, func(w *flushCloser) { o.txt = w }},
		{cfg.ResultJSONPath, func(w *flushCloser) { o.jsn = w }},
		{cfg.CredsPath, func(w *flushCloser) { o.creds = w }},
		{cfg.RDPJSONPath, func(w *flushCloser) { o.rdpjson = w }},
		{cfg.RDPTXTPath, func(w *flushCloser) { o.rdptxt = w }},
	}
	for _, op := range openers {
		if op.path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(op.path), 0o755); err != nil {
			_ = o.Close()
			return nil, fmt.Errorf("mkdir for %s: %w", op.path, err)
		}
		f, err := os.OpenFile(op.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			_ = o.Close()
			return nil, fmt.Errorf("open %s: %w", op.path, err)
		}
		op.set(&flushCloser{bw: bufio.NewWriter(f), f: f})
	}
	return o, nil
}

// Close flushes and closes all opened files. Safe to call on a partially-
// initialized Output (e.g. when OpenOutput failed midway).
//
// Close 刷新并关闭所有已打开的文件。允许在 OpenOutput 中途失败的部分初始化
// 状态上调用。
func (o *Output) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var firstErr error
	closeAll := func(w *flushCloser, label string) {
		if w == nil {
			return
		}
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", label, err)
		}
	}
	closeAll(o.txt, "txt")
	closeAll(o.jsn, "json")
	closeAll(o.creds, "creds")
	closeAll(o.rdpjson, "rdp.json")
	closeAll(o.rdptxt, "rdp.txt")
	return firstErr
}

// Flush forces all buffered writers to flush to disk. Call periodically
// (and on shutdown) to ensure data is durable.
//
// Flush 强制把所有 buffer 写盘。周期性调用（以及关闭前）以保证数据落盘。
func (o *Output) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var firstErr error
	flush := func(w *flushCloser) {
		if w == nil {
			return
		}
		if w.bw != nil {
			if err := w.bw.Flush(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	flush(o.txt)
	flush(o.jsn)
	flush(o.creds)
	flush(o.rdpjson)
	flush(o.rdptxt)
	return firstErr
}

// WriteResult writes a single result to TXT and NDJSON files.
// WriteResult 把单个 result 写入 TXT 和 NDJSON 文件。
func (o *Output) WriteResult(r *types.Result) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.txt != nil {
		ts := r.Time.Format("2006-01-02 15:04:05")
		var credSuffix string
		if r.Cred != nil {
			credSuffix = fmt.Sprintf("  [cred] %s / %s", r.Cred.User, r.Cred.Pass)
		}
		fmt.Fprintf(o.txt, "%s [+] %s:%d  [%s]  %s%s\n",
			ts, r.Host, r.Port, r.Service, r.Banner, credSuffix)
	}
	if o.jsn != nil {
		enc := json.NewEncoder(o.jsn)
		_ = enc.Encode(r)
	}
	return nil
}

// WriteCred appends a credential hit to creds.txt (separate from
// result.txt to make it easy to grep / diff).
//
// WriteCred 追加凭据命中到 creds.txt（与 result.txt 分离便于 grep / diff）。
func (o *Output) WriteCred(r *types.Result) error {
	if r.Cred == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.creds == nil {
		return nil
	}
	ts := r.Time.Format("2006-01-02 15:04:05")
	fmt.Fprintf(o.creds, "%s:%d  %s  %s / %s  %s\n",
		r.Host, r.Port, r.Service, r.Cred.User, r.Cred.Pass, ts)
	return nil
}

// RDPFingerprint is the extended RDP fingerprint structure that we persist
// to dedicated rdp.json / rdp.txt files (beyond the regular result stream).
//
// RDPFingerprint 是我们持久化到专用 rdp.json / rdp.txt 文件的扩展 RDP 指纹结构
// （超出常规 result 流的范围）。
type RDPFingerprint struct {
	Host             string    `json:"host"`
	Port             int       `json:"port"`
	ServerName       string    `json:"server_name,omitempty"`
	Domain           string    `json:"domain,omitempty"`
	DomainJoined     bool      `json:"domain_joined"`
	OSVersion        string    `json:"os_version,omitempty"`
	OSBuild          string    `json:"os_build,omitempty"`
	ProductID        string    `json:"product_id,omitempty"`
	ServerFlags      []string  `json:"server_flags,omitempty"`
	NLASupported     bool      `json:"nla_supported"`
	CredSSPSupported bool      `json:"credssp_supported"`
	CertSubject      string    `json:"cert_subject,omitempty"`
	CertIssuer       string    `json:"cert_issuer,omitempty"`
	CertValidFrom    string    `json:"cert_valid_from,omitempty"`
	CertValidTo      string    `json:"cert_valid_to,omitempty"`
	CertThumbprint   string    `json:"cert_thumbprint,omitempty"`
	ProtocolVersion  uint32    `json:"protocol_version,omitempty"`
	ScanTime         time.Time `json:"scan_time"`
}

// WriteRDP writes a structured RDP fingerprint to rdp.json (NDJSON) and
// rdp.txt (human-readable).
//
// WriteRDP 把结构化的 RDP 指纹写入 rdp.json（NDJSON）和 rdp.txt（人类可读）。
func (o *Output) WriteRDP(fp RDPFingerprint) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rdpjson != nil {
		enc := json.NewEncoder(o.rdpjson)
		_ = enc.Encode(fp)
	}
	if o.rdptxt != nil {
		ts := fp.ScanTime.Format("2006-01-02 15:04:05")
		fmt.Fprintf(o.rdptxt,
			"[%s] %s:%d  name=%q domain=%q os=%s build=%s nla=%v flags=%v cert=%q issuer=%q\n",
			ts, fp.Host, fp.Port,
			fp.ServerName, fp.Domain, fp.OSVersion, fp.OSBuild,
			fp.NLASupported, fp.ServerFlags,
			fp.CertSubject, fp.CertIssuer)
	}
	return nil
}
