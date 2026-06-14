// mongodb_scram.go — SCRAM-SHA-256 auth flow + crypto helpers for
// the MongoDB authenticator. Extracted from mongodb.go (was one
// 457-line file) so the high-level orchestrator in mongodb.go and
// the BSON + wire-protocol details in mongodb_bson.go can evolve
// independently.
//
// mongodb_scram.go — MongoDB authenticator 的 SCRAM-SHA-256 流程 +
// 加密 helper。从 mongodb.go（原 457 行）抽出；让 mongodb.go 的高层
// 编排与 mongodb_bson.go 的 BSON+线协议可独立演化。
//
// See mongodb.go for the full SCRAM-SHA-256 algorithm spec; the
// outline is:
//
//	1. client-first  ("n,,n=user,r=clientNonce")
//	2. server-first  ("r=serverNonce,s=base64Salt,i=iterations")
//	3. client-proof (SaltedPassword, ClientKey, StoredKey, ClientSig, XOR)
//	4. client-final ("c=biws,r=serverNonce,p=base64(ClientProof)")
//	5. server-final ("v=base64ServerSig" on success)
//
// We treat any "v=" reply as a hit; we don't recompute the server
// signature locally (v0.1 trusts the server).
package database

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

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
