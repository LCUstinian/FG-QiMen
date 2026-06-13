// rdp_test.go — integration tests for the RDP plugin's Identify
// orchestrator. Spins up a fake RDP server that completes the
// X.224 + MCS handshake with a controlled serverCore, then asserts
// the plugin extracts hostname / build / NLA flag correctly.
//
// rdp_test.go — RDP 插件 Identify 编排器的集成测试。启一个假 RDP 服务器
// 完成 X.224 + MCS 握手并返可控的 serverCore，然后断言插件正确抽
// hostname / build / NLA 标志。
package rdp_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/plugins/adapted/rdp"
	"github.com/LCUstinian/FG-QiMen/common"
)

// fakeRDPServer handles one RDP client connection: reads the X.224 CR,
// replies with X.224 CC selecting `selectedProto`, reads the MCS
// Connect-Initial, then writes a Connect-Response containing
// serverCore = {name, build, version}.
//
// fakeRDPServer 处理一条 RDP 客户端连接：读 X.224 CR，返 X.224 CC 选
// `selectedProto`，读 MCS Connect-Initial，然后写 Connect-Response 含
// serverCore = {name, build, version}。
func fakeRDPServer(t *testing.T, selectedProto uint32, name string, build uint32, version uint32) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		// Read the X.224 CR (TPKT-framed).
		// / 读 X.224 CR（TPKT 包裹）。
		_, _ = readTPKT(c)
		// Reply with X.224 CC.
		// / 返 X.224 CC。
		cc := buildX224CC(selectedProto)
		_, _ = c.Write(cc)
		// Read MCS Connect-Initial.
		// / 读 MCS Connect-Initial。
		_, _ = readTPKT(c)
		// Write a Connect-Response with embedded serverCore.
		// / 写 Connect-Response，含 serverCore。
		resp := buildMCSConnectResponse(name, build, version)
		_, _ = c.Write(resp)
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// readTPKT is a tiny TPKT reader for the fake server.
// readTPKT 是假服务器的最小 TPKT 读函数。
func readTPKT(c net.Conn) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := readFull(c, hdr); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(hdr[2:4]))
	if length < 4 {
		return nil, fmt.Errorf("short TPKT")
	}
	body := make([]byte, length-4)
	if _, err := readFull(c, body); err != nil {
		return nil, err
	}
	return body, nil
}

func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// buildX224CC builds a TPKT-framed X.224 Connection Confirm that
// returns selectedProto. / buildX224CC 构造一个 TPKT 包裹的 X.224
// Connection Confirm，selectedProto 给定。
func buildX224CC(selectedProto uint32) []byte {
	// X.224 CC body: length(1) + CC(1) + DST(2) + SRC(2) + Class(1) + proto(4)
	// / X.224 CC body：length(1) + CC(1) + DST(2) + SRC(2) + Class(1) + proto(4)
	body := []byte{
		10,    // length of remaining X.224 PDU
		0xD0,  // CC
		0, 1,  // DST-REF
		0, 2,  // SRC-REF
		0x00,  // Class Option
	}
	var proto [4]byte
	binary.LittleEndian.PutUint32(proto[:], selectedProto)
	body = append(body, proto[:]...)
	return tpkFrame(body)
}

