// webfingerprint.go — WebFingerprint, the structured payload of the
// webtitle plugin, shared by the plugin (producer), the pipeline sink
// (dispatcher) and the output package (renderer).
// webfingerprint.go — WebFingerprint，webtitle 插件的结构化 payload，
// 由插件（生产者）、管线 sink（分发者）与 output 包（渲染者）共享。
//
// LAYERING NOTE: same pattern as types.RDPFingerprint — this type
// lives in types, NOT output, so the producing plugin never imports
// the output layer. The pipeline sink type-asserts Result.Extra to
// *WebFingerprint and hands it to output.Output.WriteWeb, which
// dual-writes web.json / web.txt.
//
// 分层说明：与 types.RDPFingerprint 同一模式——类型放 types 而非
// output，生产方插件因此不依赖输出层。管线 sink 对 Result.Extra
// 做 *WebFingerprint 断言后交给 output.Output.WriteWeb，双写到
// web.json / web.txt。
package types

import "time"

// WebFingerprint is the structured view of one webtitle Identify hit:
// the response facts (status / title / server / content length), the
// matched fingerprint names, and — for https targets — the leaf
// certificate identity (subject / SANs / issuer / validity). The
// certificate fields are the high-value addition for intranet work:
// SANs and CNs routinely leak internal hostnames and domains that
// banner matching alone never sees.
//
// WebFingerprint 是 webtitle Identify 命中的结构化视图：响应事实
// （status / title / server / 响应长度）、命中的指纹名，以及——对
// https 目标——叶子证书身份（subject / SAN / issuer / 有效期）。
// 证书字段是内网场景的高价值增量：SAN 和 CN 常会暴露仅靠 banner
// 匹配看不到的内网机器名与域名。
type WebFingerprint struct {
	URL        string   `json:"url"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Scheme     string   `json:"scheme"`
	StatusCode int      `json:"status_code"`
	Title      string   `json:"title,omitempty"`
	Server     string   `json:"server,omitempty"`
	ContentLen int      `json:"content_length"`
	Fingers    []string `json:"fingers,omitempty"`

	// TLS leaf-certificate identity, https targets only (http leaves
	// all of these empty). / TLS 叶子证书身份，仅 https 目标填写
	//（http 全部留空）。
	CertSubject   string   `json:"cert_subject,omitempty"`
	CertIssuer    string   `json:"cert_issuer,omitempty"`
	CertSANs      []string `json:"cert_sans,omitempty"`
	CertValidFrom string   `json:"cert_valid_from,omitempty"`
	CertValidTo   string   `json:"cert_valid_to,omitempty"`
	TLSVersion    string   `json:"tls_version,omitempty"`

	ScanTime time.Time `json:"scan_time"`
}
