// tlsinfo.go — leaf-certificate identity extraction for https targets.
//
// The http response already carries the TLS connection state
// (resp.TLS) — the standard Transport fills PeerCertificates even
// with InsecureSkipVerify (verification is skipped, collection is
// not). So certificate identity costs ZERO extra connections: no
// second handshake, no separate TLS dial.
//
// tlsinfo.go — https 目标的叶子证书身份提取。
//
// http 响应本身携带 TLS 连接状态（resp.TLS）——标准 Transport 即使
// 在 InsecureSkipVerify 下也会填 PeerCertificates（跳过的是验证，
// 不是收集）。因此证书身份零额外连接：不二次握手、不单独拨 TLS。
package webtitle

import (
	"crypto/tls"
)

// tlsVersionNames maps the wire constants to operator-readable names.
// / tlsVersionNames 把协议常量映射成操作员可读的名字。
var tlsVersionNames = map[uint16]string{
	tls.VersionTLS10: "TLS 1.0",
	tls.VersionTLS11: "TLS 1.1",
	tls.VersionTLS12: "TLS 1.2",
	tls.VersionTLS13: "TLS 1.3",
}

// tlsInfo is the extracted certificate identity. Empty strings mean
// "no certificate / not https" — the caller just leaves the
// corresponding types.WebFingerprint fields empty.
//
// tlsInfo 是提取出的证书身份。空串表示"无证书 / 非 https"——调用方
// 直接让 types.WebFingerprint 对应字段留空。
type tlsInfo struct {
	Subject   string
	Issuer    string
	SANs      []string
	ValidFrom string
	ValidTo   string
	Version   string
}

// extractTLSInfo pulls the leaf-certificate identity out of a
// connection state. Returns the zero tlsInfo when the handshake
// carried no certificates (anonymous ciphersuite, TLS-in-TLS tunnel,
// etc). Time formatting is RFC 3339 date-only (YYYY-MM-DD) so the
// web.txt column stays grep-aligned.
//
// extractTLSInfo 从连接状态抽叶子证书身份。握手无证书（匿名套件、
// TLS-in-TLS 隧道等）时返回零值。时间格式 RFC 3339 仅日期
// （YYYY-MM-DD），web.txt 列保持 grep 对齐。
func extractTLSInfo(cs *tls.ConnectionState) tlsInfo {
	if cs == nil || len(cs.PeerCertificates) == 0 {
		return tlsInfo{}
	}
	cert := cs.PeerCertificates[0]
	out := tlsInfo{
		Issuer:    cert.Issuer.CommonName,
		ValidFrom: cert.NotBefore.Format("2006-01-02"),
		ValidTo:   cert.NotAfter.Format("2006-01-02"),
	}
	// Subject: CommonName first. SAN-only certs (modern LE chains,
	// httptest's local cert) have no CN — fall back to the serialized
	// RDN string so the identity isn't lost. / Subject：先取
	// CommonName。SAN-only 证书（现代 LE 链、httptest 本地证书）没有
	// CN——回退到序列化 RDN 串，避免身份信息丢失。
	if out.Subject = cert.Subject.CommonName; out.Subject == "" {
		out.Subject = cert.Subject.String()
	}
	if out.Issuer == "" {
		out.Issuer = cert.Issuer.String()
	}
	// SAN DNS names, self-lookup deduped (a leaf cert often lists its
	// own CN again). IP SANs are dropped: the operator already knows
	// the scanned IP; hostnames are the signal. / SAN DNS 名，去重
	//（叶子证书常把自身 CN 再列一遍）。IP SAN 丢弃：操作员本就知道
	// 被扫 IP；主机名才是信号。
	seen := make(map[string]struct{}, len(cert.DNSNames))
	for _, name := range cert.DNSNames {
		if name == "" || name == out.Subject {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out.SANs = append(out.SANs, name)
	}
	if v, ok := tlsVersionNames[cs.Version]; ok {
		out.Version = v
	}
	return out
}
