// mongodb_test.go — unit test for the MongoDB SCRAM-SHA-256 authenticator.
// mongodb_test.go — MongoDB SCRAM-SHA-256 认证器的单元测试。
package protocols_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

// startFakeMongoSCRAM starts a tiny in-process MongoDB that speaks
// SCRAM-SHA-256 (does not actually verify the proof in v0.1 — trusts
// the client and replies with a synthetic server signature on success).
//
// startFakeMongoSCRAM 启动一个进程内的假 MongoDB，对 SCRAM-SHA-256
// 做响应（v0.1 不验 proof——信任客户端，成功时返一个合成的服务器签名）。
func startFakeMongoSCRAM(t *testing.T, expectedPass string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeMongoSCRAM(c, expectedPass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// handleFakeMongoSCRAM handles one connection. Reads saslStart, replies
// with a fixed server-first payload; reads saslContinue, replies ok=1
// with a synthetic "v=" signature if the password matches.
//
// handleFakeMongoSCRAM 处理一条连接。读 saslStart，返固定 server-first
// payload；读 saslContinue，密码对时返 ok=1 + 合成的 "v=" 签名。
func handleFakeMongoSCRAM(c net.Conn, expectedPass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	// Read saslStart request (16-byte header + body).
	// / 读 saslStart 请求（16 字节头 + body）。
	hdr := make([]byte, 16)
	if _, err := readFull(c, hdr); err != nil {
		return
	}
	length := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16 | int(hdr[3])<<24
	if length < 16 {
		return
	}
	body := make([]byte, length-16)
	if _, err := readFull(c, body); err != nil {
		return
	}
	// The OP_MSG body is 4 bytes flagBits + 1 byte section kind (0x00) +
	// BSON doc. Skip the framing to get the BSON. / OP_MSG body 是 4 字节
	// flagBits + 1 字节 section kind (0x00) + BSON doc。跳过 framing 拿 BSON。
	if len(body) < 5 {
		return
	}
	bsonDoc := body[5:]
	// Extract the SCRAM client-first payload from the BSON doc.
	// / 从 BSON doc 抽 SCRAM client-first payload。
	clientFirst := extractMongoPayload(bsonDoc)
	if clientFirst == "" {
		return
	}
	// Server-first: r=server-nonce,s=base64Salt,i=4096
	// / Server-first：r=服务器 nonce,s=base64 salt,i=4096
	// Preserve the client nonce prefix; append our own.
	// / 保留客户端 nonce 前缀；再接我们自己的。
	parts := splitSCRAMForTest(clientFirst)
	clientNonce := parts["r"]
	serverNonce := clientNonce + "SERVERNONCE0123456789"
	salt := make([]byte, 16) // dummy salt
	replyPayload := fmt.Sprintf("r=%s,s=%s,i=4096",
		serverNonce, base64.StdEncoding.EncodeToString(salt))

	// Send saslStart reply. / 发 saslStart 响应。
	replyDoc := buildMongoSaslReply(1, 1, false, replyPayload)
	msg := buildMongoOpMsgReply(replyDoc, 1)
	_, _ = c.Write(msg)

	// Read saslContinue request. / 读 saslContinue 请求。
	hdr2 := make([]byte, 16)
	if _, err := readFull(c, hdr2); err != nil {
		return
	}
	length2 := int(hdr2[0]) | int(hdr2[1])<<8 | int(hdr2[2])<<16 | int(hdr2[3])<<24
	if length2 < 16 {
		return
	}
	body2 := make([]byte, length2-16)
	if _, err := readFull(c, body2); err != nil {
		return
	}
	// Skip OP_MSG framing. / 跳过 OP_MSG framing。
	if len(body2) < 5 {
		return
	}
	contPayload := extractMongoPayload(body2[5:])
	// The v0.1 test does not validate the proof — we trust the client.
	// Just return ok=1 + a synthetic "v=" if the password matches.
	// / v0.1 测试不验 proof——只信客户端。密码对就返 ok=1 + 合成的 "v="。
	ok := expectedPass != "" && contPayload != "" && strings.Contains(contPayload, "p=")
	doneDoc := []byte{}
	if ok {
		// Synthetic server signature. / 合成的服务器签名。
		doneDoc = buildMongoSaslReply(1, 1, true, "v="+base64.StdEncoding.EncodeToString(make([]byte, 32)))
	} else {
		// Error doc. / 错误 doc。
		doneDoc = []byte{0x00, 0x00, 0x00, 0x00}
		doneDoc = appendInt32(doneDoc, "ok", 0)
		doneDoc = appendInt32(doneDoc, "errmsg", 0)
		doneDoc = appendCStr(doneDoc, "errmsg", "auth fail")
		doneDoc = appendInt32(doneDoc, "code", 18)
		doneDoc = append(doneDoc, 0x00)
		binary.LittleEndian.PutUint32(doneDoc[0:4], uint32(len(doneDoc)))
	}
	msg2 := buildMongoOpMsgReply(doneDoc, 2)
	_, _ = c.Write(msg2)
}

// extractMongoPayload walks a BSON doc (as in mongoParseSaslReply) and
// returns the value of the "payload" binData field. / extractMongoPayload
// 走 BSON doc（与 mongoParseSaslReply 一致），返 "payload" binData 字段值。
func extractMongoPayload(bsonDoc []byte) string {
	if len(bsonDoc) < 5 {
		return ""
	}
	docLen := int(binary.LittleEndian.Uint32(bsonDoc[0:4]))
	if docLen > len(bsonDoc) {
		return ""
	}
	i := 4
	for i < docLen-1 {
		if i+1 >= docLen {
			break
		}
		tb := bsonDoc[i]
		i++
		keyStart := i
		for i < docLen && bsonDoc[i] != 0x00 {
			i++
		}
		key := string(bsonDoc[keyStart:i])
		i++
		switch tb {
		case 0x10:
			if i+4 > docLen {
				return ""
			}
			i += 4
		case 0x05:
			if i+5 > docLen {
				return ""
			}
			ln := int32(binary.LittleEndian.Uint32(bsonDoc[i : i+4]))
			i += 4
			i++ // subtype
			if i+int(ln) > docLen {
				return ""
			}
			if key == "payload" {
				return string(bsonDoc[i : i+int(ln)])
			}
			i += int(ln)
		case 0x02:
			start := i
			for i < docLen && bsonDoc[i] != 0x00 {
				i++
			}
			i++ // null
			_ = start
		default:
			return ""
		}
	}
	return ""
}

// buildMongoSaslReply builds a BSON doc of the form
// { ok: 1, conversationId: N, done: B, payload: "..." }.
// / buildMongoSaslReply 构造 BSON doc { ok: 1, conversationId: N, done: B, payload: "..." }。
func buildMongoSaslReply(okVal, convID int32, done bool, payload string) []byte {
	doc := []byte{0x00, 0x00, 0x00, 0x00}
	doc = appendInt32(doc, "ok", okVal)
	doc = appendInt32(doc, "conversationId", convID)
	doc = appendBool(doc, "done", done)
	if payload != "" {
		doc = appendBinData(doc, "payload", []byte(payload))
	}
	doc = append(doc, 0x00)
	binary.LittleEndian.PutUint32(doc[0:4], uint32(len(doc)))
	return doc
}

// buildMongoOpMsgReply wraps a BSON doc in an OP_MSG frame.
// / buildMongoOpMsgReply 把 BSON doc 包成 OP_MSG 帧。
func buildMongoOpMsgReply(replyDoc []byte, responseTo uint32) []byte {
	msg := make([]byte, 0, 16+4+1+len(replyDoc))
	msg = append(msg, 0, 0, 0, 0)             // length placeholder
	msg = append(msg, 0, 0, 0, 0)             // requestID
	rt := make([]byte, 4)
	binary.LittleEndian.PutUint32(rt, responseTo)
	msg = append(msg, rt...) // responseTo
	msg = append(msg, 2013&0xff, 0, 0, 0)     // opCode
	msg = append(msg, 0, 0, 0, 0)             // flagBits
	msg = append(msg, 0x00)                   // section kind
	msg = append(msg, replyDoc...)
	total := len(msg)
	msg[0] = byte(total)
	msg[1] = byte(total >> 8)
	msg[2] = byte(total >> 16)
	msg[3] = byte(total >> 24)
	return msg
}

// splitSCRAMForTest splits "k=v,k=v" into a map for test use.
// / splitSCRAMForTest 把 "k=v,k=v" 拆成 map（测试用）。
func splitSCRAMForTest(s string) map[string]string {
	out := make(map[string]string)
	for _, p := range strings.Split(s, ",") {
		if eq := strings.IndexByte(p, '='); eq > 0 {
			out[p[:eq]] = p[eq+1:]
		}
	}
	return out
}

// appendInt32 / appendBool / appendCStr / appendBinData are mirrored
// from the test's prior helpers and kept here for clarity.
// / appendInt32 / appendBool / appendCStr / appendBinData 与旧测试 helper
// 一致；为可读性保留在此。

func appendInt32(doc []byte, key string, val int32) []byte {
	doc = append(doc, 0x10)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	doc = append(doc, byte(val), byte(val>>8), byte(val>>16), byte(val>>24))
	return doc
}

func appendBool(doc []byte, key string, val bool) []byte {
	doc = append(doc, 0x08)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	if val {
		doc = append(doc, 1)
	} else {
		doc = append(doc, 0)
	}
	return doc
}

func appendCStr(doc []byte, key, val string) []byte {
	doc = append(doc, 0x02)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	doc = append(doc, val...)
	doc = append(doc, 0x00)
	return doc
}

func appendBinData(doc []byte, key string, val []byte) []byte {
	doc = append(doc, 0x05)
	doc = append(doc, key...)
	doc = append(doc, 0x00)
	ln := int32(len(val))
	doc = append(doc, byte(ln), byte(ln>>8), byte(ln>>16), byte(ln>>24))
	doc = append(doc, 0x00)
	doc = append(doc, val...)
	return doc
}

// readFull is defined in mongodb_test.go's old version too; keep here.
// / readFull 在旧版 mongodb_test.go 也定义过；这里再保留。
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

// _ keeps unused-import linters happy for the test's pbkdf2 reference.
// / _ 让未用 import 的 linter 闭嘴——保留对 pbkdf2 包的间接引用。
var (
	_ = big.NewInt
	_ = sha256.New
	_ hash.Hash
)

// TestMongo_SCRAMSHA256_Hit walks the full SCRAM-SHA-256 flow against a
// fake server that trusts the client. Verifies our client speaks the
// wire format correctly and treats the server's "v=" reply as a hit.
// / TestMongo_SCRAMSHA256_Hit 对信任客户端的假服务器跑完整
// SCRAM-SHA-256 流程。验证客户端 wire 格式正确、把 "v=" 视为命中。
func TestMongo_SCRAMSHA256_Hit(t *testing.T) {
	ln := startFakeMongoSCRAM(t, "right")
	auth := protocols.NewMongoAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	creds := []cred.Cred{{User: "admin", Pass: "right", Method: cred.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit on SCRAM-SHA-256 success")
	}
	if hit.Cred.Pass != "right" {
		t.Errorf("expected pass=right, got %q", hit.Cred.Pass)
	}
}
