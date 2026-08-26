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

	// Build expected entries indexed by key. Iteration order over a
	// Go map is randomized, so this test must be order-independent
	// (the previous version iterated expecting a specific order and
	// flaked on macOS CI). / 按 key 索引期望条目。Go map 迭代是
	// 随机的，测试必须顺序无关（上一版假设固定顺序，在 macOS CI
	// 上 flake）。
	type kv struct {
		wantType byte
	}
	wantsByKey := map[string]kv{
		"hello": {0x10}, // 0x10 = int32
		"$db":   {0x02}, // 0x02 = string
	}
	seen := map[string]bool{}

	i := 0
	for i < len(pairs) {
		if pairs[i] != 0x10 && pairs[i] != 0x02 {
			t.Errorf("unknown type byte 0x%02x at offset %d", pairs[i], i)
			return
		}
		elemType := pairs[i]
		i++
		// Read NUL-terminated key.
		keyStart := i
		for i < len(pairs) && pairs[i] != 0x00 {
			i++
		}
		if i >= len(pairs) {
			t.Fatalf("unterminated key at offset %d", keyStart)
		}
		key := string(pairs[keyStart:i])
		i++ // consume the 0x00 terminator

		w, ok := wantsByKey[key]
		if !ok {
			t.Errorf("unexpected key %q in BSON doc", key)
			return
		}
		if seen[key] {
			t.Errorf("duplicate key %q in BSON doc", key)
		}
		seen[key] = true

		if elemType != w.wantType {
			t.Errorf("key %q type byte = 0x%02x, want 0x%02x", key, elemType, w.wantType)
		}
		// Skip value payload per type:
		//   - int32 (0x10): 4 bytes LE
		//   - string (0x02): 4-byte LE length + N bytes
		switch elemType {
		case 0x10:
			i += 4
		case 0x02:
			strLen := int(binary.LittleEndian.Uint32(pairs[i : i+4]))
			i += 4 + strLen
		}
	}

	for k := range wantsByKey {
		if !seen[k] {
			t.Errorf("key %q not found in BSON doc", k)
		}
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

// TestBsonDocStringLengthPrefixed — for a single string entry,
// verify the wire format includes the BSON-spec 4-byte LE length
// prefix followed by the raw UTF-8 bytes (no trailing NUL). The
// previous bug wrote a NUL-terminated cstring, which real MongoDB
// servers reject. This test pins the corrected encoding.
//
// / TestBsonDocStringLengthPrefixed — 单个 string 条目，验证 wire
// format 含 BSON 规范的 4 字节 LE 长度前缀 + 原 UTF-8 字节（无尾部
// NUL）。旧 bug 写 NUL 终止的 cstring，真实 MongoDB 服务器会拒收。
// 本测试固定修正后的编码。
func TestBsonDocStringLengthPrefixed(t *testing.T) {
	doc := bsonDoc(map[string]any{"k": "v"})

	// Layout (LE):
	//   offset 0-3:   doc length (uint32 LE) = 13 (4+1+2+4+1+1)
	//   offset 4:     type 0x02 (string)
	//   offset 5-6:   key "k\0"
	//   offset 7-10:  string length 1 (uint32 LE)
	//   offset 11:    'v'  (no trailing NUL)
	//   offset 12:    0x00 doc terminator
	wantLen := 13
	if len(doc) != wantLen {
		t.Fatalf("len(doc) = %d, want %d", len(doc), wantLen)
	}
	if doc[4] != 0x02 {
		t.Errorf("type byte = 0x%02x, want 0x02 (string)", doc[4])
	}
	if doc[5] != 'k' || doc[6] != 0x00 {
		t.Errorf("key bytes = [0x%02x, 0x%02x], want ['k', 0x00]", doc[5], doc[6])
	}
	if got := binary.LittleEndian.Uint32(doc[7:11]); got != 1 {
		t.Errorf("string length = %d, want 1", got)
	}
	if doc[11] != 'v' {
		t.Errorf("string content = 0x%02x, want 'v'", doc[11])
	}
	// Crucially, no trailing NUL after 'v' — the byte at offset 12
	// must be the doc terminator (0x00), not a string NUL.
	if doc[12] != 0x00 {
		t.Errorf("doc terminator = 0x%02x, want 0x00", doc[12])
	}
}

// TestBsonDocStringEmptyLen — a zero-length string value must be
// encoded as 4 zero bytes (length=0) followed immediately by the
// doc terminator, NOT as a single 0x00 (which an old buggy
// implementation might write as "length prefix + empty cstring").
//
// / TestBsonDocStringEmptyLen — 空字符串值必须编码为 4 字节 0（长度
// = 0），后跟 doc 终止符；不能写成单个 0x00（旧 buggy 实现可能写成
// "长度前缀 + 空 cstring"）。
func TestBsonDocStringEmptyLen(t *testing.T) {
	doc := bsonDoc(map[string]any{"k": ""})

	// Layout (LE):
	//   offset 0-3:   doc length = 12 (4+1+2+4+0+1)
	//   offset 4:     type 0x02
	//   offset 5-6:   key "k\0"
	//   offset 7-10:  string length 0 (uint32 LE)
	//   offset 11:    doc terminator (NO string bytes — length was 0)
	wantLen := 12
	if len(doc) != wantLen {
		t.Fatalf("len(doc) = %d, want %d", len(doc), wantLen)
	}
	if got := binary.LittleEndian.Uint32(doc[7:11]); got != 0 {
		t.Errorf("string length = %d, want 0", got)
	}
	if doc[11] != 0x00 {
		t.Errorf("doc terminator = 0x%02x, want 0x00", doc[11])
	}
}
