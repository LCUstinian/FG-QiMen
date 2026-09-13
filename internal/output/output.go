// output.go — multi-format result sink (TXT, NDJSON, creds, RDP, CSV).
// output.go — 多格式结果汇（text / NDJSON / 凭据 / RDP / CSV）。
package output

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// flushCloser wraps a *rotatingWriter and provides a Close() that
// flushes the active file and rotates if the size cap is hit.
//
// flushCloser 把 *rotatingWriter 包起来，Close() 行为：先 flush
// 当前文件，必要时滚到下一个文件。
type flushCloser struct {
	rw *rotatingWriter
}

func (fc *flushCloser) Write(p []byte) (int, error) { return fc.rw.Write(p) }
func (fc *flushCloser) Close() error {
	if fc.rw != nil {
		return fc.rw.Close()
	}
	return nil
}

// bw returns the underlying bufio.Writer so callers like
// csv.NewWriter / json.NewEncoder can wrap the rotating file.
// / bw 返底层 bufio.Writer，方便 csv.NewWriter / json.NewEncoder
// 这类调用方包旋转文件。
func (fc *flushCloser) bw() *bufio.Writer {
	if fc.rw == nil {
		return nil
	}
	return fc.rw.bw()
}

// Output writes results to TXT, NDJSON, creds, RDP, Web, and CSV files.
// Output 把结果写入 TXT、NDJSON、凭据、RDP、Web、CSV 文件。
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
	txtMu, jsnMu, credsMu, rdpjsonMu, rdptxtMu, webjsonMu, webtxtMu, csvMu, aliveMu sync.Mutex

	// txt  : one human-readable line per result
	// json : one JSON object per line (NDJSON)
	// creds: "host:port  plugin  user/pass  time" per hit
	// rdpjson / rdptxt: RDP deep fingerprint
	// webjson / webtxt: webtitle structured fingerprint (+ TLS identity)
	// csv  : RFC 4180 one row per result (header on first write)
	// alive: one host per line (deduped; pipeline-friendly)
	txt, jsn, creds, rdpjson, rdptxt, webjson, webtxt, csv, alive *flushCloser

	// csvWriter is hoisted to a field so we allocate it once at
	// OpenOutput time, not per WriteResult. The previous code
	// allocated csv.NewWriter(o.csv.bw) inside the lock on every
	// result row — a measurable hot-path allocation at 200+ workers.
	// / csvWriter 提升为字段，仅 OpenOutput 时分配一次。旧代码在
	// 锁内 per-row 调 csv.NewWriter——200+ worker 下是热路径分配。

	// jsnEnc is hoisted for the same reason as csvWriter (audit L-1):
	// json.NewEncoder per row was one avoidable allocation per result
	// on the NDJSON hot path. Encoder reuse is safe here — every use
	// is under jsnMu, and Encode emits one trailing newline per call,
	// which is exactly the NDJSON framing. / jsnEnc 与 csvWriter 同理
	// 提升（审计 L-1）：每行一个 json.NewEncoder 是 NDJSON 热路径上
	// 可省的一次分配。复用安全——所有使用都在 jsnMu 内，且 Encode
	// 每次调用输出一个换行，恰好就是 NDJSON 帧格式。
	jsnEnc *json.Encoder

	// AliveFormat wires the wire format of the alive-list file.
	// "" or "txt" = one host per line (default, pipeline-
	// friendly); "json" = NDJSON per line; "csv" = CSV header +
	// one row per host. Set from OutputConfig.AliveFormat at
	// OpenOutput time. / AliveFormat 控制 alive-list 文件线协议。
	// "" 或 "txt" = 每行一个 host（默认，管道友好）；"json" =
	// 每行 NDJSON；"csv" = CSV header + 每行一行 host。由
	// OpenOutput 时从 OutputConfig.AliveFormat 复制。
	AliveFormat string

	// aliveCSVWriter is allocated only when AliveFormat=="csv"; it
	// wraps the alive sink and owns the CSV header (written on
	// first call). Lazily-allocated on first use, just like the
	// per-sink dedup map. / aliveCSVWriter 仅在 AliveFormat=="csv"
	// 时分配；包 alive sink 拥有 CSV header（首次调用时写）。懒
	// 分配，跟 per-sink 去重 map 一样。
	aliveCSVWriter *csv.Writer
	csvWriter      *csv.Writer

	// csvHeaderWritten tracks whether the CSV header has been emitted
	// yet. We use a plain bool (not a separate "exists in the file"
	// check) because the file is opened with O_APPEND — the bool is
	// always 0 after OpenOutput.
	//
	// csvHeaderWritten 跟踪 CSV 表头是否已写入。直接用 bool（不查
	// "文件中是否已存在"）是因为文件以 O_APPEND 打开，OpenOutput 后
	// bool 一定是 0。
	csvHeaderWritten bool

	// aliveSeen dedupes the alive-list writes — a host that
	// responds to multiple probes (port 22 + 80 + 443) must
	// appear once, not three times. Guarded by aliveMu. The map
	// is allocated lazily on first use; nil-safe on the dead path
	// (no alive sink configured). / aliveSeen 对 alive-list 写
	// 去重——一个主机响应多个探测（22 + 80 + 443）必须只出现
	// 一次，不能三次。aliveMu 守护。map 在首次使用时懒分配；
	// alive sink 没配置时 nil-safe。
	aliveSeen map[string]struct{}

	// v0.4: SARIF buffer. SARIF is a single JSON document, not a
	// stream — we accumulate results and emit at Close(). / v0.4：
	// SARIF buffer。SARIF 是单 JSON 文档，不是流——累积结果并在
	// Close() 一次性输出。
	sarif    *flushCloser
	sarifMu  sync.Mutex
	sarifBuf []*types.Result

	// v0.9: discovery / shares / ftp sink pairs (NDJSON + txt) and the
	// servers aggregator. Discovery, shares and ftp stream incrementally;
	// servers aggregates per host and emits one record per host at
	// Close() (a half-written server row mid-stream would read as a
	// complete inventory). / v0.9：discovery / shares / ftp sink 对
	//（NDJSON + txt）与 servers 聚合器。discovery、shares、ftp 增量
	// 流式写；servers 按 host 聚合，Close() 时每 host 一条记录输出
	//（流中半截服务器记录会被误读为完整清单）。
	discjson, disctxt       *flushCloser
	discMu                  sync.Mutex
	sharesjson, sharestxt   *flushCloser
	sharesMu                sync.Mutex
	ftpjson, ftptxt         *flushCloser
	ftpMu                   sync.Mutex
	serversjson, serverstxt *flushCloser
	serversMu               sync.Mutex
	serversBuf              map[string]*ServerRecord

	// hostnameLookup enriches server records with hostnames captured by
	// the scope tracker (NBNS name tables). Injected via OutputConfig to
	// keep the output layer free of a core dependency.
	// / hostnameLookup 用范围追踪器捕获的主机名（NBNS 名字表）充实服
	// 务器记录。经 OutputConfig 注入，output 层因此不依赖 core。
	hostnameLookup func(host string) string

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
	WebJSONPath    string // empty = no web.json output
	WebTXTPath     string // empty = no web.txt output
	// ResultAlivePath is the optional alive-host list (one IP per
	// line, deduped). When set, every WriteResult appends the host
	// to this file. Pipeline-friendly: the output is directly
	// usable as `nmap -iL` / `masscan --targets` / `curl` loop
	// input. / ResultAlivePath 是可选的存活主机列表（每行一个
	// IP，去重）。设置后，每个 WriteResult 把 host 追加到此文件。
	// 管道友好——输出可直接用作 `nmap -iL` / `masscan --targets` /
	// `curl` 循环输入。
	ResultAlivePath string // empty = no alive-list output
	// AliveFormat controls the wire format of the alive-list file.
	// "" or "txt" = one host per line (default, pipeline-friendly).
	// "json" = NDJSON, one {host, port, service, time} per line.
	// "csv" = CSV header + one row per host.
	// / AliveFormat 控制 alive-list 文件的线协议格式。"" 或
	// "txt" = 每行一个 host（默认，管道友好）。"json" = NDJSON，
	// 每行一个 {host, port, service, time}。"csv" = CSV header +
	// 每行一个 host。
	AliveFormat string // "txt" | "json" | "csv"; empty = "txt"

	// v0.4: SARIF (Static Analysis Results Interchange Format) output.
	// When set, the SARIF JSON document is assembled and written at
	// Close() time (SARIF is a single document, not a stream).
	// / v0.4：SARIF（静态分析结果交换格式）输出。设置时，SARIF JSON
	// 文档在 Close() 时组装并写入（SARIF 是单文档，不是流）。
	ResultSARIFPath string // empty = no sarif output

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

	// v0.4: size-based output rotation. RotateMaxBytes is the
	// per-file size cap; when the active sink crosses it the
	// file is closed, renamed to <path>.1, and a new <path>
	// opened. RotateMaxFiles is the total number of files to
	// keep (active + .1 .2 ...). 0 in either field disables
	// rotation (default). / v0.4：基于大小的输出轮转。
	// RotateMaxBytes 是单文件大小阈值；现行 sink 跨过时关闭
	// 改名为 <path>.1 并开新 <path>。RotateMaxFiles 是总
	// 保留文件数（active + .1 .2 ...）。任一字段 0 关闭
	// 轮转（默认）。
	RotateMaxBytes int64
	RotateMaxFiles int

	// v0.9: discovery / shares / ftp / servers sink paths and the
	// hostname-enrichment hook (see Output.hostnameLookup).
	// / v0.9：discovery / shares / ftp / servers sink 路径与主机名充实
	// 钩子（见 Output.hostnameLookup）。
	DiscoveryJSONPath string // empty = no discovery output
	DiscoveryTXTPath  string // empty = no discovery output
	SharesJSONPath    string // empty = no shares output
	SharesTXTPath     string // empty = no shares output
	FTPJSONPath       string // empty = no ftp output
	FTPTXTPath        string // empty = no ftp output
	ServersJSONPath   string // empty = no servers output
	ServersTXTPath    string // empty = no servers output
	HostnameLookup    func(host string) string
}

