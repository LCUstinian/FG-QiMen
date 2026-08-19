// output.go — multi-format result sink (TXT, NDJSON, creds, RDP, CSV).
// output.go — 多格式结果汇（text / NDJSON / 凭据 / RDP / CSV）。
package output

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
		// P2-7 (audit): f.Sync() forces the OS to flush the file's
		// buffers, surviving power-loss / OOM-kill without losing the
		// last ~200ms of buffered writes. / P2-7（审计）：f.Sync() 强制
		// OS 把文件 buffer 落盘，避免掉电/OOM 杀进程时丢最后 ~200ms
		// 写入。
		// We deliberately ignore the Sync error: Close() below is the
		// authoritative close, and failing here would mask the close
		// error which is more actionable. / 这里有意忽略 Sync 错误：
		// 下面的 Close() 是权威关闭，此处失败会掩盖更可执行的 close 错误。
		_ = fc.f.Sync()
		return fc.f.Close()
	}
	return nil
}

// Output writes results to TXT, NDJSON, creds, RDP, and CSV files.
// Output 把结果写入 TXT、NDJSON、凭据、RDP、CSV 文件。
//
// Each sink has its own mutex so writes to one file never block writes
// to another. At 200 worker goroutines pushing results concurrently,
// the previous single-mutex design serialised all five sinks — a slow
// creds.txt write (e.g. on Windows Defender scan) would block txt and
// json writes for the duration. The per-sink split keeps each sink
// independently hot. / 每个 sink 有独立 mutex，写一个文件不阻塞其他。
// 200 worker 并发推结果时，旧单 mutex 设计把 5 个 sink 串行化——
// 一个慢的 creds.txt 写（如 Windows Defender 扫描时）会阻塞 txt
// 和 json 写。per-sink 拆分让每个 sink 独立。
type Output struct {
	// Per-sink mutexes. Locked by the matching Write* / Close / Flush
	// method. / 每个 sink 独立 mutex。由对应 Write* / Close / Flush
	// 方法上锁。
	txtMu, jsnMu, credsMu, rdpjsonMu, rdptxtMu, csvMu sync.Mutex

	// txt  : one human-readable line per result
	// json : one JSON object per line (NDJSON)
	// creds: "host:port  plugin  user/pass  time" per hit
	// rdpjson / rdptxt: RDP deep fingerprint
	// csv  : RFC 4180 one row per result (header on first write)
	txt, jsn, creds, rdpjson, rdptxt, csv *flushCloser

	// csvWriter is hoisted to a field so we allocate it once at
	// OpenOutput time, not per WriteResult. The previous code
	// allocated csv.NewWriter(o.csv.bw) inside the lock on every
	// result row — a measurable hot-path allocation at 200+ workers.
	// / csvWriter 提升为字段，仅 OpenOutput 时分配一次。旧代码在
	// 锁内 per-row 调 csv.NewWriter——200+ worker 下是热路径分配。
	csvWriter *csv.Writer

	// csvHeaderWritten tracks whether the CSV header has been emitted
	// yet. We use a plain bool (not a separate "exists in the file"
	// check) because the file is opened with O_APPEND — the bool is
	// always 0 after OpenOutput.
	//
	// csvHeaderWritten 跟踪 CSV 表头是否已写入。直接用 bool（不查
	// "文件中是否已存在"）是因为文件以 O_APPEND 打开，OpenOutput 后
	// bool 一定是 0。
	csvHeaderWritten bool

	// showCleartext gates whether result.txt, result.json, and result.csv
	// embed the cleartext password (default: redacted fingerprint).
	// creds.txt is ALWAYS cleartext — the operator's working file.
	// See OutputConfig.ShowCleartext. P4.5 (audit roadmap): the
	// --show-creds flag (which sets ShowCleartext) applies to ALL
	// shareable sinks (TXT/JSON/CSV), NOT to creds.txt.
	// / showCleartext 决定 result.txt / result.json / result.csv 是否
	// 嵌入明文密码（默认：脱敏指纹）。creds.txt 始终明文——操作员工作文
	// 件。P4.5（审计路线图）：--show-creds flag（设置 ShowCleartext）作用
	// 于所有可分享 sink（TXT/JSON/CSV），而非 creds.txt。
	showCleartext bool
}

