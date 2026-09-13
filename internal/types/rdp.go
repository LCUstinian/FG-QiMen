// rdp.go — RDPFingerprint, the structured payload of an RDP deep
// fingerprint, shared by the rdp plugin (producer), the pipeline sink
// (dispatcher) and the output package (renderer).
// rdp.go — RDPFingerprint，RDP 深指纹的结构化 payload，由 rdp 插件
// （生产者）、管线 sink（分发者）与 output 包（渲染者）共享。
//
// LAYERING NOTE: this type lives in types — NOT in output — because
// the rdp plugin stashes it in types.Result.Extra. If it lived in
// output, the plugin layer would have to import the output layer
// (plugins → output inversion) just to set one field. types is the
// leaf every layer already imports, so it is the natural home.
//
// 分层说明：本类型放在 types 而非 output——rdp 插件要把它塞进
// types.Result.Extra；若放在 output，插件层就得 import 输出层
// （plugins → output 倒置）只为设一个字段。types 是所有层都已
// 依赖的叶子包，是它的自然归宿。
package types

import "time"

// RDPFingerprint is the extended RDP fingerprint structure that gets
// persisted to dedicated rdp.json / rdp.txt files (beyond the regular
// result stream). The pipeline sink type-asserts Result.Extra to
// *RDPFingerprint and hands it to output.Output.WriteRDP.
//
// RDPFingerprint 是持久化到专用 rdp.json / rdp.txt 的扩展 RDP 指纹
// 结构（超出常规 result 流）。管线 sink 对 Result.Extra 做
// *RDPFingerprint 断言后交给 output.Output.WriteRDP。
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
