// wire_test.go — unit tests for the RDP wire protocol helpers
// (TPKT / X.224 / MCS / GCC encoders and decoders).
//
// wire_test.go — RDP 协议辅助函数（TPKT / X.224 / MCS / GCC 编解码）的
// 单元测试。
package rdp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ─── TPKT tests ─────────────────────────────────────────────────────

func TestTPKTFrame_RoundTrip(t *testing.T) {
	// Frame a 5-byte payload. / 给 5 字节 payload 加 TPKT 帧。
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	framed := TPKTFrame(payload)
	if len(framed) != 4+len(payload) {
		t.Fatalf("framed len = %d, want %d", len(framed), 4+len(payload))
	}
	if framed[0] != 0x03 {
		t.Errorf("version = 0x%02x, want 0x03", framed[0])
	}
	if framed[1] != 0x00 {
		t.Errorf("reserved = 0x%02x, want 0x00", framed[1])
	}
	if int(binary.BigEndian.Uint16(framed[2:4])) != len(framed) {
		t.Errorf("length field = %d, want %d", binary.BigEndian.Uint16(framed[2:4]), len(framed))
	}
	if !bytes.Equal(framed[4:], payload) {
		t.Errorf("payload mismatch")
	}
}

func TestTPKTRead_OK(t *testing.T) {
	// Server sends: version=3, reserved=0, length=10, then 6 bytes of payload.
	// / 服务器发：version=3，reserved=0，length=10，然后 6 字节 payload。
	framed := []byte{0x03, 0x00, 0x00, 0x0A, 'H', 'E', 'L', 'L', 'O', '!'}
	r := bytes.NewReader(framed)
	payload, err := TPKTRead(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "HELLO!" {
		t.Errorf("payload = %q, want HELLO!", payload)
	}
}

func TestTPKTRead_BadVersion(t *testing.T) {
	framed := []byte{0x04, 0x00, 0x00, 0x08, 0, 0, 0, 0} // version 0x04 (bad)
	r := bytes.NewReader(framed)
	_, err := TPKTRead(r)
	if err == nil {
		t.Errorf("expected error for bad TPKT version")
	}
}

func TestTPKTRead_TooShort(t *testing.T) {
	framed := []byte{0x03, 0x00, 0x00, 0x10, 0, 0} // length=16 but only 6 bytes
	r := bytes.NewReader(framed)
	_, err := TPKTRead(r)
	if err == nil {
		t.Errorf("expected error for short TPKT")
	}
}

// ─── X.224 CR/CC tests ──────────────────────────────────────────────

func TestX224CR_EncodesCorrectly(t *testing.T) {
	got := EncodeX224CR("mstshash=alice", ProtocolHYBRID)
	// Layout (after TPKT 4-byte frame):
	//   1 byte: length (of remaining X.224 PDU)
	//   1 byte: CR (0xE0)
	//   2 bytes: DST-REF (0x0000)
	//   2 bytes: SRC-REF (0x1234) — any nonzero
	//   1 byte: Class Option (0x00)
	//   1 byte: Cookie length
	//   N bytes: cookie
	//   1 byte: RDP negotiation cookie terminator (0x0D 0x0A)
	//   4 bytes: requestedProtocols (LE)
	// / 布局（TPKT 4 字节帧后）：
	//   1 字节：长度（剩余 X.224 PDU）
	//   1 字节：CR (0xE0)
	//   2 字节：DST-REF (0x0000)
	//   2 字节：SRC-REF (0x1234) — 任意非零
	//   1 字节：Class Option (0x00)
	//   1 字节：Cookie 长度
	//   N 字节：cookie
	//   1 字节：RDP negotiation cookie 结束符 (0x0D 0x0A)
	//   4 字节：requestedProtocols (LE)
	if len(got) < 4 {
		t.Fatalf("got too short: %d bytes", len(got))
	}
	// Skip TPKT 4-byte header. / 跳过 TPKT 4 字节头。
	pdu := got[4:]
	// Length byte. / 长度字节。
	x224Len := int(pdu[0])
	if 1+x224Len > len(pdu) {
		t.Fatalf("X.224 length %d exceeds PDU %d", x224Len, len(pdu))
	}
	// CR byte. / CR 字节。
	if pdu[1] != 0xE0 {
		t.Errorf("CR = 0x%02x, want 0xE0", pdu[1])
	}
	// DST-REF = 0. / DST-REF = 0。
	if binary.LittleEndian.Uint16(pdu[2:4]) != 0 {
		t.Errorf("DST-REF nonzero")
	}
	// Class Option = 0. / Class Option = 0。
	if pdu[6] != 0x00 {
		t.Errorf("Class Option = 0x%02x, want 0x00", pdu[6])
	}
	// Cookie length follows. / Cookie 长度在后面。
	cookieLen := int(pdu[7])
	cookie := string(pdu[8 : 8+cookieLen])
	if cookie != "mstshash=alice" {
		t.Errorf("cookie = %q, want mstshash=alice", cookie)
	}
	// After cookie, 0x0D 0x0A terminator. / cookie 之后 0x0D 0x0A 结束符。
	off := 8 + cookieLen
	if pdu[off] != 0x0D || pdu[off+1] != 0x0A {
		t.Errorf("terminator = %02x %02x, want 0D 0A", pdu[off], pdu[off+1])
	}
	// Then requestedProtocols (4 bytes LE). / 然后 requestedProtocols (4 字节 LE)。
	off += 2
	gotProto := binary.LittleEndian.Uint32(pdu[off : off+4])
	if gotProto != ProtocolHYBRID {
		t.Errorf("requestedProtocols = 0x%08x, want 0x%08x", gotProto, ProtocolHYBRID)
	}
}

func TestX224CR_NoCookie(t *testing.T) {
	got := EncodeX224CR("", ProtocolRDP)
	pdu := got[4:]
	cookieLen := int(pdu[7])
	if cookieLen != 0 {
		t.Errorf("cookieLen = %d, want 0", cookieLen)
	}
	// After cookie, terminator (0x0D 0x0A) even with empty cookie.
	// / cookie 之后 0x0D 0x0A 结束符（即使空 cookie）。
	if pdu[8] != 0x0D || pdu[9] != 0x0A {
		t.Errorf("terminator = %02x %02x, want 0D 0A", pdu[8], pdu[9])
	}
}

func TestX224CC_DecodesHYBRID(t *testing.T) {
	// Build a fake CC: TPKT(4) length=15 + length(1)=10 + CC(1)=0xD0 +
	// DST-REF(2)=1 + SRC-REF(2)=2 + Class Option(1)=0 + selectedProtocol(4).
	// / 构造一个假 CC：TPKT(4) 长度 15 + length(1)=10 + CC(1)=0xD0 +
	// DST-REF(2)=1 + SRC-REF(2)=2 + Class Option(1)=0 + selectedProtocol(4)。
	cc := []byte{0x03, 0x00, 0x00, 0x0F,
		10,               // length of remaining X.224 PDU
		0xD0,             // CC
		0, 1,             // DST-REF
		0, 2,             // SRC-REF
		0x00,             // Class Option
		0x02, 0x00, 0x00, 0x00, // selectedProtocol = PROTOCOL_HYBRID
	}
	conf, err := DecodeX224CC(bytes.NewReader(cc))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conf.SelectedProtocol != ProtocolHYBRID {
		t.Errorf("SelectedProtocol = 0x%08x, want 0x%08x", conf.SelectedProtocol, ProtocolHYBRID)
	}
}

func TestX224CC_DecodesRDP(t *testing.T) {
	cc := []byte{0x03, 0x00, 0x00, 0x0F,
		10, 0xD0, 0, 1, 0, 2, 0x00,
		0x00, 0x00, 0x00, 0x00, // PROTOCOL_RDP
	}
	conf, err := DecodeX224CC(bytes.NewReader(cc))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conf.SelectedProtocol != ProtocolRDP {
		t.Errorf("SelectedProtocol = 0x%08x, want 0x%08x", conf.SelectedProtocol, ProtocolRDP)
	}
}

func TestX224CC_RejectsBadType(t *testing.T) {
	// CR (0xE0) instead of CC (0xD0). / CR (0xE0) 替代 CC (0xD0)。
	cc := []byte{0x03, 0x00, 0x00, 0x0F,
		10, 0xE0, 0, 1, 0, 2, 0x00,
		0, 0, 0, 0,
	}
	_, err := DecodeX224CC(bytes.NewReader(cc))
	if err == nil {
		t.Errorf("expected error for CR-as-CC")
	}
}

// ─── MCS Connect-Initial test ───────────────────────────────────────

func TestMCSConnectInitial_Encodes(t *testing.T) {
	got := EncodeMCSConnectInitial()
	// Just check it's non-empty, TPKT-framed, and the payload contains
	// the GCC Conference Create Request magic 0x14 0x76 ... / 只检查非
	// 空、TPKT 框架、payload 含 GCC Conference Create Request magic。
	if len(got) < 20 {
		t.Fatalf("got too short: %d bytes", len(got))
	}
	if got[0] != 0x03 {
		t.Errorf("TPKT version = 0x%02x, want 0x03", got[0])
	}
	// Find the GCC magic. The payload is 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53
	// followed by 0x40 0x00 (which encodes "MCS Connect-Initial" in BER).
	// / 找 GCC magic。payload 是 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53
	// 然后 0x40 0x00（BER 编码的 "MCS Connect-Initial"）。
	magic := []byte{0x14, 0x76, 0x62, 0x36, 0x88, 0x4E, 0xCE, 0x53}
	if !bytes.Contains(got, magic) {
		t.Errorf("payload missing GCC magic")
	}
}

// ─── serverCore parser test ─────────────────────────────────────────

func TestServerCore_ParseFingerprint(t *testing.T) {
	// Build a fake serverCore block with a known ClientName and ClientBuild.
	// / 构造一个 serverCore block，含已知的 ClientName 和 ClientBuild。
	sc := makeServerCoreForTest("WIN-SRV-01", 19041, 0x00080004)
	parsed, err := parseServerCore(sc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Version != 0x00080004 {
		t.Errorf("Version = 0x%08x, want 0x00080004", parsed.Version)
	}
	if parsed.ClientBuild != 19041 {
		t.Errorf("ClientBuild = %d, want 19041", parsed.ClientBuild)
	}
	// ClientName is 32 bytes, padded with NUL. / ClientName 是 32 字节，
	// 后面填 NUL。
	gotName := string(bytes.TrimRight(parsed.ClientName[:], "\x00"))
	if gotName != "WIN-SRV-01" {
		t.Errorf("ClientName = %q, want WIN-SRV-01", gotName)
	}
}

func TestServerCore_VersionToOSName(t *testing.T) {
	cases := []struct {
		v    uint32
		want string
	}{
		{0x00080004, "Windows 7 / Server 2008 R2 / 10 / 2016"},
		{0x00080005, "Windows 10 1607+ / Server 2019 / 11"},
		{0xDEADBEEF, "0xDEADBEEF"},
	}
	for _, c := range cases {
		sc := &ServerCore{Version: c.v}
		if got := sc.VersionToOSName(); got != c.want {
			t.Errorf("Version 0x%08x → %q, want %q", c.v, got, c.want)
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────

// makeServerCoreForTest builds a byte slice matching the serverCore wire
// layout in MS-RDPBCGR §2.2.1.3.2.
//
// Layout (offsets, all LE): version(0:4) | desktopWidth(4:6) |
// desktopHeight(6:8) | colorDepth(8:10) | SASSequence(10:12) |
// keyboardLayout(12:16) | clientBuild(16:20) | clientName(20:52).
//
// makeServerCoreForTest 构造一个匹配 MS-RDPBCGR §2.2.1.3.2 serverCore
// 线格式的字节切片。
func makeServerCoreForTest(name string, build uint32, version uint32) []byte {
	const size = 4 + 2 + 2 + 2 + 2 + 4 + 4 + 32 + 4 + 4 + 4 + 64
	b := make([]byte, size)
	binary.LittleEndian.PutUint32(b[0:4], version)
	binary.LittleEndian.PutUint32(b[16:20], build)
	copy(b[20:52], name)
	return b
}
