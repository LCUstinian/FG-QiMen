// mongodb_bson.go — BSON encoding + MongoDB wire-protocol helpers
// for the MongoDB authenticator. Extracted from mongodb.go (was
// one 457-line file) so the high-level orchestrator in mongodb.go
// and the SCRAM-SHA-256 flow in mongodb_scram.go can evolve
// independently of the wire-format details.
//
// mongodb_bson.go — MongoDB authenticator 的 BSON 编码 + 线协议
// helper。从 mongodb.go（原 457 行）抽出；让 mongodb.go 的高层
// 编排、mongodb_scram.go 的 SCRAM-SHA-256 流程与本文件的线格式
// 细节可独立演化。
//
// Two concerns live here:
//   - BSON document construction: bsonSaslStart / bsonSaslContinue
//     build the saslStart / saslContinue commands; bsonAppend*
//     helpers append typed BSON elements
//   - OP_MSG wire format: mongoOpMsg / mongoReadReply / readFullMongo
//     handle the 16-byte message header + section kind 0x00 (body)
//
// mongoParseSaslReply walks a BSON doc element-by-element (per
// type byte) to extract conversationId + payload, with no
// substring search (the v0.1 bug).
package database

import (
	"encoding/binary"
	"fmt"
	"net"
)

// bsonSaslStart builds a BSON document for the saslStart command.
// / bsonSaslStart 构造 saslStart 命令的 BSON 文档。
//
//	{ saslStart: 1, mechanism: "SCRAM-SHA-256", payload: <bindata>, $db: "admin" }
func bsonSaslStart(mechanism, payload string) []byte {
	doc := []byte{0x00, 0x00, 0x00, 0x00} // length placeholder
	doc = bsonAppendInt32(doc, "saslStart", 1)
	doc = bsonAppendString(doc, "mechanism", mechanism)
	doc = bsonAppendBinData(doc, "payload", []byte(payload))
	doc = bsonAppendString(doc, "$db", "admin")
	doc = append(doc, 0x00) // null terminator
	binary.LittleEndian.PutUint32(doc[0:4], uint32(len(doc)))
	return doc
}

// bsonSaslContinue builds a BSON document for saslContinue.
// / bsonSaslContinue 构造 saslContinue 的 BSON 文档。
//
//	{ saslContinue: 1, conversationId: <int32>, payload: <bindata> }
func bsonSaslContinue(convID int32, payload string) []byte {
	doc := []byte{0x00, 0x00, 0x00, 0x00}
	doc = bsonAppendInt32(doc, "saslContinue", 1)
	doc = bsonAppendInt32(doc, "conversationId", convID)
	doc = bsonAppendBinData(doc, "payload", []byte(payload))
	doc = append(doc, 0x00)
	binary.LittleEndian.PutUint32(doc[0:4], uint32(len(doc)))
	return doc
}

// bsonAppendInt32 appends `key\x00<value: int32 LE>` (type 0x10).
// / bsonAppendInt32 追加 `key\x00<value: int32 LE>`（类型 0x10）。
func bsonAppendInt32(doc []byte, key string, val int32) []byte {
	doc = append(doc, 0x10)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	doc = append(doc,
		byte(val), byte(val>>8), byte(val>>16), byte(val>>24))
	return doc
}

// bsonAppendString appends `key\x00<value: cstring>` (type 0x02).
// / bsonAppendString 追加 `key\x00<value: cstring>`（类型 0x02）。
func bsonAppendString(doc []byte, key, val string) []byte {
	doc = append(doc, 0x02)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	doc = append(doc, val...)
	doc = append(doc, 0x00)
	return doc
}

// bsonAppendBinData appends `key\x00<int32 len><subtype 0x00><bytes>`
// (type 0x05). / bsonAppendBinData 追加 `key\x00<int32 长度><subtype 0x00><bytes>`
// （类型 0x05）。
func bsonAppendBinData(doc []byte, key string, val []byte) []byte {
	doc = append(doc, 0x05)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	ln := int32(len(val))
	doc = append(doc,
		byte(ln), byte(ln>>8), byte(ln>>16), byte(ln>>24))
	doc = append(doc, 0x00) // subtype
	doc = append(doc, val...)
	return doc
}

