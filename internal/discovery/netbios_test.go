// netbios_test.go — synthetic NBSTAT response parsing (golden rule:
// synthetic / RFC / public-protocol samples only — real intranet
// banners stay out of the repo).
//
// netbios_test.go — 合成 NBSTAT 响应解析（golden 规则：只允许合成 /
// RFC / 公开协议样本——真实内网 banner 不入库）。
package discovery

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// rawNBNameField builds the RAW 16-byte RDATA name field: name padded
// with spaces to 15 chars + suffix byte (first-level encoding applies
// only to DNS-style labels, not to RDATA). / rawNBNameField 构造原始
// 16 字节 RDATA 名字段：名字空格填充至 15 字符 + 后缀字节（一级编码
// 只用于 DNS 式标签，不用于 RDATA）。
func rawNBNameField(name string, suffix byte) []byte {
	field := make([]byte, 15)
	copy(field, name)
	return append(field, suffix)
}

// buildNBSTATResponse assembles a synthetic NBNS NBSTAT reply:
// header → question (echoed wildcard name) → answer RR (ptr + NBSTAT
// RDATA: entries + trailing MAC). / buildNBSTATResponse 组装合成 NBNS
// NBSTAT 回复：头 → 问题段（回显通配名）→ 应答 RR（指针 + NBSTAT
// RDATA：条目 + 尾部 MAC）。
func buildNBSTATResponse(t *testing.T, flags uint16, ancount uint16, entries []byte, mac []byte) []byte {
	t.Helper()
	var pkt []byte
	pkt = append(pkt, 0x12, 0x34)                     // NAME_TRN_ID
	pkt = binary.BigEndian.AppendUint16(pkt, flags)   // FLAGS
	pkt = binary.BigEndian.AppendUint16(pkt, 1)       // QDCOUNT
	pkt = binary.BigEndian.AppendUint16(pkt, ancount) // ANCOUNT
	pkt = binary.BigEndian.AppendUint16(pkt, 0)       // NSCOUNT
	pkt = binary.BigEndian.AppendUint16(pkt, 0)       // ARCOUNT
	// Question: echoed encoded wildcard name (32 bytes, len 0x20).
	// / 问题段：回显的编码通配名（32 字节，长度 0x20）。
	pkt = append(pkt, 0x20)
	pkt = append(pkt, encodeNetBIOSName(DefaultNBNSName)...)
	pkt = append(pkt, 0x00)                          // null
	pkt = binary.BigEndian.AppendUint16(pkt, 0x0021) // NBSTAT
	pkt = binary.BigEndian.AppendUint16(pkt, 0x0001) // IN
	// Answer RR: compressed name pointer → question.
	// / 应答 RR：压缩名指针指向问题段。
	pkt = append(pkt, 0xC0, 0x0C)
	pkt = binary.BigEndian.AppendUint16(pkt, 0x0021) // type NBSTAT
	pkt = binary.BigEndian.AppendUint16(pkt, 0x0001) // class IN
	pkt = binary.BigEndian.AppendUint32(pkt, 10)     // TTL
	rdata := append([]byte{byte(len(entries) / 22)}, entries...)
	rdata = append(rdata, mac...)
	pkt = binary.BigEndian.AppendUint16(pkt, uint16(len(rdata))) // RDLENGTH
	pkt = append(pkt, rdata...)
	return pkt
}

// nbstatEntry22 builds one 22-byte RDATA entry (name + flags + IP).
// / nbstatEntry22 构造一条 22 字节 RDATA 条目（名字 + 标志 + IP）。
func nbstatEntry22(name string, suffix byte, group bool, ip [4]byte) []byte {
	var e []byte
	e = append(e, rawNBNameField(name, suffix)...)
	flags := uint16(0x0400) // active, registered
	if group {
		flags |= 0x8000
	}
	e = binary.BigEndian.AppendUint16(e, flags)
	e = append(e, ip[0], ip[1], ip[2], ip[3])
	return e
}

