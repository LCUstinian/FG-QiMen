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

// ScanItem is the unit of work emitted by the port scan producer and
// consumed by plugin workers.
// ScanItem 是端口扫描 producer 发出、被 plugin worker 消费的工作单位。
type ScanItem struct {
	Host string
	Port int
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
}

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
	resultPool.Put(r)
}
