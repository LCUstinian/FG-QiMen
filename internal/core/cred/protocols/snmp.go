// Package protocols: SNMP community-string authenticator.
//
// Strategy: send an SNMPv2c GET request for sysDescr.0 with the
// candidate community string. Success = response received. Failure
// = timeout or "noSuchName" + community not accepted.
//
// We do NOT support SNMPv3 USM in v0.1 — it's a much larger
// handshake (engine discovery, user hashing, etc.) and out of
// scope. v0.2+ adds SNMPv3.
//
// HARD RULE: on a hit we return. We do NOT walk the MIB or do any
// write operation (SET, INFORM, TRAP).
//
// 包 protocols：SNMP community-string 认证器。
// 策略：发 SNMPv2c GET 请求取 sysDescr.0，用候选 community string。
// 成功 = 收到响应。失败 = 超时或 "noSuchName" + community 不接受。
//
// v0.1 不支持 SNMPv3 USM——握手更大（engine 发现、user hash 等），
// 超范围。v0.2+ 加 SNMPv3。
//
// 硬性原则：命中即返回。不 walk MIB、不做任何写操作（SET/INFORM/TRAP）。
package protocols

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
)

// SNMPAuthenticator authenticates against SNMP via v2c community
// string. / SNMPAuthenticator 通过 v2c community string 对 SNMP 认证。
//
// DefaultPorts returns 161/162 (SNMP / SNMPTRAP). We send to 161;
// 162 is for receiving traps, not for queries. / DefaultPorts 返
// 161/162（SNMP / SNMPTRAP）。我们发到 161；162 是收 trap 的，不查
// 询。
type SNMPAuthenticator struct{}

// NewSNMPAuthenticator returns a default SNMP authenticator.
// NewSNMPAuthenticator 返回默认配置的 SNMP 认证器。
func NewSNMPAuthenticator() *SNMPAuthenticator { return &SNMPAuthenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *SNMPAuthenticator) Name() string { return "snmp" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *SNMPAuthenticator) DefaultPorts() []int {
	return []int{161}
}

// Authenticate implements cred.Authenticator. Tries each community
// string as the password (user is empty for SNMPv2c). / Authenticate
// 实现 cred.Authenticator。按顺序尝试每个 community string（user 为空）。
func (a *SNMPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
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
		ok, err := a.attempt(ctx, addr, c.Pass, timeout)
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

// attempt tries one community string against the SNMP port.
// / attempt 用一个 community string 试连 SNMP 端口。
func (a *SNMPAuthenticator) attempt(ctx context.Context, addr, community string, timeout time.Duration) (bool, error) {
	host, port, _ := net.SplitHostPort(addr)
	if host == "" {
		host = addr
	}
	portNum := 161
	if p, _ := strconv.Atoi(port); p > 0 {
		portNum = p
	}
	target := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(portNum),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   timeout,
		Retries:   0, // we already iterate creds; don't retry
	}
	if err := target.Connect(); err != nil {
		return false, err
	}
	defer target.Conn.Close()
	// GET sysDescr.0.1.3.6.1.2.1.1.1.0. Any response (even noSuchObject)
	// indicates the community was accepted. / GET sysDescr.0
	// (1.3.6.1.2.1.1.1.0)。任何响应（含 noSuchObject）即 community
	// 接受。
	result, err := target.Get([]string{"1.3.6.1.2.1.1.1.0"})
	if err != nil {
		// "noSuchName" or similar after a valid PDU response also
		// means community accepted. gosnmp returns noSuchName as an
		// error in this version. / 收到合法 PDU 后的 "noSuchName" 等
		// 也表示 community 接受。gosnmp 此版本把 noSuchName 返为
		// error。
		// We treat any non-context error as a successful auth (the
		// server talked to us with this community). / 任何非 ctx
		// 错误都视为 auth 成功（服务器用这个 community 跟我们说话了）。
		_ = fmt.Sprintf("snmp: %v", err)
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		// Heuristic: a connection-level error (timeout, refused) means
		// the community was wrong. / 启发式：连接级错（超时、拒绝）
		// 意味着 community 错。
		if isConnError(err) {
			return false, nil
		}
		// Protocol-level error (e.g. noSuchName) — community accepted.
		// / 协议级错（如 noSuchName）——community 接受。
		_ = result
		return true, nil
	}
	_ = result
	return true, nil
}

// isConnError returns true if err is a connection-level error
// (timeout / refused / reset) rather than a protocol-level error
// (noSuchName etc.). / isConnError 当 err 是连接级错（超时/拒绝/
// 重置）而非协议级错（noSuchName 等）时返 true。
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	keywords := []string{"timeout", "refused", "reset", "unreachable", "no such host", "i/o timeout"}
	for _, k := range keywords {
		if contains(msg, k) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(sub) <= len(s) && (s == sub || indexOfStr(s, sub) >= 0)
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// init registers the SNMP authenticator. / init 注册 SNMP 认证器。
func init() {
	cred.Register(NewSNMPAuthenticator())
}