func TestParseNBSTATAttrs_FullTable(t *testing.T) {
	entries := append(nbstatEntry22("SERVER1", 0x00, false, [4]byte{192, 168, 1, 10}),
		nbstatEntry22("WORKGROUP", 0x00, true, [4]byte{192, 168, 1, 10})...)
	entries = append(entries, nbstatEntry22("SERVER1", 0x20, false, [4]byte{10, 0, 0, 5})...)
	pkt := buildNBSTATResponse(t, 0x8500, 1, entries, []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})

	attrs := parseNBSTATAttrs(pkt, "192.168.1.10")
	if attrs == nil {
		t.Fatal("expected attrs, got nil")
	}
	want := map[string]string{
		"name":    "SERVER1",
		"domain":  "WORKGROUP",
		"names":   "SERVER1@192.168.1.10,WORKGROUP@192.168.1.10,SERVER1@10.0.0.5",
		"aliases": "10.0.0.5=SERVER1",
		"mac":     "00:11:22:33:44:55",
	}
	if !reflect.DeepEqual(attrs, want) {
		t.Errorf("attrs mismatch\n got=%v\nwant=%v", attrs, want)
	}
}

func TestParseNBSTATAttrs_RejectsNonResponses(t *testing.T) {
	full := buildNBSTATResponse(t, 0x8500, 1,
		nbstatEntry22("SERVER1", 0x00, false, [4]byte{192, 168, 1, 10}),
		[]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})
	entries := nbstatEntry22("SERVER1", 0x00, false, [4]byte{192, 168, 1, 10})
	cases := []struct {
		note string
		pkt  []byte
	}{
		{"query bit clear (not a response)", buildNBSTATResponse(t, 0x0500, 1, entries, nil)},
		{"zero answers", buildNBSTATResponse(t, 0x8500, 0, entries, nil)},
		{"truncated header", full[:10]},
		{"wrong RR type", func() []byte {
			p := buildNBSTATResponse(t, 0x8500, 1, entries, nil)
			// RR type sits right after the 2-byte answer name pointer:
			// header(12) + question(38) + ptr(2). / RR 类型紧跟 2 字节
			// 应答名指针：头(12) + 问题段(38) + 指针(2)。
			rr := 12 + 38 + 2
			binary.BigEndian.PutUint16(p[rr:rr+2], 0x0001) // A, not NBSTAT
			return p
		}()},
		{"rdata shorter than entries claim", func() []byte {
			p := buildNBSTATResponse(t, 0x8500, 1, entries, nil)
			// Claim 3 entries, provide 1. / 宣称 3 条，实给 1 条。
			rdataStart := 12 + 38 + 2 + 2 + 2 + 4 + 2
			p[rdataStart] = 3
			return p
		}()},
	}
	for _, tc := range cases {
		if attrs := parseNBSTATAttrs(tc.pkt, "192.168.1.10"); attrs != nil {
			t.Errorf("%s: expected nil attrs, got %v", tc.note, attrs)
		}
	}
}

func TestParseNBSTATAttrs_NoMACNoAliases(t *testing.T) {
	// Single in-table entry whose IP equals the responder: no MAC
	// trailer, no alias. / 表内单条目且 IP 等于响应者：无 MAC 尾、无别名。
	pkt := buildNBSTATResponse(t, 0x8500, 1,
		nbstatEntry22("SERVER1", 0x00, false, [4]byte{192, 168, 1, 10}), nil)
	attrs := parseNBSTATAttrs(pkt, "192.168.1.10")
	if attrs == nil {
		t.Fatal("expected attrs, got nil")
	}
	if _, ok := attrs["mac"]; ok {
		t.Errorf("unexpected mac: %v", attrs)
	}
	if _, ok := attrs["aliases"]; ok {
		t.Errorf("unexpected aliases: %v", attrs)
	}
	if attrs["name"] != "SERVER1" {
		t.Errorf("name = %q, want SERVER1", attrs["name"])
	}
}

func TestNbstatRawName(t *testing.T) {
	cases := []struct {
		note string
		in   []byte
		want string
	}{
		{"padded name with suffix", rawNBNameField("SERVER1", 0x00), "SERVER1"},
		{"exactly 15 chars keeps all", rawNBNameField("ABCDEFGHIJKLMNO", 0x20), "ABCDEFGHIJKLMNO"},
		{"server suffix byte not leaked", rawNBNameField("SERVER1", 0x20), "SERVER1"},
		{"all padding", rawNBNameField("", 0x00), ""},
		{"non-printable stripped", rawNBNameField("A\x01B", 0x00), "AB"},
	}
	for _, tc := range cases {
		if got := nbstatRawName(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.note, got, tc.want)
		}
	}
}
