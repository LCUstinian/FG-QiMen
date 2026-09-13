// tlsinfo_test.go — unit coverage for the TLS leaf-identity
// extraction. Certificates are generated in-memory (ECDSA P-256
// self-signed) — no fixture files, no network. The extraction path
// under test is the same one the Identify flow feeds from
// resp.TLS.PeerCertificates.
//
// tlsinfo_test.go — TLS 叶子身份提取的单测。证书内存生成（ECDSA
// P-256 自签）——无 fixture 文件、无网络。被测提取路径与 Identify
// 流程从 resp.TLS.PeerCertificates 喂入的是同一条。
package webtitle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// makeLeafCert builds an in-memory leaf signed by a freshly minted
// "Corp Root CA" CA (self-signed issuer), with the given leaf CN and
// DNS SANs (duplicates preserved on purpose — the dedup is part of
// what we assert). A real CA-issued leaf is needed because
// CreateCertificate fills the leaf's Issuer from the SIGNING cert's
// Subject, so a self-signed leaf would overwrite the intended issuer.
// / makeLeafCert 内存构造由新造的 "Corp Root CA" CA（自签 issuer）
// 签发的叶子证书，含给定 CN 与 DNS SAN（故意保留重复——去重本身
// 就是断言点）。必须用真 CA 签发：CreateCertificate 用签署方证书
// 的 Subject 填 Issuer，自签叶子会把预期 issuer 覆盖掉。
func makeLeafCert(t *testing.T, cn string, dnsNames []string) *x509.Certificate {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Corp Root CA"},
		NotBefore:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC),
		DNSNames:     dnsNames,
		IPAddresses:  []net.IP{net.ParseIP("192.168.7.10")},
	}
	// CN-less certs in the wild (httptest, org-issued chains) still
	// carry an Organization RDN — add one so the fallback has
	// something to surface. / 野生的无 CN 证书（httptest、组织签发
	// 链）仍带 Organization RDN——补一个让 fallback 有东西可给。
	if cn == "" {
		leafTmpl.Subject.Organization = []string{"Acme Co"}
	}
	der, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

// TestExtractTLSInfo_HappyPath asserts subject/issuer/SAN
// dedup (self-CN dropped, IP SAN dropped)/validity dates and the
// TLS version name. / TestExtractTLSInfo_HappyPath 断言
// subject/issuer/SAN 去重（自身 CN 丢弃、IP SAN 丢弃）、有效期日期
// 与 TLS 版本名。
func TestExtractTLSInfo_HappyPath(t *testing.T) {
	cert := makeLeafCert(t, "intranet-web.corp.local",
		[]string{"web01.corp.local", "intranet-web.corp.local", "web01.corp.local"})
	cs := &tls.ConnectionState{
		Version:          tls.VersionTLS12,
		PeerCertificates: []*x509.Certificate{cert},
	}
	got := extractTLSInfo(cs)
	if got.Subject != "intranet-web.corp.local" {
		t.Errorf("Subject = %q, want %q", got.Subject, "intranet-web.corp.local")
	}
	if got.Issuer != "Corp Root CA" {
		t.Errorf("Issuer = %q, want %q", got.Issuer, "Corp Root CA")
	}
	wantSANs := []string{"web01.corp.local"} // self-CN + dup + IP SAN dropped
	if len(got.SANs) != len(wantSANs) || got.SANs[0] != wantSANs[0] {
		t.Errorf("SANs = %v, want %v", got.SANs, wantSANs)
	}
	if got.ValidFrom != "2026-01-02" {
		t.Errorf("ValidFrom = %q, want 2026-01-02", got.ValidFrom)
	}
	if got.ValidTo != "2027-03-04" {
		t.Errorf("ValidTo = %q, want 2027-03-04", got.ValidTo)
	}
	if got.Version != "TLS 1.2" {
		t.Errorf("Version = %q, want TLS 1.2", got.Version)
	}
}

// TestExtractTLSInfo_CNLess pins the SAN-only fallback: a cert with
// no CommonName (modern LE chains, httptest's local cert) must still
// surface a subject via the serialized RDN string, not lose identity.
// / TestExtractTLSInfo_CNLess 钉死 SAN-only 回退：无 CommonName 的
// 证书（现代 LE 链、httptest 本地证书）仍须经序列化 RDN 串给出
// subject，而不是丢身份。
func TestExtractTLSInfo_CNLess(t *testing.T) {
	cert := makeLeafCert(t, "", []string{"only-san.corp.local"})
	cs := &tls.ConnectionState{
		Version:          tls.VersionTLS13,
		PeerCertificates: []*x509.Certificate{cert},
	}
	got := extractTLSInfo(cs)
	if got.Subject == "" || got.Issuer == "" {
		t.Errorf("CN-less cert lost identity: subject=%q issuer=%q", got.Subject, got.Issuer)
	}
	if !strings.Contains(got.Subject, "O=") {
		t.Errorf("Subject fallback = %q, want serialized RDN containing O=", got.Subject)
	}
	if len(got.SANs) != 1 || got.SANs[0] != "only-san.corp.local" {
		t.Errorf("SANs = %v, want [only-san.corp.local]", got.SANs)
	}
	if got.Version != "TLS 1.3" {
		t.Errorf("Version = %q, want TLS 1.3", got.Version)
	}
}

// TestExtractTLSInfo_EmptyStates asserts the zero-value contract:
// nil state, missing certs, and unknown version all degrade to the
// zero tlsInfo (fields stay empty in the JSON payload via omitempty).
// / TestExtractTLSInfo_EmptyStates 断言零值契约：nil 状态、无证书、
// 未知版本都退化为零 tlsInfo（payload JSON 经 omitempty 字段留空）。
func TestExtractTLSInfo_EmptyStates(t *testing.T) {
	for name, cs := range map[string]*tls.ConnectionState{
		"nil state":     nil,
		"no certs":      {Version: tls.VersionTLS13},
		"unknown ver":   {Version: 0x9999, PeerCertificates: nil},
		"unknown + ppt": {Version: 0x9999, PeerCertificates: []*x509.Certificate{makeLeafCert(t, "a", nil)}},
	} {
		got := extractTLSInfo(cs)
		if name == "unknown + ppt" {
			// A cert with an unknown wire version still extracts; only
			// the version name stays empty. / 未知 wire 版本下证书仍
			// 提取，只有版本名留空。
			if got.Subject != "a" {
				t.Errorf("%s: Subject = %q, want a", name, got.Subject)
			}
			if got.Version != "" {
				t.Errorf("%s: Version = %q, want empty", name, got.Version)
			}
			continue
		}
		// tlsInfo contains a slice, so compare field-by-field.
		// / tlsInfo 含切片，逐字段比较。
		if got.Subject != "" || got.Issuer != "" || len(got.SANs) != 0 ||
			got.ValidFrom != "" || got.ValidTo != "" || got.Version != "" {
			t.Errorf("%s: got %+v, want zero tlsInfo", name, got)
		}
	}
}
