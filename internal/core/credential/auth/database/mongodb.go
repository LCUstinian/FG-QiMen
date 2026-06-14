// Package protocols: MongoDB authenticator.
// Package protocols：MongoDB 认证器。
//
// Implements SCRAM-SHA-256 authentication (the default for modern
// MongoDB ≥ 4.0). Flow per cred:
//   1. OP_MSG { saslStart: 1, mechanism: "SCRAM-SHA-256",
//               payload: "n,,n=user,r=clientNonce" }
//   2. Server replies with conversationId + payload
//      "r=serverNonce,s=base64Salt,i=iterations".
//   3. Client computes:
//        SaltedPassword = PBKDF2-HMAC-SHA256(Normalize(pw), salt, i, 32)
//        ClientKey   = HMAC-SHA256(SaltedPassword, "Client Key")
//        StoredKey   = SHA256(ClientKey)
//        AuthMessage = client-first-bare + "," + server-first + "," + client-final-no-proof
//        ClientSig   = HMAC-SHA256(StoredKey, AuthMessage)
//        ClientProof = ClientKey XOR ClientSig
//        client-final = "c=biws,r=serverNonce,p=base64(ClientProof)"
//   4. OP_MSG { saslContinue: 1, conversationId, payload: client-final }
//   5. Server replies ok=1, done=true, payload "v=base64ServerSig"
//      on success. We treat any "v=" reply as a hit — server has
//      validated our proof; we don't recompute the server signature
//      locally (v0.1 trust-the-server).
//
// 实现 SCRAM-SHA-256 认证（MongoDB ≥ 4.0 默认）。每个凭据的流程：
//   1. OP_MSG { saslStart: 1, mechanism: "SCRAM-SHA-256",
//               payload: "n,,n=user,r=clientNonce" }
//   2. 服务器返 conversationId + payload
//      "r=serverNonce,s=base64Salt,i=iterations"。
//   3. 客户端算 SaltedPassword / ClientKey / StoredKey / AuthMessage
//      / ClientSig / ClientProof / client-final。
//   4. OP_MSG { saslContinue: 1, conversationId, payload: client-final }
//   5. 服务器返 ok=1, done=true, payload "v=base64ServerSig"
//      即成功。我们把任何 "v=" 响应视为命中——服务器已验过 proof；
//      v0.1 不本地重算 server signature（信任服务器）。
//
// HARD RULE: on a hit we return; we do NOT run any commands
// (no find / insert / update).
//
// 硬性原则：命中即返回；不跑任何命令（不 find / insert / update）。
package database

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// MongoAuthenticator authenticates against MongoDB via SCRAM-SHA-256.
// MongoAuthenticator 通过 SCRAM-SHA-256 对 MongoDB 认证。
type MongoAuthenticator struct{}

