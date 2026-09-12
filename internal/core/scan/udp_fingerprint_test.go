// udp_fingerprint_test.go — unit tests for UDPServiceProbe, the
// multi-payload "one socket, write-all, read-once" UDP service probe.
// / udp_fingerprint_test.go — UDPServiceProbe 的单元测试：多 payload
// "一条 socket、全写单读"的 UDP 服务探针。
package scan

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestUDPServiceProbe_MultiPayloadHitOnSecond pins the core contract:
// the probe writes ALL payloads for the port on ONE connected socket
// and takes the first response — here the service only answers the
// second payload. Serial payload-then-read rounds would stack a 2s
// timeout per payload (53 has 3 probes → 6s/host); this must return
// fast with the response as banner.
// / TestUDPServiceProbe_MultiPayloadHitOnSecond 钉死核心契约：探针把
// 该端口的全部 payload 在同一条 connected socket 上写完再取首个响应
// ——本例服务只回应第二个 payload。逐个"发-读"会按 payload 叠 2s
// 超时（53 有 3 个 probe → 6s/主机）；这里必须快速返回响应 banner。
func TestUDPServiceProbe_MultiPayloadHitOnSecond(t *testing.T) {
	p1 := []byte{0xDE, 0xAD}
	p2 := []byte{0x00, 0x06, 0x01, 0x00}
	resp := []byte{0x00, 0x06, 0x01, 0x00, 0x00, 0x00, 0xC0, 0x0C}
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, _ *net.UDPAddr) []byte {
		if bytes.Equal(req, p2) {
			return resp
		}
		return nil
	})

	probe := NewUDPServiceProbe(func(int) [][]byte { return [][]byte{p1, p2} })
	start := time.Now()
	res, err := probe.Probe(context.Background(), host, port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateOpen {
		t.Fatalf("State = %v, want Open", res.State)
	}
	if res.Banner != string(resp) {
		t.Errorf("Banner = % x, want % x", res.Banner, resp)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("probe took %v — multi-payload should not stack per-payload timeouts", elapsed)
	}
}

// TestUDPServiceProbe_SilentPortIsOpenEmpty: a silent (open|filtered)
// port returns StateOpen with an empty banner — the ambiguity is
// documented UDP behavior. / TestUDPServiceProbe_SilentPortIsOpenEmpty：
// 静默（open|filtered）端口返回 StateOpen + 空 banner——该模糊性是
// 文档化的 UDP 行为。
func TestUDPServiceProbe_SilentPortIsOpenEmpty(t *testing.T) {
	// Bind+close a TCP port to get an unused port number; the UDP
	// send goes nowhere (loopback may or may not surface ICMP → we
	// accept Open/Closed, mirroring TestUDPProbe_ConnRefused).
	// / 绑+关一个 TCP 端口拿未用端口号；UDP 包发去无回（环回是否浮
	// 出 ICMP 不定 → 接受 Open/Closed，同 TestUDPProbe_ConnRefused）。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	probe := NewUDPServiceProbe(nil)
	res, err := probe.Probe(context.Background(), "127.0.0.1", port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Method != MethodUDP {
		t.Errorf("Method = %v, want UDP", res.Method)
	}
	if res.State != StateOpen && res.State != StateClosed {
		t.Errorf("State = %v, want Open or Closed", res.State)
	}
}

// TestUDPServiceProbe_RefusedIsClosed: a listener that binds and CLOSES
// before the probe lets the kernel ICMP-refuse the datagram; the probe
// must report StateClosed when the refusal surfaces. Environments that
// suppress ICMP (some sandboxes) degrade to StateOpen — accepted, same
// convention as the UDPProbe tests. / TestUDPServiceProbe_RefusedIsClosed：
// 先绑定后关闭的 listener 让内核对数据报 ICMP 拒绝；拒绝浮出时探针
// 必须报 StateClosed。压制 ICMP 的环境（部分沙箱）降级为 StateOpen
// ——可接受，与 UDPProbe 测试同一约定。
func TestUDPServiceProbe_RefusedIsClosed(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close() // release: kernel now refuses datagrams to this port

	probe := NewUDPServiceProbe(nil)
	res, err := probe.Probe(context.Background(), "127.0.0.1", port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateClosed && res.State != StateOpen {
		t.Errorf("State = %v, want Closed (or Open when ICMP suppressed)", res.State)
	}
}

// TestUDPServiceProbe_BinaryBannerRaw pins the banner-fidelity fix:
// binary responses (DNS/SNMP/NBTStat) must come back byte-exact — the
// old trimASCII space-substitution destroyed every non-printable byte
// and made binary-anchored UDP rules unmatchable.
// / TestUDPServiceProbe_BinaryBannerRaw 钉死 banner 保真修复：二进制
// 响应（DNS/SNMP/NBTStat）必须按字节原样返回——旧的 trimASCII 空格
// 替换毁掉全部不可打印字节，让二进制锚定的 UDP 规则永远无法匹配。
func TestUDPServiceProbe_BinaryBannerRaw(t *testing.T) {
	want := []byte{0x00, 0x01, 0xFE, 0xFF, 0x07, 'B', 'I', 'N', 0x00}
	host, port := fakeserver.ListenUDPLoop(t, func([]byte, *net.UDPAddr) []byte {
		return want
	})

	probe := NewUDPServiceProbe(nil)
	res, err := probe.Probe(context.Background(), host, port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateOpen {
		t.Fatalf("State = %v, want Open", res.State)
	}
	if res.Banner != string(want) {
		t.Errorf("Banner = % x, want raw % x (trimASCII must NOT run on UDP banners)", res.Banner, want)
	}
}
