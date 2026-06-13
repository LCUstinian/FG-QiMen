// Package protocols: POP3 authenticator.
//
// Strategy: send USER <name> + PASS <pass> per RFC 1939. +OK = hit,
// -ERR = miss. We do NOT issue any LIST/RETR/STAT/DELE — cred test
// only.
//
// HARD RULE: on a hit we QUIT and close. No mailbox read.
//
// 包 protocols：POP3 认证器。
// 策略：按 RFC 1939 发 USER <name> + PASS <pass>。+OK = 命中，
// -ERR = miss。我们不发任何 LIST/RETR/STAT/DELE——只测凭据。
//
// 硬性原则：命中即 QUIT 关连接。不读邮箱。
package protocols

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
)

// POP3Authenticator authenticates against POP3 servers via raw
// RFC 1939 USER/PASS. / POP3Authenticator 通过 RFC 1939 USER/PASS
// 对 POP3 认证。
//
// DefaultPorts returns 110/995 (plaintext / POP3S). We don't open
// TLS ourselves for 995 — v0.2+ can add a TLS dialer.
// / DefaultPorts 返 110/995（明文 / POP3S）。v0.1 不为 995 开 TLS——
// v0.2+ 可以加 TLS dialer。
type POP3Authenticator struct{}

// NewPOP3Authenticator returns a default POP3 authenticator.
// NewPOP3Authenticator 返回默认配置的 POP3 认证器。
func NewPOP3Authenticator() *POP3Authenticator { return &POP3Authenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *POP3Authenticator) Name() string { return "pop3" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *POP3Authenticator) DefaultPorts() []int {
	return []int{110, 995}
}

// Authenticate implements cred.Authenticator. Tries each cred in
// order; returns the first hit. / Authenticate 实现 cred.Authenticator。
// 按顺序尝试每个 cred；首个命中返回 Hit。
func (a *POP3Authenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != cred.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one (user, pass) try against the POP3 port. / attempt
// 跑一次 (user, pass) 试连。
func (a *POP3Authenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	// Read greeting. / 读 greeting。
	if !readPOP3OK(br) {
		return false, nil
	}
	// USER. / USER。
	if _, err := bw.WriteString("USER " + user + "\r\n"); err != nil {
		return false, err
	}
	if err := bw.Flush(); err != nil {
		return false, err
	}
	if !readPOP3OK(br) {
		return false, nil
	}
	// PASS. / PASS。
	if _, err := bw.WriteString("PASS " + pass + "\r\n"); err != nil {
		return false, err
	}
	if err := bw.Flush(); err != nil {
		return false, err
	}
	ok := readPOP3OK(br)
	// QUIT regardless of result. / 无论结果都 QUIT。
	_, _ = bw.WriteString("QUIT\r\n")
	_ = bw.Flush()
	return ok, nil
}

// readPOP3OK returns true if the next line starts with "+OK".
// / readPOP3OK 当下一行以 "+OK" 开头时返 true。
func readPOP3OK(br *bufio.Reader) bool {
	line, err := br.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimRight(line, "\r\n"), "+OK")
}

// init registers the POP3 authenticator. / init 注册 POP3 认证器。
func init() {
	cred.Register(NewPOP3Authenticator())
}

// Keep fmt import alive for future debug logging. / fmt 保留供将来
// debug 日志。
var _ = fmt.Sprintf