// NewMongoAuthenticator returns a default MongoDB authenticator.
// NewMongoAuthenticator 返回默认 MongoDB 认证器。
func NewMongoAuthenticator() *MongoAuthenticator { return &MongoAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *MongoAuthenticator) Name() string { return "mongodb" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *MongoAuthenticator) DefaultPorts() []int { return []int{27017, 27018} }

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first successful Hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个成功
// 返回 Hit，否则返回 nil。
func (a *MongoAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	conn, err := credential.DialTCP(ctx, host, port, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		// MongoDB default user is "admin" in the admin db. Use
		// c.User if set, else "admin".
		// / MongoDB 默认 user 是 admin db 下的 "admin"。c.User 设了就用
		// c.User，否则 "admin"。
		user := c.User
		if user == "" {
			user = "admin"
		}
		ok, err := scramSHA256Auth(ctx, conn, user, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// scramSHA256Auth performs one SCRAM-SHA-256 auth round. Returns true on
// a server-side "v=" reply (server verified the proof). / scramSHA256Auth
// 跑一次 SCRAM-SHA-256 认证。服务器回 "v=" 即返回 true（服务器已验 proof）。
func scramSHA256Auth(_ context.Context, conn net.Conn, user, pass string, _ time.Duration) (bool, error) {
	// Step 1: build client-first. / Step 1：构造 client-first。
	nonce := randomMongoNonce(24)
	clientFirstBare := fmt.Sprintf("n=%s,r=%s", mongoUserName(user), nonce)
	clientFirst := "n,," + clientFirstBare

	// Encode saslStart BSON doc. / 编码 saslStart BSON 文档。
	startDoc := bsonSaslStart("SCRAM-SHA-256", clientFirst)
	resp, err := mongoOpMsg(conn, startDoc)
	if err != nil {
		return false, err
	}
	convID, payload2, err := mongoParseSaslReply(resp)
	if err != nil {
		return false, err
	}
	if payload2 == "" {
		// Server rejected (e.g. auth not enabled). / 服务器拒了。
		return false, nil
	}
	parts := splitSCRAMFields(payload2)
	recvNonce := parts["r"]
	saltB64 := parts["s"]
	iterStr := parts["i"]
	if recvNonce == "" || saltB64 == "" || iterStr == "" {
		return false, nil
	}
	if !strings.HasPrefix(recvNonce, nonce) {
		// Server tampered with the nonce. / 服务器篡改了 nonce。
		return false, nil
	}
	iter := 0
	for _, c := range iterStr {
		if c < '0' || c > '9' {
			return false, nil
		}
		iter = iter*10 + int(c-'0')
	}
	if iter < 1 {
		iter = 10000 // MongoDB default for SCRAM-SHA-256
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return false, nil
	}

	// Step 2: compute clientProof. / Step 2：算 clientProof。
	clientFinalNoProof := fmt.Sprintf("c=biws,r=%s", recvNonce)
	authMsg := clientFirstBare + "," + payload2 + "," + clientFinalNoProof
	saltedPass := pbkdf2.Key([]byte(pass), salt, iter, 32, sha256.New)
	clientKey := hmacSHA256(saltedPass, []byte("Client Key"))
	storedKey := sha256Sum(clientKey)
	clientSig := hmacSHA256(storedKey, []byte(authMsg))
	proofBytes := xorBytes(clientKey, clientSig)
	proofB64 := base64.StdEncoding.EncodeToString(proofBytes)
	clientFinal := clientFinalNoProof + ",p=" + proofB64

	// Step 3: send saslContinue. / Step 3：发 saslContinue。
	continueDoc := bsonSaslContinue(convID, clientFinal)
	resp2, err := mongoOpMsg(conn, continueDoc)
	if err != nil {
		return false, err
	}
	_, payload3, err := mongoParseSaslReply(resp2)
	if err != nil {
		return false, err
	}
	if payload3 == "" {
		return false, nil
	}
	// payload3 should be "v=<base64 server signature>" on success.
	// / payload3 成功时是 "v=<base64 服务器签名>"。
	vParts := splitSCRAMFields(payload3)
	if vParts["v"] != "" {
		return true, nil
	}
	return false, nil
}

// randomMongoNonce returns a base64-friendly alphanumeric string of
// given length. / randomMongoNonce 返回给定长度的类 base64 字母数字串。
func randomMongoNonce(n int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

// mongoUserName returns the saslprepped username. We don't do full
// SASLprep for v0.1 — usernames rarely have special chars.
// / mongoUserName 返回 saslprepped 用户名。v0.1 不做完整 SASLprep——
// 用户名很少含特殊字符。
func mongoUserName(s string) string { return strings.ReplaceAll(s, "=", "=3D") }

// splitSCRAMFields splits "k=v,k=v,..." into a map. / splitSCRAMFields 把
// "k=v,k=v,..." 拆成 map。
func splitSCRAMFields(s string) map[string]string {
	out := make(map[string]string)
	for _, p := range strings.Split(s, ",") {
		if eq := strings.IndexByte(p, '='); eq > 0 {
			out[p[:eq]] = p[eq+1:]
		}
	}
	return out
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────
// BSON / wire helpers
// ─────────────────────────────────────────────────────────────────────

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
