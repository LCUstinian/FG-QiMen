// result.go — Result / Cred / ScanItem types flowing through the pipeline.
// result.go — 管线中流转的 Result / Cred / ScanItem 类型。
package types

import (
	"sync"
	"time"
)

// Cred is a single (user, pass) credential pair to test.
// Cred 是单个 (user, pass) 待测凭据对。
type Cred struct {
	User     string `json:"user"`
	Pass     string `json:"pass"`
	AuthType string `json:"auth_type,omitempty"` // "password" / "key" / ...
}

// Protocol values for ScanItem.Protocol. The scan layer produces
// them and the plugin-routing layer compares against them; use the
// constants instead of string literals so the scan→plugin contract
// has a single source of truth.
// / ScanItem.Protocol 的取值。扫描层产出、插件路由层比对；用常量
// 替代字符串字面量，让 scan→plugin 契约只有一个真相源。
const (
	ProtocolTCP = "tcp" // historic default / 历史默认
	ProtocolUDP = "udp"
)

// ScanItem is the unit of work emitted by the port scan producer and
// consumed by plugin workers.
// ScanItem 是端口扫描 producer 发出、被 plugin worker 消费的工作单位。
type ScanItem struct {
	Host string
	Port int
	// Protocol selects the transport for this item: "" or "tcp" (the
	// historic default) or "udp". UDP items are produced by the UDP
	// scan phase and must be routed to UDP-aware fingerprinting
	// (VScan.MatchUDPBanner) and UDP-only plugins.
	// / Protocol 选择该项的传输层："" 或 "tcp"（历史默认），或 "udp"。
	// UDP 项由 UDP 扫描阶段产出，必须路由到 UDP 感知的指纹识别
	//（VScan.MatchUDPBanner）与 UDP-only 插件。
	Protocol string
	// Banner is the raw bytes (as a string) received right after the
	// port open (up to 256 bytes). Empty if the probe did not
	// capture one. Plugins can use this for service fingerprinting
	// (e.g. fingerprint).
	// / Banner 是端口开放后立即收到的原始字节（最多 256 字节，存为
	// 字符串）；未捕获则为空。插件可拿来跑服务指纹识别（如 fingerprint）。
	Banner string
}