// mongoOpMsg sends an OP_MSG (opCode 2013) and reads the reply.
// / mongoOpMsg 发 OP_MSG（opCode 2013）并读响应。
func mongoOpMsg(conn net.Conn, body []byte) ([]byte, error) {
	section := append([]byte{0x00}, body...)
	msg := make([]byte, 0, 16+4+len(section))
	msg = append(msg, 0, 0, 0, 0) // length placeholder
	msg = append(msg, 0, 0, 0, 0) // requestID
	msg = append(msg, 0, 0, 0, 0) // responseTo
	msg = append(msg, 2013&0xff, 0, 0, 0)
	msg = append(msg, 0, 0, 0, 0) // flagBits
	msg = append(msg, section...)
	total := len(msg)
	msg[0] = byte(total)
	msg[1] = byte(total >> 8)
	msg[2] = byte(total >> 16)
	msg[3] = byte(total >> 24)
	msg[4] = 1 // requestID
	if _, err := conn.Write(msg); err != nil {
		return nil, err
	}
	return mongoReadReply(conn)
}

// mongoReadReply reads one OP_MSG reply. / mongoReadReply 读一条 OP_MSG 响应。
func mongoReadReply(conn net.Conn) ([]byte, error) {
	hdr := make([]byte, 16)
	if _, err := readFullMongo(conn, hdr); err != nil {
		return nil, err
	}
	length := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16 | int(hdr[3])<<24
	if length < 16 {
		return nil, fmt.Errorf("mongo: short reply header len=%d", length)
	}
	body := make([]byte, length-16)
	if _, err := readFullMongo(conn, body); err != nil {
		return nil, err
	}
	if len(body) < 5 {
		return nil, fmt.Errorf("mongo: short OP_MSG body")
	}
	kind := body[4]
	if kind != 0x00 {
		return nil, fmt.Errorf("mongo: unsupported section kind 0x%02x", kind)
	}
	return body[5:], nil // BSON document
}

func readFullMongo(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// mongoParseSaslReply extracts the conversationId and payload from a
// successful saslStart or saslContinue reply. / mongoParseSaslReply 从
// 成功的 saslStart/saslContinue 响应中提取 conversationId 和 payload。
//
// Walks the BSON document element-by-element, matching key names
// instead of substring-searching (which was the v0.1 bug). Returns
// ("", nil) if the server returned an error doc with no payload.
// / 逐 element 走 BSON 文档，按 key 名匹配——v0.1 是子串搜索（bug）。
// 服务器返错误 doc（无 payload）时返回 ("", nil)。
func mongoParseSaslReply(bson []byte) (convID int32, payload string, err error) {
	if len(bson) < 5 {
		return 0, "", fmt.Errorf("mongo: short BSON")
	}
	// Skip 4-byte length prefix. / 跳过 4 字节长度前缀。
	docLen := int(binary.LittleEndian.Uint32(bson[0:4]))
	if docLen > len(bson) {
		return 0, "", fmt.Errorf("mongo: BSON length %d > buf %d", docLen, len(bson))
	}
	// Walk elements. Last byte is 0x00 terminator.
	// / 走 elements。最后字节是 0x00 结束符。
	i := 4
	for i < docLen-1 {
		if i+1 >= docLen {
			break
		}
		typeByte := bson[i]
		i++
		// Read cstring key (null-terminated). / 读 cstring key（null 结尾）。
		keyStart := i
		for i < docLen && bson[i] != 0x00 {
			i++
		}
		if i >= docLen {
			return 0, "", fmt.Errorf("mongo: unterminated key at %d", keyStart)
		}
		key := string(bson[keyStart:i])
		i++ // skip null terminator
		// Parse value per type. / 按类型解析值。
		switch typeByte {
		case 0x10: // int32
			if i+4 > docLen {
				return 0, "", fmt.Errorf("mongo: short int32 for key %q", key)
			}
			v := int32(binary.LittleEndian.Uint32(bson[i : i+4]))
			i += 4
			if key == "conversationId" {
				convID = v
			}
		case 0x08: // bool
			if i+1 > docLen {
				return 0, "", fmt.Errorf("mongo: short bool for key %q", key)
			}
			i++ // skip 0 or 1
		case 0x05: // binData
			if i+5 > docLen {
				return 0, "", fmt.Errorf("mongo: short bindata for key %q", key)
			}
			ln := int32(binary.LittleEndian.Uint32(bson[i : i+4]))
			i += 4
			subtype := bson[i]
			i++
			_ = subtype
			if i+int(ln) > docLen {
				return 0, "", fmt.Errorf("mongo: bindata len %d out of bounds for key %q", ln, key)
			}
			if key == "payload" {
				payload = string(bson[i : i+int(ln)])
			}
			i += int(ln)
		default:
			return 0, "", fmt.Errorf("mongo: unsupported element type 0x%02x for key %q", typeByte, key)
		}
	}
	if payload == "" {
		// No payload → server returned an error doc. / 无 payload → 服务器
		// 返了错误 doc。
		return convID, "", nil
	}
	return convID, payload, nil
}
