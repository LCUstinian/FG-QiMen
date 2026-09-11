// snmpv3_test.go — fake-server test for the SNMPv3 Identify plugin.
//
// v0.6.1: rebuilt using gosnmp's own MarshalMsg / UnmarshalMessage
// helpers. The fake server always replies with a NoAuthNoPriv
// Report whose single varbind is the standard
// `usmStatsUnknownUserNames` OID (.1.3.6.1.6.3.15.1.1.3.0).
// gosnmp accepts any Report in response to either the discovery
// probe or the auth probe (its `testAuthentication` skips the
// HMAC check when msgFlags == NoAuthNoPriv), and surfaces
// `ErrUnknownUsername` when the varbind OID matches. The plugin's
// containsAny check matches "unknownUserName" → reports a hit.
//
// / v0.6.1：用 gosnmp 自己的 MarshalMsg / UnmarshalMessage helper
// 重建。假 server 永远回一个 NoAuthNoPriv Report，单 varbind
// 是标准 `usmStatsUnknownUserNames` OID（.1.3.6.1.6.3.15.1.1.3.0）。
// gosnmp 对 discovery 探测或 auth 探测的响应都接受 Report
//（testAuthentication 在 msgFlags == NoAuthNoPriv 时跳过 HMAC
// 检查），varbind OID 匹配时 surface `ErrUnknownUsername`。
// plugin 的 containsAny 匹配 "unknownUserName" → 报命中。
package snmpv3

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// testEngineID is the engine ID we advertise. Anything > 5 bytes is
// fine per RFC 3411 §5 (engine ID format). / testEngineID 是
// 我们宣告的 engine ID。任何 > 5 字节都行（RFC 3411 §5）。
var testEngineID = "\x80\x00\x00\x00\x05\x00\x00\x00\x00\x00\x00\x01"

// usmStatsUnknownUserNames is the standard usmStats OID for
// "unknown username" — a standard Report varbind per RFC 3414.
// / usmStatsUnknownUserNames 是 RFC 3414 "unknown username" 的
// 标准 usmStats OID，标准 Report varbind。
var usmStatsUnknownUserNames = gosnmp.SnmpPDU{Name: "1.3.6.1.6.3.15.1.1.3.0"}

// buildUnknownUserReport builds a NoAuthNoPriv Report PDU
// containing the `usmStatsUnknownUserNames` varbind. The MsgID is
// arbitrary (gosnmp matches Report to original request via the
// same engine ID rather than MsgID in this loop). / buildUnknownUserReport
// 构造一个 NoAuthNoPriv Report PDU，含 `usmStatsUnknownUserNames`
// varbind。MsgID 任意（gosnmp 此循环里通过 engine ID 而非 MsgID
// 匹配 Report 到原始请求）。
func buildUnknownUserReport(engineID string) ([]byte, error) {
	pkt := &gosnmp.SnmpPacket{
		Version:       gosnmp.Version3,
		MsgFlags:      gosnmp.NoAuthNoPriv, // no HMAC; Report's varbind is what counts
		SecurityModel: gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{
			AuthoritativeEngineID:    engineID,
			AuthoritativeEngineBoots: 1,
			AuthoritativeEngineTime:  1,
		},
		PDUType: gosnmp.Report,
		MsgID:   1,
		Variables: []gosnmp.SnmpPDU{
			{
				Name:  usmStatsUnknownUserNames.Name,
				Type:  gosnmp.Null, // null value; varbind is the OID itself
			},
		},
	}
	return pkt.MarshalMsg()
}

// TestSnmpv3_IdentifyHit drives the full discovery + auth round
// trip. The fake server replies to every packet with a
// NoAuthNoPriv Report carrying usmStatsUnknownUserNames. gosnmp
// surfaces ErrUnknownUsername, the plugin's containsAny matches
// "unknownUserName" → reports a hit.
//
// / TestSnmpv3_IdentifyHit 驱动完整 discovery + auth 往返。
// 假 server 对每个包回一个 NoAuthNoPriv Report 带
// usmStatsUnknownUserNames。gosnmp surface ErrUnknownUsername，
// plugin 的 containsAny 匹配 "unknownUserName" → 报命中。
func TestSnmpv3_IdentifyHit(t *testing.T) {
	var pktCount int
	host, port := fakeserver.ListenUDPLoop(t, func(_ []byte, _ *net.UDPAddr) []byte {
		pktCount++
		// Always reply with the same Report regardless of
		// whether the client is sending the discovery probe or the
		// auth probe. Both branches of gosnmp's v3 state machine
		// accept the Report; the varbind OID drives the error
		// message that reaches the plugin. / 不管 client 发的是
		// discovery 还是 auth 探测都回同样的 Report。gosnmp v3
		// 状态机两个分支都接受 Report；varbind OID 决定到达
		// plugin 的错误消息。
		resp, err := buildUnknownUserReport(testEngineID)
		if err != nil {
			t.Logf("buildUnknownUserReport err: %v", err)
			return nil
		}
		return resp
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Logf("pktCount=%d (plugin returned nil)", pktCount)
		t.Fatal("expected hit on SNMPv3 server")
	}
	if hit.Service != "snmpv3" {
		t.Errorf("Service = %q, want snmpv3", hit.Service)
	}
	// Note: gosnmp's v3 loop terminates as soon as the server
	// returns a Report carrying a known-error OID
	// (usmStatsUnknownUserNames). One packet (the discovery
	// probe) is enough — the auth probe never gets sent. This
	// is the intended fast path: the plugin doesn't actually
	// need to authenticate to learn that v3 is enabled.
	// / 注意：gosnmp 的 v3 循环一旦收到带已知 error OID 的
	// Report（usmStatsUnknownUserNames）就终止。一个包
	// （discovery 探测）就够了——auth 探测根本不发。这是预期
	// 的快路径：plugin 不需要真认证就能知道 v3 启用。
	if pktCount < 1 {
		t.Errorf("expected >=1 packet, got %d", pktCount)
	}
}

// TestSnmpv3_CredentialHit is skipped: snmpv3.Credential is a
// documented no-op stub returning nil (snmpv3.go:56-58) —
// real credential testing lives in
// core/cred/auth/network's SnmpV3Authenticator.
// / TestSnmpv3_CredentialHit 跳过：snmpv3.Credential 是文档
// 化的空 stub 返回 nil（snmpv3.go:56-58）——真正的凭据测试
// 在 core/cred/auth/network 的 SnmpV3Authenticator。
func TestSnmpv3_CredentialHit(t *testing.T) {
	t.Skip("snmpv3.Credential is a documented no-op stub; see snmpv3.go Credential().")
}
