// Package protocols: WinRM authenticator.
//
// Strategy: HTTP POST /wsman with `Authorization: Basic <b64(user:pass)>`
// header (and minimal WSMan SOAP body). 200 = hit, 401 = miss.
// We do NOT run any WMI query (no Win32_Process, no Win32_Service).
//
// WinRM supports HTTP (5985) and HTTPS (5986). We start with HTTP
// and fall back to HTTPS. NTLM / Kerberos / CredSSP are out of
// scope for v0.1 — Basic auth is the lowest-friction path that
// proves credentials are correct.
//
// HARD RULE: on a hit we return. We do NOT enumerate WMI classes
// or run any shell command.
//
// 包 protocols：WinRM 认证器。
// 策略：HTTP POST /wsman 加 `Authorization: Basic <b64(user:pass)>`
// 头（+ 最小 WSMan SOAP body）。200 = 命中，401 = miss。我们不跑任何
// WMI query（不 Win32_Process、不 Win32_Service）。
//
// WinRM 支持 HTTP (5985) 和 HTTPS (5986)。先 HTTP 再回退 HTTPS。
// NTLM / Kerberos / CredSSP 超出 v0.1 范围——Basic auth 是证明凭据
// 正确的最低摩擦路径。
//
// 硬性原则：命中即返回。不枚举 WMI 类、不跑任何 shell 命令。
package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// WinRMAuthenticator authenticates against WinRM via HTTP Basic
// auth probe. / WinRMAuthenticator 通过 HTTP Basic 认证探测对 WinRM
// 认证。
//
// DefaultPorts returns 5985/5986 (HTTP / HTTPS WinRM). Same as
// standard Microsoft docs. / DefaultPorts 返 5985/5986（HTTP / HTTPS
// WinRM）。与微软标准文档一致。
type WinRMAuthenticator struct{}

// NewWinRMAuthenticator returns a default WinRM authenticator.
// NewWinRMAuthenticator 返回默认配置的 WinRM 认证器。
func NewWinRMAuthenticator() *WinRMAuthenticator { return &WinRMAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *WinRMAuthenticator) Name() string { return "winrm" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *WinRMAuthenticator) DefaultPorts() []int {
	return []int{5985, 5986}
}

// wsmanBody is the minimal WSMan SOAP envelope. / wsmanBody 是最小
// WSMan SOAP 信封。
//
// We send a "wsen:None" operation just to trigger HTTP auth on the
// server. The actual SOAP body doesn't matter for credential
// testing — we only need to know if the server accepts the cred.
// / 我们发一个 "wsen:None" 操作只是为了触发服务器 HTTP auth。实际
// SOAP body 对凭据测试无关——我们只需要知道服务器是否接受凭据。
const wsmanBody = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:wsman="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd">
  <s:Header>
    <wsa:Action s:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2004/09/transfer/Get</wsa:Action>
    <wsa:To>HTTP://localhost:5985/wsman</wsa:To>
    <wsman:ResourceURI s:mustUnderstand="true">http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ComputerSystem</wsman:ResourceURI>
  </s:Header>
  <s:Body/>
</s:Envelope>`

// Authenticate implements credential.Authenticator. Tries each cred in
// order (HTTP first, then HTTPS); returns the first hit or nil.
// / Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred（先 HTTP
// 再 HTTPS）；首个命中返回 Hit。
func (a *WinRMAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		// Fall back to "Administrator" (Windows default admin) if c.User
		// is empty. / c.User 空时回退到 "Administrator"（Windows 默认管理员）。
		user := c.User
		if user == "" {
			user = "Administrator"
		}
		hit, err := a.probe(ctx, addr, user, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if hit {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// probe sends one HTTP POST /wsman with Basic auth. Returns
// (true, nil) on a hit, (false, nil) on a miss, (false, err) on
// network failure.
//
// probe 跑一次 HTTP POST /wsman 加 Basic auth。命中返 (true, nil)，
// miss 返 (false, nil)，网络错返 (false, err)。
func (a *WinRMAuthenticator) probe(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	tr := &http.Transport{
		TLSClientConfig:       transport.TLSConfig(false),
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	// M15: port 5986 is HTTPS WinRM — use https://. Port 5985 stays
	// plaintext http://. / M15：端口 5986 是 HTTPS WinRM——用
	// https://。端口 5985 保持明文 http://。
	scheme := "http"
	if strings.HasSuffix(addr, ":5986") {
		scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", scheme+"://"+addr+"/wsman",
		strings.NewReader(wsmanBody))
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "fg-qimen/0.1")
	req.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")
	if user != "" || pass != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 200 = hit (server accepted the Basic auth). 401 / 403 = miss.
	// / 200 = 命中（服务器接受了 Basic auth）。401 / 403 = miss。
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return true, nil
	}
	return false, nil
}

// init registers the WinRM authenticator. / init 注册 WinRM 认证器。
func init() {
	credential.Register(NewWinRMAuthenticator())
}
