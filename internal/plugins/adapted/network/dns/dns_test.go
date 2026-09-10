// dns_test.go — tests for the DNS service-fingerprint plugin. / DNS
// 服务指纹插件测试。
package dns

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// validDNSResponse builds a 32-byte DNS response that echoes the
// request ID, sets QR=1, RCODE=0, QDCOUNT=1, and the supplied
// ANCOUNT. / validDNSResponse 构造一个 32 字节 DNS 响应，回显请求
// ID，置 QR=1，RCODE=0，QDCOUNT=1 和指定的 ANCOUNT。
func validDNSResponse(req []byte, an uint16) []byte {
	resp := make([]byte, 32)
	copy(resp[:2], req[:2])                   // ID echo
	resp[2] = 0x81                            // QR=1, RD=1
	resp[3] = 0x80                            // RA=1, RCODE=0
	binary.BigEndian.PutUint16(resp[4:6], 1)  // QDCOUNT=1
	binary.BigEndian.PutUint16(resp[6:8], an) // ANCOUNT
	return resp
}

// refusedDNSResponse echoes the request ID with QR=1 and RCODE=5
// (REFUSED). Returning a non-nil response keeps the fakeserver
// goroutine alive (it dies on read deadline after 2s of silence).
// / refusedDNSResponse 回显请求 ID，QR=1，RCODE=5（REFUSED）。返回
// 非 nil 响应以避免 fakeserver goroutine 在 2 秒静默后退出。
func refusedDNSResponse(req []byte) []byte {
	resp := make([]byte, 12)
	copy(resp[:2], req[:2])
	resp[2] = 0x81 // QR=1, RD=1
	resp[3] = 0x85 // RA=1, RCODE=5
	return resp
}

// TestDNS_Hit verifies that a server answering the CHAOS-class
// version.bind TXT probe is identified as DNS. / 验证响应 CHAOS
// 类 version.bind TXT 探测的 server 被识别为 DNS。
func TestDNS_Hit(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		return validDNSResponse(req, 1)
	})
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on DNS server")
	}
	if hit.Service != "dns" {
		t.Errorf("Service = %q, want dns", hit.Service)
	}
}

// TestDNS_FallbackToRootA verifies the CHAOS probe can be refused
// while a regular A query for "." still triggers DNS identification.
// / 验证 CHAOS 探测被拒后常规 "." 的 A 查询仍触发 DNS 识别。
func TestDNS_FallbackToRootA(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// Refuse CHAOS (ID=0xBEEF); answer everything else with
		// ANCOUNT=0 so AXFR is not flagged as a leak. / 拒绝
		// CHAOS（ID=0xBEEF）；以 ANCOUNT=0 回应其它请求以避
		// 免 AXFR 被误报为泄露。
		if req[0] == 0xBE && req[1] == 0xEF {
			return refusedDNSResponse(req)
		}
		return validDNSResponse(req, 0)
	})
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit via root A fallback")
	}
	if hit.Service != "dns" {
		t.Errorf("Service = %q, want dns", hit.Service)
	}
}

// TestDNS_AXFRLeak verifies a misconfigured server that allows AXFR
// zone transfers is flagged with the AXFR warning banner. / 验证
// 允许 AXFR zone transfer 的错误配置 server 被标记为 AXFR 警告。
func TestDNS_AXFRLeak(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// Refuse CHAOS; reply to A and AXFR with ANCOUNT>0 to
		// simulate a leaky authoritative server. / 拒绝 CHAOS；
		// 以 ANCOUNT>0 回应 A 和 AXFR 以模拟泄露的权威 server。
		if req[0] == 0xBE && req[1] == 0xEF {
			return refusedDNSResponse(req)
		}
		return validDNSResponse(req, 5)
	})
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected AXFR leak detection")
	}
	if hit.Service != "dns" {
		t.Errorf("Service = %q, want dns", hit.Service)
	}
	if hit.Banner == "" {
		t.Error("Banner empty; want AXFR leak marker")
	}
}
