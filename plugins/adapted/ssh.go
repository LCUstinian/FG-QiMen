// ssh.go — SSH plugin (Identify + Credential).
// ssh.go — SSH 插件（识别 + 凭据测试）。
//
// v0.1 implementation is hand-written from scratch (rather than copied
// from upstream) for clarity and testability. The Identify path reads
// the SSH banner; the Credential path uses golang.org/x/crypto/ssh to
// perform password authentication only — NO Session.Exec / Shell calls.
//
// v0.1 实现从零手写（而非从上游复制）以提升可读性和可测性。识别路径读取
// SSH banner；凭据测试路径使用 golang.org/x/crypto/ssh 仅做密码认证——
// 不调用 Session.Exec / Shell。
//
// Hard rule reminder: never add Session.NewSession / Exec / Shell calls
// to this file. On a credential hit, return *Result with Cred set; the
// pipeline writes to creds.txt and nothing else.
//
// 硬性原则提醒：绝不要在本文件添加 Session.NewSession / Exec / Shell
// 调用。凭据命中时返回带 Cred 字段的 *Result；管线只写入 creds.txt。
package adapted

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// sshBannerRegex parses "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1" → ["2.0", "OpenSSH_8.9p1 Ubuntu-3ubuntu0.1"].
// sshBannerRegex 解析 SSH banner。
var sshBannerRegex = regexp.MustCompile(`^SSH-([0-9.]+)-(.+?)\r?\n?$`)

// SSHPlugin implements Identify (banner grab) + Credential (password auth only).
// SSHPlugin 实现 Identify（读 banner）和 Credential（仅密码认证）。
type SSHPlugin struct{}

// NewSSHPlugin returns a fresh SSHPlugin. The returned value can be
// registered into the global plugin registry via plugins.Register.
// NewSSHPlugin 返回一个新的 SSHPlugin。可通过 plugins.Register 注册到
// 全局插件注册表。
func NewSSHPlugin() *SSHPlugin { return &SSHPlugin{} }

func init() { plugins.Register(NewSSHPlugin()) }

// Name implements plugins.Plugin.
// Name 实现 plugins.Plugin。
func (p *SSHPlugin) Name() string { return "ssh" }

// Ports implements plugins.Plugin. SSH commonly runs on 22 (and a few
// alternate ports for dev/git deployments).
// Ports 实现 plugins.Plugin。SSH 通常跑在 22（及少数开发/Git 部署的备用端口）。
func (p *SSHPlugin) Ports() []int { return []int{22, 2222, 2200, 22222} }

// Modes returns Identify | Credential. v0.1 SSH is the ONLY plugin with
// dual capability; other plugins only do Identify in v0.1.
// Modes 返回 Identify | Credential。v0.1 中 SSH 是唯一双能力插件。
func (p *SSHPlugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Identify connects to host:port, reads the SSH version banner, and
// returns a *Result on success.
//
// Identify 连接到 host:port 读取 SSH 版本 banner，成功时返回 *Result。
//
// The connection is closed immediately after reading the banner — we
// do not open a session or authenticate during Identify.
//
// 读完 banner 立即关闭连接——Identify 阶段不开 session、不认证。
func (p *SSHPlugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	timeout := 3 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if left := time.Until(d); left < timeout {
			timeout = left
		}
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	// SSH servers send their banner first; read up to 256 bytes.
	// SSH 服务器先发 banner；最多读 256 字节。
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		return nil
	}
	banner := strings.TrimSpace(string(buf[:n]))
	m := sshBannerRegex.FindStringSubmatch(banner)
	if m == nil {
		// Banner is something we don't recognize as SSH.
		// 不是我们能识别的 SSH banner。
		return nil
	}
	return &common.Result{
		Host:    host,
		Port:    port,
		Service: "ssh",
		Banner:  fmt.Sprintf("SSH %s (%s)", m[1], m[2]),
		Time:    time.Now(),
	}
}

// Credential tests the user:pass combinations against host:port.
//
// Credential 测试 user:pass 组合对 host:port 的认证。
//
// SECURITY CONTRACT: this function MUST NOT call ssh.NewSession, Exec,
// Shell, or any other post-authentication API. On a hit it returns a
// *Result with Cred set; the pipeline writes to creds.txt and does
// nothing else.
//
// 安全契约：本函数严禁调用 ssh.NewSession、Exec、Shell 或任何认证后
// API。命中时返回带 Cred 字段的 *Result；管线只写入 creds.txt，不做
// 任何其他动作。
//
// Implementation note: we use one TCP dial per (user, pass) pair rather
// than ssh.Client because the latter creates persistent state that
// callers might be tempted to use for post-auth actions. Each
// successful auth client is closed before returning.
//
// 实现注意：每个 (user, pass) 对用一次 TCP dial，而不是 ssh.Client，
// 因为后者会创建持久状态，调用方可能会被诱惑去执行认证后动作。每个
// 成功认证的 client 在返回前被关闭。
func (p *SSHPlugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	for _, c := range creds {
		if ctx.Err() != nil {
			return nil
		}
		if hit := sshTryOnce(ctx, host, port, c); hit {
			return &common.Result{
				Host:    host,
				Port:    port,
				Service: "ssh",
				Cred:    &c,
				Banner:  "auth ok",
				Time:    time.Now(),
			}
		}
	}
	return nil
}

// sshTryOnce attempts a single SSH password authentication. Returns
// true on success.
//
// sshTryOnce 尝试一次 SSH 密码认证。成功返回 true。
//
// HARD: this function does NOT call ssh.Client.NewSession / Exec / Shell.
// It only calls NewClientConn to verify the credential, then closes.
// Hard 限制：本函数不调用 ssh.Client.NewSession / Exec / Shell。只调用
// NewClientConn 验证凭据后关闭。
func sshTryOnce(ctx context.Context, host string, port int, c common.Cred) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	timeout := 3 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if left := time.Until(d); left < timeout {
			timeout = left
		}
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            []ssh.AuthMethod{ssh.Password(c.Pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // scanner
		Timeout:         timeout,
	}
	// Build a TCP conn with context-cancelable dial.
	// 用可被 context 取消的 dial 建 TCP 连接。
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	// NewClientConn performs the SSH handshake + auth. It does NOT open
	// a session.
	// NewClientConn 执行 SSH 握手 + 认证。它不打开 session。
	sshConn, _, _, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return false
	}
	// We have an authenticated client. We do NOT use it for anything.
	// Close immediately to make the intent obvious to reviewers and to
	// prevent any accidental post-auth action.
	// 我们有了一个已认证的 client。但我们不把它用于任何事情。立即关闭，
	// 让意图对 reviewer 显而易见，并防止任何意外的认证后动作。
	_ = sshConn.Close()
	return true
}
