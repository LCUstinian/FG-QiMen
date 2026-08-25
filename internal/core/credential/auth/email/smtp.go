// Package protocols: SMTP authenticator.
// Package protocols：SMTP 认证器。
//
// Strategy: standard SMTP EHLO + AUTH LOGIN flow (RFC 4954).
// / 策略：标准 SMTP EHLO + AUTH LOGIN 流程（RFC 4954）。
//
//  1. EHLO <hostname>
//  2. Server replies 250 with capability list (including AUTH LOGIN)
//  3. AUTH LOGIN
//  4. Server replies 334 (User Name\0)
//  5. Send base64(user)
//  6. Server replies 334 (Password\0)
//  7. Send base64(pass)
//  8. Server replies 235 (auth successful) or 535 (rejected)
//
// HARD RULE: on a hit we return. We do NOT send MAIL FROM, RCPT TO,
// or any other post-auth command. / 硬性原则：命中即返回。不发
// MAIL FROM / RCPT TO 或任何认证后命令。
//
// Implicit-TLS ports (465, 587 with STARTTLS): we wrap the TCP
// conn with TLS immediately. STARTTLS upgrade for 25/587 is NOT
// implemented in v0.4 — only implicit-TLS paths. / 隐式 TLS 端口
// （465、587 STARTTLS）：立即把 TCP 连接包成 TLS。25/587 STARTTLS
// 升级 v0.4 不实现——仅隐式 TLS。
package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// SMTPAuthenticator authenticates against SMTP via AUTH LOGIN.
// SMTP authenticator 通过 AUTH LOGIN 对 SMTP 服务器认证。
type SMTPAuthenticator struct{}

// NewSMTPAuthenticator returns a default SMTP authenticator.
// NewSMTPAuthenticator 返回默认 SMTP 认证器。
func NewSMTPAuthenticator() *SMTPAuthenticator { return &SMTPAuthenticator{} }

func init() { credential.Register(NewSMTPAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *SMTPAuthenticator) Name() string { return "smtp" }

// DefaultPorts implements credential.Authenticator.
// 25 (plaintext), 465 (implicit TLS), 587 (STARTTLS — v0.4 plaintext).
// / DefaultPorts：25（明文）、465（隐式 TLS）、587（STARTTLS — v0.4 明文）。
func (a *SMTPAuthenticator) DefaultPorts() []int { return []int{25, 465, 587} }

// Authenticate implements credential.Authenticator.
// / Authenticate 实现 credential.Authenticator。
func (a *SMTPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, host, port, c.User, c.Pass, timeout)
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

// attempt runs one AUTH LOGIN round. / attempt 跑一次 AUTH LOGIN。
func (a *SMTPAuthenticator) attempt(ctx context.Context, host string, port int, user, pass string, timeout time.Duration) (bool, error) {
	conn, err := credential.DialTCP(ctx, host, port, timeout)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Port 465 is implicit TLS. / 端口 465 是隐式 TLS。
	if port == 465 {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return false, err
		}
		conn = tlsConn
	}
	br := bufio.NewReader(conn)

	// 1. EHLO. / EHLO。
	if err := writeSMTPCommand(conn, "EHLO", "fg-qimen"); err != nil {
		return false, err
	}
	if !readUntilReady(conn, br, 250) {
		return false, nil
	}

	// 2. AUTH LOGIN. / AUTH LOGIN。
	if err := writeSMTPCommand(conn, "AUTH", "LOGIN"); err != nil {
		return false, err
	}
	first, code, ok := readSMTPResponse(conn, br)
	if !ok || code != 334 {
		return false, nil
	}
	_ = first // 334 contains base64("User Name\0")

	// 3. Send base64(user). / 发 base64(user)。
	if err := writeSMTPData(conn, base64.StdEncoding.EncodeToString([]byte(user))); err != nil {
		return false, err
	}
	_, code, ok = readSMTPResponse(conn, br)
	if !ok || code != 334 {
		return false, nil
	}

	// 4. Send base64(pass). / 发 base64(pass)。
	if err := writeSMTPData(conn, base64.StdEncoding.EncodeToString([]byte(pass))); err != nil {
		return false, err
	}
	_, code, ok = readSMTPResponse(conn, br)
	if !ok {
		return false, nil
	}
	// 235 = Authentication successful. / 235 = 认证成功。
	return code == 235, nil
}

// writeSMTPCommand writes a single SMTP command line (e.g. "EHLO x").
// / writeSMTPCommand 写一行 SMTP 命令。
func writeSMTPCommand(conn interface {
	Write([]byte) (int, error)
}, verb, arg string) error {
	line := verb + " " + arg + "\r\n"
	_, err := conn.Write([]byte(line))
	return err
}

// writeSMTPData writes a single SMTP data payload (no verb prefix).
// / writeSMTPData 写 SMTP 数据负载（无动词前缀）。
func writeSMTPData(conn interface {
	Write([]byte) (int, error)
}, payload string) error {
	_, err := conn.Write([]byte(payload + "\r\n"))
	return err
}

// readUntilReady drains multi-line server reply until status code 250.
// / readUntilReady 排空多行服务器回复直到状态码 250。
func readUntilReady(conn interface {
	SetReadDeadline(time.Time) error
}, br *bufio.Reader, want int) bool {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return false
		}
		// status code is the first 3 chars; multi-line replies have
		// '-' as the 4th char (e.g. "250-AUTH LOGIN PLAIN"). Final
		// line has ' ' as the 4th char ("250 OK").
		// / 状态码是前 3 字符；多行回复第 4 字符是 '-'。最后
		// 一行第 4 字符是 ' '。
		if len(line) >= 4 && line[3] == ' ' {
			var code int
			if _, err := fmt.Sscanf(line[:3], "%d", &code); err == nil && code == want {
				return true
			}
			return false
		}
	}
}

// readSMTPResponse reads one full SMTP reply (single or multi-line).
// / readSMTPResponse 读一条完整 SMTP 回复（单行或多行）。
func readSMTPResponse(conn interface {
	SetReadDeadline(time.Time) error
}, br *bufio.Reader) (string, int, bool) {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var lines []string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return strings.Join(lines, ""), 0, false
		}
		lines = append(lines, line)
		if len(line) >= 4 && line[3] == ' ' {
			var code int
			if _, err := fmt.Sscanf(line[:3], "%d", &code); err == nil {
				return strings.Join(lines, ""), code, true
			}
			return strings.Join(lines, ""), 0, false
		}
	}
}
