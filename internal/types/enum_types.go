// enum_types.go — read-only enumeration payloads (SMB shares, FTP
// directory trees) and the scope-discovery event, shared by the
// producing plugins (producers), the pipeline sink (dispatcher) and
// the output package (renderers).
// enum_types.go — 只读枚举载荷（SMB 共享、FTP 目录树）与范围发现事件，
// 由生产插件（生产者）、管线 sink（分发者）与 output 包（渲染者）共享。
//
// LAYERING NOTE: same pattern as types.RDPFingerprint / types.WebFingerprint —
// these types live in types, NOT output, so the producing plugins never
// import the output layer. The pipeline sink type-asserts Result.Extra
// and hands the payload to the matching output.Write* method.
//
// 分层说明：与 types.RDPFingerprint / types.WebFingerprint 同一模式
// ——类型放 types 而非 output，生产插件因此不依赖输出层。管线 sink
// 对 Result.Extra 做类型断言后交给对应的 output.Write* 方法。
//
// EVIDENCE BOUNDARY (v0.9): enumeration is READ-ONLY evidence
// collection — listing share names, directory entries and metadata.
// Downloading file CONTENT, writing, deleting or executing is attack
// territory and stays forbidden. / 取证边界（v0.9）：枚举是只读取证
// ——列共享名、目录条目与元数据。下载文件内容、写入、删除、执行属
// 攻击行为，仍然禁止。
package types

import "time"

// EnumLimits caps a single enumeration run so a huge NAS share or a
// deep FTP tree can neither stall the AIMD worker pool nor balloon the
// evidence file. Defaults live in internal/core constants.
// EnumLimits 限制单次枚举的规模，防止巨型 NAS 共享或深层 FTP 目录树
// 拖垮 AIMD worker 池或撑爆证据文件。默认值在 internal/core 常量里。
type EnumLimits struct {
	MaxDepth    int           `json:"max_depth"`
	MaxEntries  int           `json:"max_entries"`
	HostTimeout time.Duration `json:"host_timeout"`
}

// EnumFileInfo is one file/directory entry captured during a read-only
// walk. / EnumFileInfo 是只读遍历中捕获的单个文件/目录条目。
type EnumFileInfo struct {
	Name  string    `json:"name"`
	Size  int64     `json:"size,omitempty"`
	IsDir bool      `json:"is_dir"`
	MTime time.Time `json:"mtime,omitempty"`
}

// ShareInfo is one SMB share probed via a null session. Access records
// what the ANONYMOUS session could do — "list" means the share mounted
// and its top level was enumerable. Write access is deliberately NOT
// tested (that would be modification = attack). / ShareInfo 是通过
// null session 探测的单个 SMB 共享。Access 记录匿名会话能做什么
// ——"list" 表示共享可挂载且顶层可枚举。写权限刻意不测（写入=修改
// =攻击）。
type ShareInfo struct {
	Name      string         `json:"name"`
	Access    string         `json:"access"` // "none" / "list"
	Remark    string         `json:"remark,omitempty"`
	Files     []EnumFileInfo `json:"files,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ShareEnumResult is the Extra payload of an SMB null-session
// enumeration, dual-written to shares.ndjson / shares.txt.
// ShareEnumResult 是 SMB 匿名枚举的 Extra 载荷，双写到
// shares.ndjson / shares.txt。
type ShareEnumResult struct {
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Anonymous bool        `json:"anonymous"` // null session succeeded / null session 成功
	Shares    []ShareInfo `json:"shares"`
	Truncated bool        `json:"truncated,omitempty"`
	Limits    EnumLimits  `json:"limits"`
	ScanTime  time.Time   `json:"scan_time"`
}

// FTPDir is one directory level of an FTP walk. Perms carry the raw
// LIST permission string (e.g. "drwxr-xr-x") when the server prints
// one. / FTPDir 是 FTP 遍历的单层目录。Perms 携带服务器打印的原始
// LIST 权限串（如 "drwxr-xr-x"）（有打印时）。
type FTPDir struct {
	Path      string         `json:"path"`
	Perms     string         `json:"perms,omitempty"`
	Files     []EnumFileInfo `json:"files,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// FTPEnumResult is the Extra payload of an FTP read-only directory
// walk (anonymous or a weak-credential hit), dual-written to
// ftp.ndjson / ftp.txt. It is deliberately SEPARATE from
// ShareEnumResult so share findings and FTP findings never merge into
// one report. / FTPEnumResult 是 FTP 只读目录遍历（匿名或弱口令命
// 中）的 Extra 载荷，双写到 ftp.ndjson / ftp.txt。刻意与
// ShareEnumResult 分开，让共享发现与 FTP 发现永不合并进同一报告。
type FTPEnumResult struct {
	Host      string     `json:"host"`
	Port      int        `json:"port"`
	Anonymous bool       `json:"anonymous"` // anonymous login / 匿名登录
	User      string     `json:"user,omitempty"`
	Dirs      []FTPDir   `json:"dirs"`
	Truncated bool       `json:"truncated,omitempty"`
	Limits    EnumLimits `json:"limits"`
	ScanTime  time.Time  `json:"scan_time"`
}

// DiscoveryEvent is one out-of-scope NIC identity captured from a
// protocol interaction (NBNS name table, TLS SAN, ...): the who
// (IP/MAC/hostname), the where-from (source protocol) and the when.
// It feeds fgqm_discovery.* and — with --expand-scope auto — the
// bounded second scan round. / DiscoveryEvent 是从协议交互（NBNS 名
// 字表、TLS SAN……）抓到的一条范围外网卡身份：是谁（IP/MAC/主机
// 名）、从哪来（来源协议）、何时发现。喂给 fgqm_discovery.*，并在
// --expand-scope auto 下喂给有界二轮扫描。
type DiscoveryEvent struct {
	Time           time.Time         `json:"time"`
	IP             string            `json:"ip,omitempty"`
	MAC            string            `json:"mac,omitempty"`
	Hostname       string            `json:"hostname,omitempty"`
	SourceProtocol string            `json:"source_protocol"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}