// OutputConfig configures which files Output should open.
// OutputConfig 配置 Output 应打开的文件。
type OutputConfig struct {
	ResultTXTPath  string // empty = no txt output
	ResultJSONPath string // empty = no json output
	ResultCSVPath  string // empty = no csv output
	CredsPath      string // empty = no creds output
	RDPJSONPath    string // empty = no rdp.json output
	RDPTXTPath     string // empty = no rdp.txt output

	// ShowCleartext controls whether result.txt's "[cred] user / pass"
	// suffix renders cleartext or the redacted fingerprint (see
	// types.ShowUserPassword). creds.txt is ALWAYS cleartext — the
	// operator runs the scan and needs the actual password to use
	// the discovered credential. result.txt is the shareable surface
	// (gitignored but routinely pasted into tickets / chat) so it
	// gets the gate.
	//
	// ShowCleartext 控制 result.txt 的 "[cred] user / pass" 后缀渲染
	// 明文还是脱敏指纹（见 types.ShowUserPassword）。creds.txt 始终是
	// 明文——操作员跑扫描就是为了拿到真实口令去用。result.txt 才是会
	// 被复制到工单/聊天里的可分享表面，所以加门。
	ShowCleartext bool
}

// OpenOutput opens (creates if needed) the configured output files and
// returns a writer that is safe for concurrent use.
//
// OpenOutput 打开（如不存在则创建）配置指定的输出文件，返回并发安全的 writer。
func OpenOutput(cfg OutputConfig) (*Output, error) {
	o := &Output{showCleartext: cfg.ShowCleartext}
	type opener struct {
		path string
		perm os.FileMode
		set  func(*flushCloser)
	}
	openers := []opener{
		{cfg.ResultTXTPath, 0o644, func(w *flushCloser) { o.txt = w }},
		{cfg.ResultJSONPath, 0o644, func(w *flushCloser) { o.jsn = w }},
		{cfg.CredsPath, 0o600, func(w *flushCloser) { o.creds = w }},
		{cfg.RDPJSONPath, 0o644, func(w *flushCloser) { o.rdpjson = w }},
		{cfg.RDPTXTPath, 0o644, func(w *flushCloser) { o.rdptxt = w }},
		{cfg.ResultCSVPath, 0o644, func(w *flushCloser) {
			o.csv = w
			// Allocate csv.Writer once; reused per WriteResult.
			// / 一次性分配 csv.Writer；WriteResult 复用。
			o.csvWriter = csv.NewWriter(w.bw)
		}},
	}
	for _, op := range openers {
		if op.path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(op.path), 0o755); err != nil {
			_ = o.Close()
			return nil, fmt.Errorf("mkdir for %s: %w", op.path, err)
		}
		f, err := os.OpenFile(op.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, op.perm)
		if err != nil {
			_ = o.Close()
			return nil, fmt.Errorf("open %s: %w", op.path, err)
		}
		op.set(&flushCloser{bw: bufio.NewWriter(f), f: f})
	}
	return o, nil
}