// Result is a single result emitted by a plugin (Identify or Credential).
// Result 是插件（Identify 或 Credential）发出的单个结果。
//
// Extra is a typed side-channel: a plugin can set it to any structured
// payload it wants to pass to the result sink (e.g. *output.RDPFingerprint from
// the RDP plugin). The pipeline type-asserts known shapes (output.RDPFingerprint
// → rdp.json/rdp.txt) and silently ignores unknown types. Was `string`
// before; repurposed to `any` because no consumer was using the string
// form (verified by `grep -rE "\.Extra\s*="`) and the structured
// side-channel is needed for v0.1 RDP deep fingerprint.
//
// Extra 是类型化旁路：插件可以塞任何结构化 payload 传给 result sink
// （如 RDP 插件的 *output.RDPFingerprint）。管线对已知类型做 type-assert
// （output.RDPFingerprint → rdp.json/rdp.txt），未知类型静默忽略。原本
// 是 `string`；改为 `any` 是因为代码库无人用 string 形式
// （grep -rE "\.Extra\s*=" 验证），且 v0.1 RDP 深指纹需要结构化旁路。
type Result struct {
	Time    time.Time `json:"time"`
	Project string    `json:"project,omitempty"`
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	Service string    `json:"service"`
	Plugin  string    `json:"plugin"`
	Banner  string    `json:"banner,omitempty"`
	Extra   any       `json:"extra,omitempty"`
	Cred    *Cred     `json:"cred,omitempty"`
	// Product / Version are the structured identity extracted by the
	// nmap-style banner fingerprint (p/.../ v/.../ segments with $N
	// substitution) or a plugin's protocol handshake. All omitempty so
	// unknown-service results keep byte-identical NDJSON output.
	// / Product / Version 是 nmap 风格 banner 指纹（p/.../ v/.../ 分段
	// 加 $N 子匹配替换）或插件协议握手提取出的结构化身份。全部
	// omitempty：未知服务的 NDJSON 输出与既往逐字节一致。
	Product string `json:"product,omitempty"`
	Version string `json:"version,omitempty"`
	// Confidence grades the identity claim: ConfHigh = authoritative
	// (nmap hard match or real protocol handshake), ConfLow = hint
	// (nmap softmatch fallback). Empty when nothing is known.
	// / Confidence 给身份断言分级：ConfHigh = 权威（nmap 硬匹配或真
	// 协议握手），ConfLow = 提示（nmap softmatch 兜底）。未知时为空。
	Confidence string `json:"confidence,omitempty"`
	// FpProbe / FpPattern record the identification BASIS of an
	// nmap-style banner match: which probe's rule fired and the
	// verbatim upstream rule text (nmap-service-probes.txt line
	// content, escape-faithful). They turn every identity claim into
	// auditable evidence — "Identify, not just connect" extends to
	// "identify, and show your work". Plugin-driven identity keeps
	// both empty: the Plugin field already names that source.
	// / FpProbe / FpPattern 记录 nmap 风格 banner 命中的识别依据：哪
	// 个探针的哪条规则命中，规则文本逐字保留上游原文（转义保真）。
	// 每条身份断言因此自带可审计证据——「识别而非仅仅连通」延伸为
	// 「识别，并展示依据」。插件驱动的识别两者留空：Plugin 字段已
	// 说明来源。
	FpProbe   string `json:"fp_probe,omitempty"`
	FpPattern string `json:"fp_pattern,omitempty"`
	// Important carries the infrastructure roles the result-sink rule
	// table attributed to this host:port ("domain-controller",
	// "backup-server", ...). Empty for ordinary services. It turns the
	// "important server" classification into per-record evidence that
	// survives into NDJSON instead of living only in the servers
	// report. / Important 携带结果汇规则表给该 host:port 归类的基础
	// 设施角色（"domain-controller"、"backup-server"……）。普通服务
	// 为空。重要服务器分类因此成为随 NDJSON 存活的逐记录证据，而不
	// 只活在 servers 报告里。
	Important []string `json:"important,omitempty"`
	// Schema is the NDJSON contract version of the record. 0 means
	// legacy/unversioned (pre-schema files); the writer stamps
	// SchemaNDJSON on every line it emits, so line-oriented consumers
	// can detect which shape they read even from partial reads. It is
	// deliberately NOT set at construction: the persisted store
	// records and the redaction copy path stay free of a constant
	// that would be identical on every row.
	// / Schema 是该记录的 NDJSON 契约版本。0 = 旧版/无版本（schema
	// 之前的文件）；写出端在每行盖上 SchemaNDJSON，使行式消费方即便
	// 只读到部分行也能识别数据形状。刻意不在构造期赋值：持久化
	// store 记录与脱敏拷贝路径不携带每行都相同的常量。
	Schema int `json:"schema,omitempty"`
}

// SchemaNDJSON is the current NDJSON record contract version.
// History: 0 = pre-schema (no fingerprint evidence fields, no
// schema); 1 = adds fp_probe / fp_pattern evidence fields; 2 = adds
// the important (infrastructure roles) evidence field.
// / SchemaNDJSON 是当前 NDJSON 记录契约版本。历史：0 = 无 schema
// （无指纹证据字段）；1 = 新增 fp_probe / fp_pattern 证据字段；
// 2 = 新增 important（基础设施角色）证据字段。
const SchemaNDJSON = 2

// Confidence vocabulary for Result.Confidence. / Result.Confidence 的
// 置信度取值。
const (
	ConfHigh = "high" // nmap hard match / real protocol handshake / nmap 硬匹配或真协议握手
	ConfLow  = "low"  // nmap softmatch fallback / nmap softmatch 兜底
)

// resultPool reuses Result objects to reduce GC pressure.
// resultPool 复用 Result 对象以减少 GC 压力。
var resultPool = sync.Pool{
	New: func() any {
		return &Result{}
	},
}

// GetResult retrieves a Result from the pool.
// GetResult 从池中获取 Result。
func GetResult() *Result {
	return resultPool.Get().(*Result)
}

// PutResult returns a Result to the pool after resetting fields.
// PutResult 重置字段后把 Result 归还池中。
func PutResult(r *Result) {
	if r == nil {
		return
	}
	r.Time = time.Time{}
	r.Project = ""
	r.Host = ""
	r.Port = 0
	r.Service = ""
	r.Plugin = ""
	r.Banner = ""
	r.Extra = nil
	r.Cred = nil
	r.Product = ""
	r.Version = ""
	r.Confidence = ""
	r.FpProbe = ""
	r.FpPattern = ""
	r.Important = nil
	r.Schema = 0
	resultPool.Put(r)
}
