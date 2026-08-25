// bson_doc_test.go — regression coverage for bsonDoc.
//
// bsonDoc previously lost the BSON type byte on each key/value pair
// because \`body = append(bsonCString(k), 0)\` reassigned body to a
// NEW slice, throwing away the type byte appended on the previous
// line. The fix appends to the existing body. This test pins the
// type-byte + key + value layout for int32 and string values so the
// off-by-reassignment cannot regress.
//
// BSON uses LITTLE-ENDIAN byte order for all multi-byte fields
// (length, int32, int64), per the BSON spec.
package mongodb

import (
	"encoding/binary"
	"testing"
)

// TestBsonDocTypeBytePresent — every key/value pair in a BSON doc
// begins with a 1-byte type tag. The previous bug dropped this
// tag; this test pins it back in place.
//
// / TestBsonDocTypeBytePresent — BSON doc 里每个键值对都以 1 字节
// 类型标签开头。旧 bug 丢失了这个标签；本测试固定住它。
func TestBsonDocTypeBytePresent(t *testing.T) {
	doc := bsonDoc(map[string]any{"hello": int32(1), "$db": "admin"})

	// Skip the 4-byte length prefix; the first element starts at
	// offset 4. / 跳过 4 字节长度前缀；第一个元素从 offset 4 开始。
	pairs := doc[4 : len(doc)-1] // drop trailing 0x00 doc terminator

	type kv struct {
		wantType byte
		wantKey  string
		// wantValueBytes is the byte-length of the value payload
		// (excluding the 1-byte type tag and the key+NUL). For int32
		// this is 4; for strings this is len(value)+1 (current code
		// writes strings as cstrings, not length-prefixed strings —
		// see TestBsonDocStringIsCString). / wantValueBytes 是 value
		// payload 的字节数（不含 type byte 和 key+NUL）。int32 是 4；
		// string 是 len(value)+1（旧代码把 string 当 cstring 写——
		// 见 TestBsonDocStringIsCString）。
		wantValueBytes int
	}
	wants := []kv{
		{0x10, "hello", 4}, // 0x10 = int32
		{0x02, "$db", 6},   // 0x02 = string "admin\0"
	}

	i := 0
	for ei, w := range wants {
		if i >= len(pairs) {
			t.Fatalf("element %d: ran out of bytes", ei)
		}
		if pairs[i] != w.wantType {
			t.Errorf("element %d type byte = 0x%02x, want 0x%02x (for key %q)", ei, pairs[i], w.wantType, w.wantKey)
		}
		i++
		// Read NUL-terminated key.
		keyStart := i
		for i < len(pairs) && pairs[i] != 0x00 {
			i++
		}
		if i >= len(pairs) {
			t.Fatalf("element %d: unterminated key", ei)
		}
		gotKey := string(pairs[keyStart:i])
		if gotKey != w.wantKey {
			t.Errorf("element %d key = %q, want %q", ei, gotKey, w.wantKey)
		}
		i++ // consume the 0x00 terminator
		i += w.wantValueBytes
	}

	if i != len(pairs) {
		t.Errorf("walked past end: i=%d, len(pairs)=%d", i, len(pairs))
	}
	// The byte just before EOF must be 0x00 (doc terminator).
	if doc[len(doc)-1] != 0x00 {
		t.Errorf("doc terminator = 0x%02x, want 0x00", doc[len(doc)-1])
	}
}

