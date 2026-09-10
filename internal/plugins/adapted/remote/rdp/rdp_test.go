// rdp_test.go — fake-server tests for the rdp Identify plugin.
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// drives the 4-step RDP handshake the plugin expects:
//  1. read X.224 Connection Request (TPKT-framed)
//  2. write X.224 Connection Confirm selecting `selectedProto`
//  3. read MCS Connect-Initial (TPKT-framed)
//  4. write MCS Connect-Response containing the serverCore
//
// The plugin's Identify walks the same state machine, parses
// the serverCore, and returns a types.Result whose Extra holds
// a *output.RDPFingerprint.
//
// / rdp_test.go — rdp Identify 插件的 fake-server 测试。模式：
// fakeserver.ListenLoop 绑 127.0.0.1:0，handler 驱动插件期望的
// 4 步 RDP 握手：1) 读 X.224 CR；2) 写 X.224 CC 选 selectedProto；
// 3) 读 MCS Connect-Initial；4) 写 MCS Connect-Response 含
// serverCore。plugin Identify 走同一状态机，解析 serverCore，
// 返 Extra = *output.RDPFingerprint 的 types.Result。
//
// This is a STATEFUL TCP plugin (Tier 3 in the v0.6.0 fake-server
// plan). The handler is a closure-free linear 4-step sequence —
// each Step is a single read or write on the same conn — which
// makes it a clean fit for the fakeserver.ListenLoop dispatch
// model. Credential is a documented no-op stub (see rdp.go:62-65
// and the TestRdp_CredentialHit skip below); the real RDP NLA
// (CredSSP) credential flow is v0.3+.
//
// / 这是 v0.6.0 fake-server 计划的 Tier 3 有状态 TCP 插件。handler
// 是无闭包的 4 步线性序列——每步在同一 conn 上的单次读/写——非常适
// 合 fakeserver.ListenLoop 分发模型。Credential 是文档化的 no-op
// stub（见 rdp.go:62-65 及下面的 TestRdp_CredentialHit skip）；
// 真正的 RDP NLA（CredSSP）凭据流程是 v0.3+。
package rdp_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/rdp"
)

// fakeRDPServer handles one RDP client connection via
// fakeserver.ListenLoop: reads the X.224 CR, replies with X.224
// CC selecting `selectedProto`, reads the MCS Connect-Initial,
// then writes a Connect-Response containing serverCore = {name,
// build, version}. Returns the bound host:port so the test can
// pass them to Identify.
//
// / fakeRDPServer 通过 fakeserver.ListenLoop 处理一条 RDP 客户端
// 连接：读 X.224 CR，回 X.224 CC 选 selectedProto，读 MCS
// Connect-Initial，然后写 Connect-Response 含 serverCore。返回绑
// 定的 host:port 以供测试传给 Identify。
func fakeRDPServer(t *testing.T, selectedProto uint32, name string, build uint32, version uint32) (host string, port int) {
	t.Helper()
	return fakeserver.ListenLoop(t, func(c net.Conn) {
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		// Step 1+2: X.224 CR → X.224 CC. / X.224 CR → X.224 CC。
		_, _ = readTPKT(c)
		cc := buildX224CC(selectedProto)
		_, _ = c.Write(cc)
		// Step 3+4: MCS Connect-Initial → Connect-Response. /
		// MCS Connect-Initial → Connect-Response。
		_, _ = readTPKT(c)
		resp := buildMCSConnectResponse(name, build, version)
		_, _ = c.Write(resp)
	})
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
		10,   // length of remaining X.224 PDU
		0xD0, // CC
		0, 1, // DST-REF
		0, 2, // SRC-REF
		0x00, // Class Option
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

// TestRdp_IdentifyHit_HYBRID covers the happy path with NLA:
// server picks PROTOCOL_HYBRID, the plugin reports NLA supported
// and extracts serverCore fields.
//
// / TestRdp_IdentifyHit_HYBRID 验证 NLA happy path：服务器选
// PROTOCOL_HYBRID，插件报 NLA 支持并抽出 serverCore 字段。
func TestRdp_IdentifyHit_HYBRID(t *testing.T) {
	host, port := fakeRDPServer(t, rdp.ProtocolHYBRID, "WIN-SRV-01", 19041, 0x00080004)

	p := rdp.New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil")
	}
	if res.Service != "rdp" {
		t.Errorf("Service = %q, want rdp", res.Service)
	}
	// Extra should hold a populated *output.RDPFingerprint.
	// / Extra 应含一个填好的 *output.RDPFingerprint。
	rdpFP, ok := res.Extra.(*output.RDPFingerprint)
	if !ok {
		t.Fatalf("Extra type = %T, want *output.RDPFingerprint", res.Extra)
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

// TestRdp_IdentifyHit_Plain covers the happy path with classic
// RDP security (no NLA). / TestRdp_IdentifyHit_Plain 验证经典
// RDP 安全（无 NLA）的 happy path。
func TestRdp_IdentifyHit_Plain(t *testing.T) {
	host, port := fakeRDPServer(t, rdp.ProtocolRDP, "PLAIN", 0, 0x00080004)

	p := rdp.New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil")
	}
	if res.Service != "rdp" {
		t.Errorf("Service = %q, want rdp", res.Service)
	}
	rdpFP := res.Extra.(*output.RDPFingerprint)
	if rdpFP.NLASupported {
		t.Errorf("NLASupported = true, want false (selectedProto=RDP)")
	}
}

// TestRdp_IdentifyMiss_ConnRefused covers the dialRDP failure
// branch: no server is listening on the target port, so dialRDP
// returns an error and Identify returns nil. / 验证 dialRDP 失
// 败分支：目标端口无服务器，dialRDP 返错，Identify 返 nil。
func TestRdp_IdentifyMiss_ConnRefused(t *testing.T) {
	// Grab a free port, then immediately close the listener so
	// nothing is listening — Identify will hit the dial error.
	// / 拿一个空闲端口然后立刻关 listener，让 Identify 撞 dial
	// 错误。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	p := rdp.New()
	res := p.Identify(context.Background(), "127.0.0.1", port)
	if res != nil {
		t.Errorf("expected nil on conn refused, got %+v", res)
	}
}

// TestRdp_CredentialHit is skipped: rdp.Credential is a
// documented no-op stub (see rdp.go:62-65 — "Credential is a
// no-op stub"). RDP NLA (CredSSP) credential testing is
// explicitly v0.3+ per the package doc; the plugin only
// fingerprints and never runs Attach / Login / Session Setup.
// / TestRdp_CredentialHit 跳过：rdp.Credential 是文档化的 no-op
// stub（见 rdp.go:62-65 "Credential 空 stub"）。RDP NLA
// （CredSSP）凭据测试按包文档明确为 v0.3+；本插件只做指纹，绝不
// 跑 Attach / Login / Session Setup。
func TestRdp_CredentialHit(t *testing.T) {
	t.Skip("rdp.Credential is a no-op stub; see rdp.go Credential() — RDP NLA is v0.3+")
}