// buildMCSConnectResponse builds a TPKT-framed MCS Connect-Response
// containing a serverCore with the given fields.
//
// buildMCSConnectResponse 构造一个 TPKT 包裹的 MCS Connect-Response，
// 含给定字段的 serverCore。
func buildMCSConnectResponse(name string, build uint32, version uint32) []byte {
	// serverCore block (128 bytes, all we need). / serverCore block（128 字节）。
	sc := make([]byte, 128)
	binary.LittleEndian.PutUint32(sc[0:4], version)
	binary.LittleEndian.PutUint32(sc[16:20], build)
	copy(sc[20:52], name)

	// GCC body: serverCore preceded by a BER tag 0x30 0xC0... Actually
	// the serverCore is wrapped in a GCC Conference Create Response
	// with tag 0x0C 0x00 0x00 0x00. The simplest valid wrapping for our
	// parser is the 0x30 0xC0 prefix used by the real wire format.
	// / GCC body：serverCore 前是 BER tag 0x30 0xC0... 实际上
	// serverCore 包在 GCC Conference Create Response（tag 0x0C ...）里。
	// 对我们的 parser 来说，最简有效包装是线格式里的 0x30 0xC0 前缀。
	gccTag := []byte{0x30, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	gccBody := append(gccTag, sc...)

	// MCS Connect-Response: BER tag 0x66 (APPLICATION 6) + length.
	// / MCS Connect-Response：BER tag 0x66 (APPLICATION 6) + length。
	// For simplicity, wrap gccBody in BER APPLICATION 6 with a minimal
	// domain reference first. / 为简单起见，先在 gccBody 前加最小 domain
	// reference。
	mcsInner := []byte{
		0x04, 0x01, 0x00, // result domain
		0x04, 0x01, 0x00, // called domain
	}
	mcsInner = append(mcsInner, gccBody...)

	mcs := []byte{0x66}
	// BER length for mcsInner.
	// / mcsInner 的 BER 长度。
	if len(mcsInner) < 128 {
		mcs = append(mcs, byte(len(mcsInner)))
	} else {
		// Long form not needed for our test sizes.
		// / 我们的测试大小用不到长形式。
	}
	mcs = append(mcs, mcsInner...)
	return tpkFrame(mcs)
}

func tpkFrame(body []byte) []byte {
	out := make([]byte, 0, 4+len(body))
	out = append(out, 0x03, 0x00)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(4+len(body)))
	out = append(out, lenBuf[:]...)
	out = append(out, body...)
	return out
}

// ─── tests ──────────────────────────────────────────────────────────

func TestRDPPlugin_HYBRID_FullFingerprint(t *testing.T) {
	ln := fakeRDPServer(t, 0x02 /* PROTOCOL_HYBRID */, "WIN-SRV-01", 19041, 0x00080004)
	host, port := splitHostPort(t, ln.Addr().String())

	p := rdp.New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil")
	}
	if res.Service != "rdp" {
		t.Errorf("Service = %q, want rdp", res.Service)
	}
	// Extra should hold a populated *common.RDPFingerprint.
	// / Extra 应含一个填好的 *common.RDPFingerprint。
	rdpFP, ok := res.Extra.(*common.RDPFingerprint)
	if !ok {
		t.Fatalf("Extra type = %T, want *common.RDPFingerprint", res.Extra)
	}
	if rdpFP.ServerName != "WIN-SRV-01" {
		t.Errorf("ServerName = %q, want WIN-SRV-01", rdpFP.ServerName)
	}
	if rdpFP.OSBuild != "19041" {
		t.Errorf("OSBuild = %q, want 19041", rdpFP.OSBuild)
	}
	if !rdpFP.NLASupported {
		t.Errorf("NLASupported = false, want true (selectedProto=HYBRID)")
	}
}

func TestRDPPlugin_PlainRDP_NoNLA(t *testing.T) {
	ln := fakeRDPServer(t, 0x00 /* PROTOCOL_RDP */, "PLAIN", 0, 0x00080004)
	host, port := splitHostPort(t, ln.Addr().String())

	p := rdp.New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil")
	}
	rdpFP := res.Extra.(*common.RDPFingerprint)
	if rdpFP.NLASupported {
		t.Errorf("NLASupported = true, want false (selectedProto=RDP)")
	}
}

func TestRDPPlugin_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := rdp.New()
	res := p.Identify(context.Background(), "127.0.0.1", port)
	if res != nil {
		t.Errorf("expected nil on conn refused, got %+v", res)
	}
}

// splitHostPort: helper for tests. / splitHostPort：测试辅助。
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}