// TestBsonDocInt32ValueRoundTrip — for a single int32 entry, verify
// the wire-format bytes are exactly what BSON spec requires. The
// previous bug was that the type byte was missing, which would
// cause MongoDB to reject the doc.
//
// / TestBsonDocInt32ValueRoundTrip — 单个 int32 条目，验证 wire
// format 字节完全符合 BSON 规范。旧 bug 丢失了 type byte，会导致
// MongoDB 拒收这个 doc。
func TestBsonDocInt32ValueRoundTrip(t *testing.T) {
	doc := bsonDoc(map[string]any{"x": int32(42)})

	// Doc layout (LE):
	//   offset 0-3:   doc length (uint32 LE) = 12
	//   offset 4:     type byte 0x10 (int32)
	//   offset 5-6:   key "x\0"
	//   offset 7-10:  value 42 as int32 LE
	//   offset 11:    0x00 doc terminator
	wantLen := 12
	if len(doc) != wantLen {
		t.Fatalf("len(doc) = %d, want %d", len(doc), wantLen)
	}
	if got := binary.LittleEndian.Uint32(doc[0:4]); got != uint32(wantLen) {
		t.Errorf("length field = %d, want %d", got, wantLen)
	}
	if doc[4] != 0x10 {
		t.Errorf("type byte = 0x%02x, want 0x10 (int32)", doc[4])
	}
	if doc[5] != 'x' || doc[6] != 0x00 {
		t.Errorf("key bytes = [0x%02x, 0x%02x], want ['x', 0x00]", doc[5], doc[6])
	}
	if got := binary.LittleEndian.Uint32(doc[7:11]); got != 42 {
		t.Errorf("value = %d, want 42", got)
	}
	if doc[11] != 0x00 {
		t.Errorf("doc terminator = 0x%02x, want 0x00", doc[11])
	}
}

// TestBsonDocStringIsCString — documents the (currently
// non-conforming) string encoding: bsonDoc writes strings as
// NUL-terminated cstrings instead of the BSON-spec length-prefixed
// form (4-byte LE length + bytes + NO trailing NUL). This is a
// separate BSON-spec violation that the type-byte fix did not
// address; it's tracked separately because real MongoDB servers
// reject cstring-encoded strings in BSON documents. The smoke
// test that exists today (mongodb_test.go) only checks the SCRAM
// payload string and would not catch this.
//
// This test pins the current behavior so future fixes don't
// silently regress it.
//
// / TestBsonDocStringIsCString — 记录当前（不符合规范的）string
// 编码方式：bsonDoc 把 string 当 NUL 终止的 cstring 写，而不是 BSON
// 规范要求的长度前缀形式（4 字节 LE 长度 + 字节 + 没有尾部 NUL）。
// 这是跟 type-byte 修复不同的另一个 BSON 规范违反；分开追踪，
// 因为真实 MongoDB 服务器会拒绝 BSON 文档里的 cstring-encoded
// string。当前的 smoke test（mongodb_test.go）只查 SCRAM payload
// 字符串，抓不到这个。
//
// 本测试固定当前行为，让未来修复不会悄悄退步。
func TestBsonDocStringIsCString(t *testing.T) {
	doc := bsonDoc(map[string]any{"k": "v"})

	// Current layout (LE):
	//   offset 0-3:   length = 10 (4 + 1 + 2 + 1 + 1 + 1 = 10)
	//   offset 4:     type 0x02 (string)
	//   offset 5-6:   key "k\0"
	//   offset 7-8:   value "v\0" (cstring, NOT length-prefixed)
	//   offset 9:     0x00 doc terminator
	wantLen := 10
	if len(doc) != wantLen {
		t.Fatalf("len(doc) = %d, want %d", len(doc), wantLen)
	}
	if doc[4] != 0x02 {
		t.Errorf("type byte = 0x%02x, want 0x02 (string)", doc[4])
	}
	if doc[5] != 'k' || doc[6] != 0x00 {
		t.Errorf("key bytes = [0x%02x, 0x%02x], want ['k', 0x00]", doc[5], doc[6])
	}
	// Pin the cstring behaviour ( no 4-byte length prefix ): the
	// string content "v" appears immediately after the key.
	if doc[7] != 'v' {
		t.Errorf("value byte at offset 7 = 0x%02x, want 'v'", doc[7])
	}
	if doc[8] != 0x00 {
		t.Errorf("cstring terminator at offset 8 = 0x%02x, want 0x00", doc[8])
	}
	if doc[9] != 0x00 {
		t.Errorf("doc terminator at offset 9 = 0x%02x, want 0x00", doc[9])
	}
}
