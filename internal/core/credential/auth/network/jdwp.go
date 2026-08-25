// Package protocols: JDWP (Java Debug Wire Protocol) probe.
// Package protocols：JDWP（Java Debug Wire Protocol）探测。
//
// JDWP is a binary protocol used by Java debuggers. The very first
// thing a JDWP agent expects is the "JDWP-Handshake" magic (14
// ASCII bytes, "JDWP-Handshake"). If the server replies with the
// same 14 bytes, JDWP is exposed on that port. / JDWP 是 Java 调
// 试器使用的二进制协议。JDWP 代理期望的第一件事是 "JDWP-Handshake"
// 魔数（14 ASCII 字节，"JDWP-Handshake"）。如果服务器回同样的 14
// 字节，JDWP 就暴露在该端口。
//
// Reference: https://docs.oracle.com/javase/8/docs/platform/jpda/
//
//	JDWP-Transport.html
//
// fscan v2.2.0-rc added JDWP as a new probe; this authenticator
// mirrors it. / fscan v2.2.0-rc 加了 JDWP；本认证器复刻。
//
// HARD RULE: on a hit we return. We do NOT send any JDWP command
// (no suspend / resume / class load / invoke) — the handshake is
// all we observe. / 硬性原则：命中即返回。不发任何 JDWP 命令（不
// suspend / resume / 加载类 / invoke）——握手是我们观察的全部。
package network

import (
	"context"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// jdwpHandshake is the 14-byte ASCII magic every JDWP agent expects
// as the very first message on a fresh connection. / jdwpHandshake
// 是 JDWP 代理在新建连接上期望的首条消息——14 字节 ASCII 魔数。
var jdwpHandshake = []byte("JDWP-Handshake")

// JDWPAuthenticator probes for an exposed JDWP agent by sending the
// handshake and waiting for the same bytes back. / JDWPAuthenticator
// 通过发握手并等同样的字节返回来探测 JDWP 代理是否暴露。
type JDWPAuthenticator struct{}

// NewJDWPAuthenticator returns a default-configured JDWP
// authenticator. / NewJDWPAuthenticator 返回默认配置的 JDWP 认证器。
func NewJDWPAuthenticator() *JDWPAuthenticator { return &JDWPAuthenticator{} }

func init() { credential.Register(NewJDWPAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *JDWPAuthenticator) Name() string { return "jdwp" }

// DefaultPorts implements credential.Authenticator.
// 8000 is the common default; 5005 (debug launcher), 4000 +
// 8453 (typical containerised Java). / DefaultPorts：8000 是常见
// 默认；5005（调试启动器）、4000 + 8453（典型容器化 Java）。
func (a *JDWPAuthenticator) DefaultPorts() []int {
	return []int{8000, 5005, 4000, 8453}
}

// Authenticate implements credential.Authenticator.
// / Authenticate 实现 credential.Authenticator。
//
// JDWP has no concept of user / pass — the entire "auth" is
// whether the handshake completes. We honour the contract by
// returning a Hit with Method=AuthNone (empty User/Pass), exactly
// like the BACnet / Modbus / NFS probes.
// / JDWP 没有 user/pass 概念——整个"鉴权"是握手是否完成。我们按
// 契约返回 Method=AuthNone（空 User/Pass）的 Hit，与 BACnet /
// Modbus / NFS 探测一致。
func (a *JDWPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// creds is unused for JDWP but the Authenticator interface
	// requires it. / creds 对 JDWP 无用，但 Authenticator 接口要
	// 求这个参数。
	_ = creds
	ok, err := a.attempt(ctx, host, port, timeout)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &credential.Hit{
		Cred:     credential.Cred{Method: credential.AuthNone},
		Attempts: 1,
		Time:     time.Now(),
	}, nil
}

// attempt sends the handshake and waits for the same bytes back.
// / attempt 发握手并等同样的字节返回。
func (a *JDWPAuthenticator) attempt(ctx context.Context, host string, port int, timeout time.Duration) (bool, error) {
	conn, err := credential.DialTCP(ctx, host, port, timeout)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// v0.4: dial via credential.DialTCP already sets the deadline.
	// / v0.4：通过 credential.DialTCP 拨号已设 deadline。
	if _, err := conn.Write(jdwpHandshake); err != nil {
		return false, err
	}
	buf := make([]byte, len(jdwpHandshake))
	if _, err := readFullExact(conn, buf); err != nil {
		return false, nil
	}
	// Match the magic exactly (case-sensitive, exact length).
	// / 精确匹配魔数（大小写敏感，长度精确）。
	return string(buf) == string(jdwpHandshake), nil
}

// readFull reads exactly len(buf) bytes or returns the error.
// / readFull 读恰好 len(buf) 字节或返回错误。
// (Renamed to avoid collision with the readFull in socks5.go.)
// / (重命名以避免与 socks5.go 中的 readFull 冲突。)
func readFullExact(c interface {
	Read([]byte) (int, error)
}, buf []byte) (int, error) {
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
