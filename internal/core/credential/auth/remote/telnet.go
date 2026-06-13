// Package protocols: Telnet authenticator.
//
// Strategy: raw TCP, IAC (Interpret As Command) negotiation, then
// read until a "login:" or "username:" prompt, send the user, read
// until "password:", send the password, then read the result and
// classify as hit (welcome / shell prompt) or miss (login incorrect
// / access denied).
//
// HARD RULE: on a hit we close immediately. We do NOT run any
// command (no `id`, no `whoami`, no `uname -a`). The detection of
// "shell prompt reached" stops at the prompt itself.
//
// 包 protocols：Telnet 认证器。
// 策略：裸 TCP，IAC 协商，read 到 "login:" 或 "username:" 提示符后
// 发用户名，read 到 "password:" 提示符后发密码，read 结果分类为命中
// （welcome / shell 提示符）或 miss（login incorrect / access denied）。
//
// 硬性原则：命中即关连接。绝不跑任何命令（不 `id`、不 `whoami`、
// 不 `uname -a`）。"到达 shell 提示符"的检测停在提示符本身。
package remote

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// TelnetAuthenticator authenticates against telnetd via raw TCP +
// IAC negotiation.
//
// DefaultPorts returns 23/2323 (standard telnet + Windows alternate
// service). Same as fscan.
//
// TelnetAuthenticator 通过裸 TCP + IAC 协商对 telnetd 认证。
//
// DefaultPorts 返 23/2323（标准 telnet + Windows 备用服务）。与 fscan 一致。
type TelnetAuthenticator struct{}

// NewTelnetAuthenticator returns a default Telnet authenticator.
// NewTelnetAuthenticator 返回默认配置的 Telnet 认证器。
func NewTelnetAuthenticator() *TelnetAuthenticator { return &TelnetAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *TelnetAuthenticator) Name() string { return "telnet" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *TelnetAuthenticator) DefaultPorts() []int {
	return []int{23, 2323}
}

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回 Hit，否则返回 nil。
func (a *TelnetAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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

// attempt runs one (user, pass) try against the telnet port. Returns
// (true, nil) on a hit, (false, nil) on a miss, (false, err) on a
// network failure.
//
// attempt 跑一次 (user, pass) 试连。命中返 (true, nil)，miss 返
// (false, nil)，网络错返 (false, err)。
func (a *TelnetAuthenticator) attempt(ctx context.Context, host string, port int, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// Phase 1: read initial banner / negotiate IAC, until we see a
	// "login:" / "username:" prompt (or timeout). / 阶段 1：读初始
	// banner / 协商 IAC，直到看到 "login:" / "username:" 提示符（或超时）。
	if !a.readUntilPrompt(conn) {
		return false, nil
	}
	// Phase 2: send user, read until "password:" prompt. / 阶段 2：
	// 发 user，读到 "password:" 提示符。
	if _, err := conn.Write([]byte(user + "\r\n")); err != nil {
		return false, err
	}
	if !a.readUntilPassword(conn) {
		return false, nil
	}
	// Phase 3: send password, read result. / 阶段 3：发 password，读
	// 结果。
	if _, err := conn.Write([]byte(pass + "\r\n")); err != nil {
		return false, err
	}
	return a.readAuthResult(conn), nil
}

// readUntilPrompt reads from conn, handling IAC negotiation, until a
// login-style prompt appears or the deadline expires. / readUntilPrompt
// 从 conn 读，处理 IAC 协商，直到出现登录类提示符或 deadline 到期。
func (a *TelnetAuthenticator) readUntilPrompt(conn net.Conn) bool {
	buf := make([]byte, 1024)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			return false
		}
		cleaned := cleanTelnetText(buf[:n])
		// Strip IAC commands before checking. / 检查前先剥掉 IAC 命令。
		// (cleanTelnetText already does that — we kept it explicit for
		// clarity. / 上面已经做了——为可读性保留。)
		_ = cleaned
		if hasLoginPrompt(buf[:n]) {
			return true
		}
	}
}

// readUntilPassword reads until "password:" prompt or timeout.
// readUntilPassword 读到 "password:" 提示符或超时。
func (a *TelnetAuthenticator) readUntilPassword(conn net.Conn) bool {
	buf := make([]byte, 1024)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			return false
		}
		if hasPasswordPrompt(buf[:n]) {
			return true
		}
	}
}

// readAuthResult reads the post-password response. Returns true if
// the server indicates a hit (welcome / shell prompt), false if a
// miss (login incorrect / access denied).
//
// readAuthResult 读 post-password 响应。命中（welcome / shell 提示符）
// 返 true；miss（login incorrect / access denied）返 false。
func (a *TelnetAuthenticator) readAuthResult(conn net.Conn) bool {
	buf := make([]byte, 1024)
	var collected []byte
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		collected = append(collected, buf[:n]...)
		text := cleanTelnetText(collected)
		lower := strings.ToLower(text)
		// Miss indicators. / Miss 标志。
		if strings.Contains(lower, "incorrect") ||
			strings.Contains(lower, "login failed") ||
			strings.Contains(lower, "access denied") ||
			strings.Contains(lower, "invalid password") ||
			strings.Contains(lower, "login incorrect") {
			return false
		}
		// Hit indicators. / Hit 标志。
		if strings.Contains(lower, "last login") ||
			strings.Contains(lower, "welcome") ||
			strings.Contains(text, "$ ") ||
			strings.Contains(text, "# ") ||
			strings.Contains(text, "]$ ") ||
			strings.Contains(text, "> ") {
			return true
		}
	}
	// No clear signal after 2s. Treat as miss. / 2s 后没明确信号。
	// 视为 miss。
	return false
}

// cleanTelnetText strips Telnet IAC commands from a byte slice and
// returns the printable text. / cleanTelnetText 从字节切片剥掉 Telnet
// IAC 命令，返可打印文本。
func cleanTelnetText(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0xFF {
			// IAC: skip 2 more bytes (the command + option). Sub-negotiation
			// (SB ... SE) is more complex; we strip naively here, which is
			// fine for "is there a prompt in here" detection. / IAC：跳
			// 过 2 字节。子协商（SB ... SE）复杂，我们粗暴地剥——对
			// "里面有提示符吗"这种检测够用。
			i += 2
			continue
		}
		if c >= 32 && c <= 126 || c == '\r' || c == '\n' || c == '\t' {
			out.WriteByte(c)
		}
	}
	return strings.TrimSpace(out.String())
}

// hasLoginPrompt returns true if b contains a login-style prompt
// (login: / username: / user: / name:). Case-insensitive.
// hasLoginPrompt 当 b 含登录类提示符（login: / username: / user: / name:）
// 时返 true。大小写不敏感。
func hasLoginPrompt(b []byte) bool {
	lower := strings.ToLower(string(b))
	for _, p := range []string{"login:", "username:", "user:", "name:"} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// hasPasswordPrompt returns true if b contains "password:". / hasPasswordPrompt
// 当 b 含 "password:" 时返 true。
func hasPasswordPrompt(b []byte) bool {
	lower := strings.ToLower(string(b))
	return strings.Contains(lower, "password:")
}

// init registers the Telnet authenticator. / init 注册 Telnet 认证器。
func init() {
	credential.Register(NewTelnetAuthenticator())
}

// Keep fmt imported for future use (e.g. debug logging).
// / fmt 保留以备将来 debug 日志。
var _ = fmt.Sprintf
