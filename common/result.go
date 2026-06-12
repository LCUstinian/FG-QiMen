// result.go — Result / Cred / ScanItem types flowing through the pipeline.
// result.go — 管线中流转的 Result / Cred / ScanItem 类型。
package common

import "time"

// Cred is a single (user, pass) credential pair to test.
// Cred 是单个 (user, pass) 待测凭据对。
type Cred struct {
	User     string
	Pass     string
	AuthType string // "password" / "key" / ...
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
	// (e.g. portfinger).
	// / Banner 是端口开放后立即收到的原始字节（最多 256 字节，存为
	// 字符串）；未捕获则为空。插件可拿来跑服务指纹识别（如 portfinger）。
	Banner string
}

// Result is a single result emitted by a plugin (Identify or Credential).
// Result 是插件（Identify 或 Credential）发出的单个结果。
type Result struct {
	Time    time.Time `json:"time"`
	Project string    `json:"project,omitempty"`
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	Service string    `json:"service"`
	Plugin  string    `json:"plugin"`
	Banner  string    `json:"banner,omitempty"`
	Extra   string    `json:"extra,omitempty"`
	Cred    *Cred     `json:"cred,omitempty"`
}
