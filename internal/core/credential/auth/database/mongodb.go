// Package database: MongoDB authenticator (high-level flow).
// Package database：MongoDB 认证器（高层流程）。
//
// File layout for the MongoDB authenticator:
//   - mongodb.go        this file: Authenticator struct + Authenticate
//     (the high-level per-cred loop)
//   - mongodb_scram.go  scramSHA256Auth + SCRAM-SHA-256 crypto helpers
//   - mongodb_bson.go   BSON encoding + MongoDB OP_MSG wire format
//
// Implements SCRAM-SHA-256 authentication (the default for modern
// MongoDB ≥ 4.0). Flow per cred:
//  1. OP_MSG { saslStart: 1, mechanism: "SCRAM-SHA-256",
//     payload: "n,,n=user,r=clientNonce" }
//  2. Server replies with conversationId + payload
//     "r=serverNonce,s=base64Salt,i=iterations".
//  3. Client computes:
//     SaltedPassword = PBKDF2-HMAC-SHA256(Normalize(pw), salt, i, 32)
//     ClientKey   = HMAC-SHA256(SaltedPassword, "Client Key")
//     StoredKey   = SHA256(ClientKey)
//     AuthMessage = client-first-bare + "," + server-first + "," + client-final-no-proof
//     ClientSig   = HMAC-SHA256(StoredKey, AuthMessage)
//     ClientProof = ClientKey XOR ClientSig
//     client-final = "c=biws,r=serverNonce,p=base64(ClientProof)"
//  4. OP_MSG { saslContinue: 1, conversationId, payload: client-final }
//  5. Server replies ok=1, done=true, payload "v=base64ServerSig"
//     on success. We treat any "v=" reply as a hit — server has
//     validated our proof; we don't recompute the server signature
//     locally (v0.1 trust-the-server).
//
// 实现 SCRAM-SHA-256 认证（MongoDB ≥ 4.0 默认）。每个凭据的流程：
//  1. OP_MSG { saslStart: 1, mechanism: "SCRAM-SHA-256",
//     payload: "n,,n=user,r=clientNonce" }
//  2. 服务器返 conversationId + payload
//     "r=serverNonce,s=base64Salt,i=iterations"。
//  3. 客户端算 SaltedPassword / ClientKey / StoredKey / AuthMessage
//     / ClientSig / ClientProof / client-final。
//  4. OP_MSG { saslContinue: 1, conversationId, payload: client-final }
//  5. 服务器返 ok=1, done=true, payload "v=base64ServerSig"
//     即成功。我们把任何 "v=" 响应视为命中——服务器已验过 proof；
//     v0.1 不本地重算 server signature（信任服务器）。
//
// HARD RULE: on a hit we return; we do NOT run any commands
// (no find / insert / update).
//
// 硬性原则：命中即返回；不跑任何命令（不 find / insert / update）。
package database

import (
	"context"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// MongoAuthenticator authenticates against MongoDB via SCRAM-SHA-256.
// MongoAuthenticator 通过 SCRAM-SHA-256 对 MongoDB 认证。
type MongoAuthenticator struct{}

// NewMongoAuthenticator returns a default MongoDB authenticator.
// NewMongoAuthenticator 返回默认 MongoDB 认证器。
func NewMongoAuthenticator() *MongoAuthenticator { return &MongoAuthenticator{} }

func init() { credential.Register(NewMongoAuthenticator()) }

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
		// Reset per-cred deadline: DialTCP sets a single deadline at
		// dial time which would expire mid-loop for large cred lists.
		// / 重置单 cred deadline：DialTCP 在拨号时设了单 deadline，
		// 大凭据列表下会在循环中途过期。
		_ = conn.SetDeadline(time.Now().Add(timeout))
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
