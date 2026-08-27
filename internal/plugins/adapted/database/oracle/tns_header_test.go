// tns_header_test.go — regression coverage for the TNS Connect packet
// header. / tns_header_test.go — TNS Connect 包头的回归测试。
//
// buildTNSConnect previously allocated a 22-byte header but wrote
// hdr[21:23] (a 2-byte BE uint16), silently indexing past the slice
// end and clobbering the first byte of the data payload. Real Oracle
// servers would have rejected the resulting packet. This test
// pins the header layout so the off-by-one cannot regress.
package oracle

import "testing"

// TestBuildTNSConnectHeaderLayout — the TNS Connect header is exactly
// 23 bytes; connect_flags_2 occupies indices 21 and 22. The data
// payload starts at index 23 and must NOT be overwritten by the
// header-write pass.
//
// / TestBuildTNSConnectHeaderLayout — TNS Connect 头恰好 23 字节；
// connect_flags_2 占索引 21 和 22。data payload 从索引 23 开始，
// 不能被头部写入覆盖。
func TestBuildTNSConnectHeaderLayout(t *testing.T) {
	// service name of length 4 ("ORCL") → data = 1 byte length +
	// 1 byte type + 4 bytes name = 6 bytes.
	pkt := buildTNSConnect("ORCL")

	// Total packet = 23 hdr + 6 data = 29 bytes.
	const wantLen = 29
	if len(pkt) != wantLen {
		t.Fatalf("buildTNSConnect(\"ORCL\") length = %d, want %d (23 hdr + 6 data)", len(pkt), wantLen)
	}

	// Length field must reflect the full packet size (BE uint16 at 0-1).
	gotLen := int(pkt[0])<<8 | int(pkt[1])
	if gotLen != wantLen {
		t.Errorf("packet length field = %d, want %d", gotLen, wantLen)
	}

	// Packet type at index 4 must be Connect (1).
	if pkt[4] != tnsPacketTypeConnect {
		t.Errorf("pkt[4] (packet type) = %d, want %d (Connect)", pkt[4], tnsPacketTypeConnect)
	}

	// connect_flags_1 at index 20 — must equal 0x04 (the value the
	// encoder writes).
	if pkt[20] != 0x04 {
		t.Errorf("pkt[20] (connect_flags_1) = 0x%02x, want 0x04", pkt[20])
	}

	// connect_flags_2 at indices 21-22 must be a clean 2-byte field
	// (the previous bug was that 22 was OOB; here we assert the
	// field exists and reads as the encoder intended, zero).
	if pkt[21] != 0x00 || pkt[22] != 0x00 {
		t.Errorf("connect_flags_2 = [0x%02x, 0x%02x], want [0x00, 0x00]", pkt[21], pkt[22])
	}

	// Data payload starts at index 23 — must begin with the length
	// prefix (5 for "ORCL"+1) followed by the "service name" tag
	// (0x01) and the service bytes. Crucially, these bytes must
	// be the actual data — not remnants of an out-of-bounds write.
	if pkt[23] != 0x05 {
		t.Errorf("pkt[23] (data length prefix) = %d, want 5 (len(\"ORCL\") + 1)", pkt[23])
	}
	if pkt[24] != 0x01 {
		t.Errorf("pkt[24] (data type tag) = 0x%02x, want 0x01 (\"service name\" tag)", pkt[24])
	}
	if string(pkt[25:29]) != "ORCL" {
		t.Errorf("pkt[25:29] = %q, want %q", pkt[25:29], "ORCL")
	}
}

// TestBuildTNSConnectZeroAllocationReuse — the encoder allocates
// `out` via make with `total` capacity so the final append doesn't
// re-allocate. Sanity-check that two consecutive builds both produce
// the same byte sequence (no global state mutation).
//
// / TestBuildTNSConnectZeroAllocationReuse — 编码器用 make + total
// 容量分配，最后的 append 不应重新分配。连续两次调用产生相同字节
// 序列（无全局状态污染）。
func TestBuildTNSConnectIdempotent(t *testing.T) {
	a := buildTNSConnect("ORCL")
	b := buildTNSConnect("ORCL")
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("byte mismatch at index %d: %d vs %d", i, a[i], b[i])
		}
	}
}