// SetHostnameLookup installs the hostname-enrichment hook used by the
// servers inventory (scope tracker NBNS names). NOT concurrency-safe:
// call once during pipeline assembly, before the result sink starts —
// after that the field is read-only. / SetHostnameLookup 安装 servers
// 清单用的主机名充实钩子（scope 追踪器的 NBNS 名）。非并发安全：管
// 线装配时调用一次，结果汇启动前——之后该字段只读。
func (o *Output) SetHostnameLookup(fn func(host string) string) {
	o.hostnameLookup = fn
}

// OpenOutput opens (creates if needed) the configured output files and
// returns a writer that is safe for concurrent use.
//
// OpenOutput 打开（如不存在则创建）配置指定的输出文件，返回并发安全的 writer。
func OpenOutput(cfg OutputConfig) (*Output, error) {
	o := &Output{showCleartext: cfg.ShowCleartext, hostnameLookup: cfg.HostnameLookup}
	type opener struct {
		path string
		perm os.FileMode
		set  func(*flushCloser)
	}
	openers := []opener{
		{cfg.ResultTXTPath, 0o644, func(w *flushCloser) { o.txt = w }},
		{cfg.ResultJSONPath, 0o644, func(w *flushCloser) {
			o.jsn = w
			// Allocate json.Encoder once; reused per WriteResult (L-1).
			// / 一次性分配 json.Encoder；WriteResult 复用（L-1）。
			o.jsnEnc = json.NewEncoder(w.bw())
		}},
		{cfg.CredsPath, 0o600, func(w *flushCloser) { o.creds = w }},
		{cfg.RDPJSONPath, 0o644, func(w *flushCloser) { o.rdpjson = w }},
		{cfg.RDPTXTPath, 0o644, func(w *flushCloser) { o.rdptxt = w }},
		{cfg.WebJSONPath, 0o644, func(w *flushCloser) { o.webjson = w }},
		{cfg.WebTXTPath, 0o644, func(w *flushCloser) { o.webtxt = w }},
		{cfg.ResultCSVPath, 0o644, func(w *flushCloser) {
			o.csv = w
			// Allocate csv.Writer once; reused per WriteResult.
			// / 一次性分配 csv.Writer；WriteResult 复用。
			o.csvWriter = csv.NewWriter(w.bw())
		}},
		{cfg.ResultSARIFPath, 0o644, func(w *flushCloser) { o.sarif = w }},
		{cfg.DiscoveryJSONPath, 0o644, func(w *flushCloser) { o.discjson = w }},
		{cfg.DiscoveryTXTPath, 0o644, func(w *flushCloser) { o.disctxt = w }},
		{cfg.SharesJSONPath, 0o644, func(w *flushCloser) { o.sharesjson = w }},
		{cfg.SharesTXTPath, 0o644, func(w *flushCloser) { o.sharestxt = w }},
		{cfg.FTPJSONPath, 0o644, func(w *flushCloser) { o.ftpjson = w }},
		{cfg.FTPTXTPath, 0o644, func(w *flushCloser) { o.ftptxt = w }},
		{cfg.ServersJSONPath, 0o644, func(w *flushCloser) { o.serversjson = w }},
		{cfg.ServersTXTPath, 0o644, func(w *flushCloser) { o.serverstxt = w }},
		{cfg.ResultAlivePath, 0o644, func(w *flushCloser) {
			o.alive = w
			// Wire alive list format from config (default = "txt"
			// preserves v0.5.1 behaviour). Empty string from CLI
			// is normalised here. / 从配置 wire alive-list 格式
			// （默认 "txt" 保留 v0.5.1 行为）。CLI 来的空串
			// 在此归一化。
			if cfg.AliveFormat == "" {
				o.AliveFormat = "txt"
			} else {
				o.AliveFormat = cfg.AliveFormat
			}
			// Lazy-allocate the dedup map now that we have an
			// alive sink. nil-safe on the dead path (no alive
			// configured). / 既然开了 alive sink，现在懒分配去重
			// map。alive 没配置时 nil-safe。
			o.aliveSeen = make(map[string]struct{})
		}},
	}
	for _, op := range openers {
		if op.path == "" {
			continue
		}
		// v0.4 Phase 2.3: size-based output rotation. When
		// RotateMaxBytes > 0 and RotateMaxFiles > 0, the
		// active sink is wrapped in a rotatingWriter that
		// auto-rolls at the size cap. 0 in either field
		// disables rotation (default). / v0.4 Phase 2.3：基于
		// 大小的输出轮转。RotateMaxBytes > 0 且 RotateMaxFiles > 0
		// 时，现行 sink 包到 rotatingWriter 里，跨过大小阈值
		// 自动滚动。任一字段为 0 关闭轮转（默认）。
		rw, err := newRotatingWriter(op.path, op.perm, cfg.RotateMaxBytes, cfg.RotateMaxFiles)
		if err != nil {
			_ = o.Close()
			return nil, err
		}
		op.set(&flushCloser{rw: rw})
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
		{o.webjson, &o.webjsonMu, "web.json"},
		{o.webtxt, &o.webtxtMu, "web.txt"},
		{o.csv, &o.csvMu, "csv"},
		{o.alive, &o.aliveMu, "alive"},
		// SARIF goes last: assemble the document from o.sarifBuf,
		// write it, then close. / SARIF 放最后：从 o.sarifBuf 组装
		// 文档、写入、再关闭。
		{o.sarif, &o.sarifMu, "sarif"},
		// servers 也放最后：先按 host 聚合输出，再关闭。
		// / servers also goes last: emit the per-host aggregation
		// first, then close.
		{o.serversjson, &o.serversMu, "servers.json"},
		{o.serverstxt, &o.serversMu, "servers.txt"},
		{o.discjson, &o.discMu, "discovery.json"},
		{o.disctxt, &o.discMu, "discovery.txt"},
		{o.sharesjson, &o.sharesMu, "shares.json"},
		{o.sharestxt, &o.sharesMu, "shares.txt"},
		{o.ftpjson, &o.ftpMu, "ftp.json"},
		{o.ftptxt, &o.ftpMu, "ftp.txt"},
	}
	var firstErr error
	for _, c := range closers {
		if c.w == nil {
			continue
		}
		c.mu.Lock()
		var err error
		if c.label == "sarif" {
			// Emit the assembled SARIF document before close.
			// / 关闭前先发出组装好的 SARIF 文档。
			err = writeSARIFDocument(c.w, o.sarifBuf)
			if err != nil {
				err = fmt.Errorf("write sarif: %w", err)
			} else {
				err = c.w.Close()
			}
		} else if c.label == "servers.json" || c.label == "servers.txt" {
			// Emit the aggregated server inventory before close.
			// / 关闭前先输出按 host 聚合的服务器清单。
			err = o.writeServersLocked(c.w, c.label)
			if err != nil {
				err = fmt.Errorf("write %s: %w", c.label, err)
			} else {
				err = c.w.Close()
			}
		} else {
			err = c.w.Close()
		}
		// Sweep untouched sinks: a scan that produced no creds / no
		// RDP fingerprints / no results for a given format used to
		// leave a zero-byte file behind, which read as "scan output
		// is missing" to anyone inspecting the workspace (the exact
		// confusion that triggered the cleanup request). Deleting
		// only size-0 files keeps fail-fast OpenOutput semantics
		// (unwritable paths still error at startup) and rotation
		// intact — a rotated sink always has bytes. / 清扫未写过
		// 的 sink：没有凭据 / RDP 指纹 / 某格式结果的扫描此前会留
		// 下 0 字节文件，检查工作区的人会误读为"扫描没输出"（正
		// 是引发清理诉求的困惑）。只删 size-0 文件：OpenOutput 的
		// fail-fast 语义（路径不可写启动即报错）与轮转机制都不
		// 受影响——轮转过的文件必有字节。
		if err == nil && c.w.rw != nil && c.w.rw.path != "" {
			if fi, statErr := os.Stat(c.w.rw.path); statErr == nil && fi.Size() == 0 {
				if rmErr := os.Remove(c.w.rw.path); rmErr != nil && firstErr == nil {
					firstErr = fmt.Errorf("remove empty %s sink: %w", c.label, rmErr)
				}
			}
		}
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
		{o.webjson, &o.webjsonMu},
		{o.webtxt, &o.webtxtMu},
		{o.csv, &o.csvMu},
		{o.alive, &o.aliveMu},
		{o.discjson, &o.discMu},
		{o.disctxt, &o.discMu},
		{o.sharesjson, &o.sharesMu},
		{o.sharestxt, &o.sharesMu},
		{o.ftpjson, &o.ftpMu},
		{o.ftptxt, &o.ftpMu},
		// servers buffers until Close — nothing to flush mid-run.
		// / servers 缓冲到 Close 才输出——中途无可刷。
	}
	var firstErr error
	for _, f := range flushers {
		if f.w == nil || f.w.bw() == nil {
			continue
		}
		f.mu.Lock()
		err := f.w.bw().Flush()
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
	// v0.4: SARIF buffer (single-doc emission at Close). / v0.4：SARIF
	// buffer（在 Close 一次性发出文档）。
	if o.sarif != nil {
		o.sarifMu.Lock()
		o.sarifBuf = append(o.sarifBuf, r)
		o.sarifMu.Unlock()
	}
	// v0.9: fold role-tagged results into the important-server
	// inventory (no-op unless r.Important is set and a servers sink is
	// open). / v0.9：把带角色标记的结果折叠进重要服务器清单（无角色
	// 或 servers sink 未开时是 no-op）。
	o.collectServers(r)
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
		// Stamp the NDJSON contract version on the emitted line (not
		// on r — the pooled object and the persisted store record stay
		// free of a per-row constant). / 在写出行上盖 NDJSON 契约版本
		//（不动 r——池化对象与持久化 store 记录不携带每行相同常量）。
		if out.Schema == 0 {
			out.Schema = types.SchemaNDJSON
		}
		_ = o.jsnEnc.Encode(out)
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
	// Alive-host list: append r.Host if this is the first time we've
	// seen it this run. Dedup'd under aliveMu so concurrent workers
	// don't double-write. Empty r.Host is a defensive no-op (would
	// produce a stray blank line that breaks `nmap -iL`). / 存活
	// 主机列表：若本次 run 首次见到 r.Host 则追加。aliveMu 守护
	// 去重，并发 worker 不会双写。空 r.Host 是防御性 no-op（会
	// 产生空行，破坏 `nmap -iL`）。
	o.writeAlive(r)
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
		// Banner is attacker-controlled (remote title / Server header) —
		// neutralize spreadsheet formula injection (OWASP CSV Injection).
		// Banner 是攻击者可控的（远程 title / Server 头）——中和表格
		// 公式注入（OWASP CSV 注入）。
		neutralizeCSVFormula(truncateForCSV(r.Banner, 1024)),
		user,
		pass,
		r.Product,
		r.Version,
		r.Confidence,
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

// WriteAliveDiscovery records a host confirmed alive by the
// discovery stage (before any port scan). Previously the alive sink
// was only fed by WriteResult, so on firewalled networks — where most
// alive hosts yield no open port — fgqm_alive stayed empty even
// though 150+ hosts had responded. Dedup is shared with the
// WriteResult path (o.aliveSeen), so a host later confirmed by an
// open-port result is written once, with the discovery entry winning
// (first write) and port/service fields coming from the open result
// only when it arrives first.
//
// WriteAliveDiscovery 记录存活发现阶段（端口扫描之前）确认存活的主
// 机。此前 alive sink 只由 WriteResult 喂数据，在防火墙网络——多数
// 存活主机没有开放端口——fgqm_alive 始终为空，即使 150+ 台主机已经
// 响应。去重与 WriteResult 路径共享（o.aliveSeen），后续被 open 端
// 口结果确认的同一主机不会重复写入（发现条目先写先占位）。
func (o *Output) WriteAliveDiscovery(host, method string, at time.Time) {
	o.writeAlive(&types.Result{Host: host, Port: 0, Service: method, Time: at})
}

// writeAlive appends r to the alive-host sink in the configured
// wire format. No-op when no sink is configured or when r.Host is
// empty (defensive — would otherwise produce a stray blank line
// that breaks `nmap -iL`). The dedup happens here (under
// aliveMu) so concurrent workers can't double-write the same
// host. / writeAlive 按配置的线协议格式把 r 追加到 alive-host
// sink。没配置 sink 或 r.Host 为空时是 no-op（防御性——否则会
// 产生空行破坏 `nmap -iL`）。去重在此（由 aliveMu 守护），并发
// worker 不会双写同一 host。
//
// Formats:
//   - "txt" (default): `1.2.3.4\n` — one host per line.
//   - "json": `{"host":"1.2.3.4","port":22,"service":"ssh","time":"..."}\n` per line.
//   - "csv": `host,port,service,time\n1.2.3.4,22,ssh,...\n` — header
//     written on first call via a dedicated csv.Writer (allocated
//     lazily on first row).
//
// / 格式：
//   - "txt"（默认）：`1.2.3.4\n` —— 每行一个 host。
//   - "json"：每行 `{"host":"...","port":22,"service":"ssh","time":"..."}\n`。
//   - "csv"：`host,port,service,time\n1.2.3.4,22,ssh,...\n` —— header
//     在首次调用时通过专用 csv.Writer（首次 row 懒分配）写入。
func (o *Output) writeAlive(r *types.Result) {
	if o.alive == nil || r.Host == "" {
		return
	}
	o.aliveMu.Lock()
	defer o.aliveMu.Unlock()
	if _, dup := o.aliveSeen[r.Host]; dup {
		return
	}
	o.aliveSeen[r.Host] = struct{}{}
	switch o.AliveFormat {
	case "json":
		ts := r.Time.Format("2006-01-02T15:04:05Z07:00")
		fmt.Fprintf(o.alive, `{"host":%q,"port":%d,"service":%q,"time":%q}`+"\n",
			r.Host, r.Port, r.Service, ts)
	case "csv":
		// Lazily allocate the csv writer so we don't pay the
		// allocation cost when the alive sink isn't configured
		// or is in txt/json mode. / 懒分配 csv writer——alive sink
		// 没配置或 txt/json 模式下不付出分配成本。
		if o.aliveCSVWriter == nil {
			o.aliveCSVWriter = csv.NewWriter(o.alive.bw())
			_ = o.aliveCSVWriter.Write([]string{"host", "port", "service", "time"})
		}
		ts := r.Time.Format("2006-01-02T15:04:05Z07:00")
		_ = o.aliveCSVWriter.Write([]string{r.Host, strconv.Itoa(r.Port), r.Service, ts})
		o.aliveCSVWriter.Flush()
	default: // "txt" or "" — backward-compatible default
		fmt.Fprintln(o.alive, r.Host)
	}
}

// WriteRDP writes a structured RDP fingerprint to rdp.json (NDJSON) and
// rdp.txt (human-readable). Each file has its own mutex.
//
// The type itself lives in types (types.RDPFingerprint): the rdp plugin
// produces it into Result.Extra, and output only renders it — keeping
// the plugin layer free of an output dependency.
// / WriteRDP 把结构化的 RDP 指纹写入 rdp.json（NDJSON）和 rdp.txt
// （人类可读）。每个文件独立 mutex。类型本体在 types
// （types.RDPFingerprint）：rdp 插件把它放进 Result.Extra，output 只
// 负责渲染——插件层因此无需依赖 output。
func (o *Output) WriteRDP(fp types.RDPFingerprint) error {
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

// WriteWeb writes a structured web fingerprint to web.json (NDJSON) and
// web.txt (human-readable). Each file has its own mutex.
//
// The type itself lives in types (types.WebFingerprint): the webtitle
// plugin produces it into Result.Extra, and output only renders it.
// TLS fields are empty for http targets and simply don't appear in the
// JSON (omitempty) / render as empty key= pairs in the txt line.
//
// / WriteWeb 把结构化的 Web 指纹写入 web.json（NDJSON）和 web.txt
// （人类可读）。每个文件独立 mutex。类型本体在 types
// （types.WebFingerprint）：webtitle 插件把它放进 Result.Extra，
// output 只负责渲染。http 目标的 TLS 字段为空，JSON 中直接不出现
// （omitempty），txt 行渲染为空的 key= 对。
func (o *Output) WriteWeb(fp types.WebFingerprint) error {
	if o.webjson != nil {
		o.webjsonMu.Lock()
		enc := json.NewEncoder(o.webjson)
		_ = enc.Encode(fp)
		o.webjsonMu.Unlock()
	}
	if o.webtxt != nil {
		o.webtxtMu.Lock()
		ts := fp.ScanTime.Format("2006-01-02 15:04:05")
		fmt.Fprintf(o.webtxt,
			"[%s] %s  [%d/%d] title=%q server=%q fps=%v cert-subject=%q cert-issuer=%q cert-san=%v tls=%q\n",
			ts, fp.URL, fp.StatusCode, fp.ContentLen,
			fp.Title, fp.Server, fp.Fingers,
			fp.CertSubject, fp.CertIssuer, fp.CertSANs, fp.TLSVersion)
		o.webtxtMu.Unlock()
	}
	return nil
}
