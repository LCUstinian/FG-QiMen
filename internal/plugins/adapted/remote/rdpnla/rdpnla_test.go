// rdpnla_test.go — unit tests for the NTLM NEGOTIATE byte
// structure and X.224 CR framing.
//
// / rdpnla_test.go — NTLM NEGOTIATE 字节结构和 X.224 CR framing
// 的单元测试。
package rdpnla

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildX224CR_Structure(t *testing.T) {
	cr := buildX224CR("test")
	// TPKT header: version=3, reserved=0, length=4+X.224len. / TPKT 头。
	if cr[0] != 0x03 || cr[1] != 0x00 {
		t.Errorf("TPKT version/reserved = %02x %02x, want 03 00", cr[0], cr[1])
	}
	tpktLen := binary.BigEndian.Uint16(cr[2:4])
	if int(tpktLen) != len(cr) {
		t.Errorf("TPKT length %d, actual %d", tpktLen, len(cr))
	}
	// X.224 CR: length byte, code=0x0E. / X.224 CR：长度字节，
	// code=0x0E。
	if cr[4] == 0 {
		t.Error("X.224 length byte is 0")
	}
	if cr[5] != 0x0E {
		t.Errorf("X.224 CR code = 0x%02x, want 0x0E", cr[5])
	}
	// Cookie is "test\x00" (5 bytes) starting at offset 11
	// (after TPKT(4) + length(1) + CR(1) + dst-ref(2) + src-ref(2) +
	// class(1)).
	// / Cookie 是 "test\x00"（5 字节）从 offset 11 开始。
	if !bytes.Equal(cr[11:16], []byte("test\x00")) {
		t.Errorf("cookie = %q, want %q", cr[11:16], "test\x00")
	}
	// requestedProtocols (4 bytes LE) = 0x00000002 (HYBRID). /
	// requestedProtocols（4 字节 LE）= 0x00000002（HYBRID）。
	if !bytes.Equal(cr[16:20], []byte{0x02, 0x00, 0x00, 0x00}) {
		t.Errorf("requestedProtocols = %x, want 02000000", cr[16:20])
	}
}

func TestBuildNTLMNegotiate_Structure(t *testing.T) {
	ntlm := buildNTLMNegotiate()
	// Signature "NTLMSSP\x00" (8 bytes). / 签名。
	if !bytes.Equal(ntlm[:8], []byte("NTLMSSP\x00")) {
		t.Errorf("signature = %q, want NTLMSSP\\0", ntlm[:8])
	}
	// Type = 1 (LE). / Type = 1（LE）。
	if got := binary.LittleEndian.Uint32(ntlm[8:12]); got != 1 {
		t.Errorf("type = %d, want 1", got)
	}
	// Flags = 0x00088207. / Flags.
	if got := binary.LittleEndian.Uint32(ntlm[12:16]); got != 0x00088207 {
		t.Errorf("flags = 0x%08x, want 0x00088207", got)
	}
	// Total length = 32 (header only, no domain/workstation data). /
	// 总长 32（仅头，无 domain/workstation data）。
	if len(ntlm) != 32 {
		t.Errorf("ntlm len = %d, want 32", len(ntlm))
	}
}

func TestBuildNTLMNegotiate_NoCredentials(t *testing.T) {
	// HARD-rule regression guard: the NEGOTIATE message MUST NOT
	// contain any credential material. / HARD 规则回归防护：
	// NEGOTIATE 消息**绝不**包含任何凭据材料。
	ntlm := buildNTLMNegotiate()
	// No LMv2 / NTLMv2 hash (which would appear after the
	// 32-byte header). / 无 LMv2 / NTLMv2 hash（出现在 32
	// 字节头之后）。
	if len(ntlm) > 32 {
		t.Errorf("NEGOTIATE should be header-only; got %d bytes (credential data present?)", len(ntlm))
	}
}