// Close flushes and closes all opened files. Safe to call on a partially-
// initialized Output (e.g. when OpenOutput failed midway). Each sink is
// closed under its own mutex so this method's wall-clock scales with
// the slowest sink, not with the sum.
//
// Close 刷新并关闭所有已打开的文件。允许在 OpenOutput 中途失败的部分初始化
// 状态上调用。每个 sink 在自己的 mutex 下关闭，本方法的墙钟取决于最慢
// 的 sink 而非总和。
func (o *Output) Close() error {
	type closable struct {
		w     *flushCloser
		mu    *sync.Mutex
		label string
	}
	closers := []closable{
		{o.txt, &o.txtMu, "txt"},
		{o.jsn, &o.jsnMu, "json"},
		{o.creds, &o.credsMu, "creds"},
		{o.rdpjson, &o.rdpjsonMu, "rdp.json"},
		{o.rdptxt, &o.rdptxtMu, "rdp.txt"},
		{o.csv, &o.csvMu, "csv"},
	}
	var firstErr error
	for _, c := range closers {
		if c.w == nil {
			continue
		}
		c.mu.Lock()
		err := c.w.Close()
		c.mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", c.label, err)
		}
	}
	return firstErr
}

// Flush forces all buffered writers to flush to disk. Call periodically
// (and on shutdown) to ensure data is durable.
//
// Flush 强制把所有 buffer 写盘。周期性调用（以及关闭前）以保证数据落盘。
func (o *Output) Flush() error {
	type flushable struct {
		w  *flushCloser
		mu *sync.Mutex
	}
	flushers := []flushable{
		{o.txt, &o.txtMu},
		{o.jsn, &o.jsnMu},
		{o.creds, &o.credsMu},
		{o.rdpjson, &o.rdpjsonMu},
		{o.rdptxt, &o.rdptxtMu},
		{o.csv, &o.csvMu},
	}
	var firstErr error
	for _, f := range flushers {
		if f.w == nil || f.w.bw == nil {
			continue
		}
		f.mu.Lock()
		err := f.w.bw.Flush()
		f.mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WriteResult writes a single result to TXT, NDJSON, and CSV files.
// Each sink's lock is acquired independently so a slow sink can't
// head-of-line-block the others. / WriteResult 把单个 result 写入
// TXT、NDJSON、CSV 文件。每个 sink 的锁独立获取，一个慢 sink
// 不会 head-of-line 阻塞其他。
func (o *Output) WriteResult(r *types.Result) error {
	// TXT — own mutex. / TXT —— 独立 mutex。
	if o.txt != nil {
		o.txtMu.Lock()
		ts := r.Time.Format("2006-01-02 15:04:05")
		var credSuffix string
		if r.Cred != nil {
			// Redact by default — result.txt is the shareable surface
			// (gitignored but routinely pasted into tickets / chat).
			// creds.txt (WriteCred below) is the operator's working
			// file and always contains cleartext. P0#2.
			// 默认脱敏——result.txt 是可分享表面（虽然 gitignored 但
			// 经常被粘到工单/聊天里）。creds.txt（下方 WriteCred）是
			// 操作员工作文件，始终是明文。P0#2。
			cfg := &types.Config{ShowCleartext: o.showCleartext}
			credSuffix = "  [cred] " + types.ShowUserPassword(cfg, r.Cred.User, r.Cred.Pass)
		}
		fmt.Fprintf(o.txt, "%s [+] %s:%d  [%s]  %s%s\n",
			ts, r.Host, r.Port, r.Service, r.Banner, credSuffix)
		o.txtMu.Unlock()
	}
	// JSON — own mutex. / JSON —— 独立 mutex。
	if o.jsn != nil {
		o.jsnMu.Lock()
		// MINOR audit fix: apply the same redaction policy to result.json
		// as result.txt. Without this, json.Encode(r) writes cleartext
		// passwords to result.json even when ShowCleartext is false.
		// We shallow-copy r and swap in a redacted Cred so the original
		// (which may be reused by other sinks) is untouched.
		// / MINOR 审计修法：对 result.json 施加与 result.txt 相同的脱敏
		// 策略。否则 json.Encode(r) 会在 ShowCleartext=false 时仍把明文
		// 密码写进 result.json。我们浅拷 r 并换入脱敏后的 Cred，原对象
		// （可能被其他 sink 复用）不受影响。
		out := r
		if r.Cred != nil && !o.showCleartext {
			cp := *r
			cp.Cred = &types.Cred{
				User:     types.RedactUser(r.Cred.User),
				Pass:     types.RedactPassword(r.Cred.Pass),
				AuthType: r.Cred.AuthType,
			}
			out = &cp
		}
		enc := json.NewEncoder(o.jsn)
		_ = enc.Encode(out)
		o.jsnMu.Unlock()
	}
	// CSV — own mutex; csv.Writer hoisted to field, header written
	// exactly once. / CSV —— 独立 mutex；csv.Writer 提升为字段，
	// 表头仅写一次。
	if o.csv != nil {
		o.csvMu.Lock()
		if !o.csvHeaderWritten {
			_ = o.csvWriter.Write(csvHeader)
			o.csvHeaderWritten = true
		}
		_ = o.writeCSVvia(o.csvWriter, r)
		o.csvWriter.Flush()
		o.csvMu.Unlock()
	}
	return nil
}

// writeCSVvia is a thin indirection that lets us swap csv.Writer for
// a test double (see csv_test.go). It only emits one row at a time
// (the header is owned by writeCSV which is called on the first row).
//
// writeCSVvia 是薄间接层，便于在测试中替换 csv.Writer。
// 只写一行（表头由 writeCSV 在第一行时负责）。
func (o *Output) writeCSVvia(cw *csv.Writer, r *types.Result) error {
	// Apply the same redaction policy as result.txt / result.json.
	// 施加与 result.txt / result.json 相同的脱敏策略。
	user, pass := "", ""
	if r.Cred != nil {
		cfg := &types.Config{ShowCleartext: o.showCleartext}
		user, pass = splitUserPass(types.ShowUserPassword(cfg, r.Cred.User, r.Cred.Pass))
	}
	row := []string{
		r.Time.Format("2006-01-02 15:04:05"),
		r.Host,
		strconv.Itoa(r.Port),
		r.Service,
		r.Plugin,
		"open", // all results reaching the sink are "open" ports
		truncateForCSV(r.Banner, 1024),
		user,
		pass,
	}
	return cw.Write(row)
}

// WriteCred appends a credential hit to creds.txt (separate from
// result.txt to make it easy to grep / diff). Uses its own mutex so
// a slow creds.txt write can't block other sinks. / WriteCred 追加
// 凭据命中到 creds.txt（与 result.txt 分离便于 grep / diff）。独立
// mutex，慢 creds.txt 写不阻塞其他 sink。
func (o *Output) WriteCred(r *types.Result) error {
	if r.Cred == nil || o.creds == nil {
		return nil
	}
	o.credsMu.Lock()
	defer o.credsMu.Unlock()
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
// rdp.txt (human-readable). Each file has its own mutex. / WriteRDP
// 把结构化的 RDP 指纹写入 rdp.json（NDJSON）和 rdp.txt（人类可读）。
// 每个文件独立 mutex。
func (o *Output) WriteRDP(fp RDPFingerprint) error {
	if o.rdpjson != nil {
		o.rdpjsonMu.Lock()
		enc := json.NewEncoder(o.rdpjson)
		_ = enc.Encode(fp)
		o.rdpjsonMu.Unlock()
	}
	if o.rdptxt != nil {
		o.rdptxtMu.Lock()
		ts := fp.ScanTime.Format("2006-01-02 15:04:05")
		fmt.Fprintf(o.rdptxt,
			"[%s] %s:%d  name=%q domain=%q os=%s build=%s nla=%v flags=%v cert=%q issuer=%q\n",
			ts, fp.Host, fp.Port,
			fp.ServerName, fp.Domain, fp.OSVersion, fp.OSBuild,
			fp.NLASupported, fp.ServerFlags,
			fp.CertSubject, fp.CertIssuer)
		o.rdptxtMu.Unlock()
	}
	return nil
}
