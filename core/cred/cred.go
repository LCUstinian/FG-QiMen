// Package cred implements credential (brute-force) testing.
// Package cred 实现凭据（爆破）测试。
//
// Architecture:
//   - Cred      : a single (user, pass) pair with an auth method
//   - Pool      : in-memory credential set with dedup; loads from flags/files
//   - Loader    : reads -user/-pass/-userfile/-passfile into a Pool
//   - Scheduler : per-target throttling (avoid hammering a single host)
//   - Authenticator interface : per-protocol authentication (SSH/FTP/MySQL/...)
//   - protocols/ : concrete Authenticator implementations
//
// HARD RULE: this package performs AUTHENTICATION ONLY. It MUST NOT
// open sessions, execute commands, write files, or take any other
// post-authentication action. The pipeline writes hits to creds.txt
// and nothing else.
//
// 硬性原则：本包只做认证。严禁打开 session、执行命令、写文件或任何
// 认证后动作。命中由管线写入 creds.txt，不做其他事。
package cred

import "time"

// AuthMethod is the credential authentication method.
// AuthMethod 是凭据认证方式。
type AuthMethod string

const (
	// AuthPassword is a plain username+password pair.
	// AuthPassword 是明文用户名+密码对。
	AuthPassword AuthMethod = "password"
	// AuthKey is a username + PEM-encoded private key. / AuthKey 是
	// 用户名 + PEM 编码的私钥。
	AuthKey AuthMethod = "key"
	// AuthToken is a bearer-style token (HTTP basic / API key). v0.2+.
	// AuthToken 是 bearer 风格 token（HTTP basic / API key）。v0.2+。
	AuthToken AuthMethod = "token"
)

// Cred is a single credential to test. The Method field selects how
// the Authenticator interprets the payload.
//
// Cred 是单个待测凭据。Method 字段决定 Authenticator 如何解析 payload。
type Cred struct {
	User string
	Pass string
	// KeyPath is the path to a private key file (AuthKey). / KeyPath
	// 是私钥文件路径（AuthKey）。
	KeyPath string
	Method  AuthMethod
}

// Equal returns true if two Creds are the same (used for dedup).
// Equal 返回两个 Cred 是否相同（用于去重）。
func (c Cred) Equal(o Cred) bool {
	return c.User == o.User && c.Pass == o.Pass && c.KeyPath == o.KeyPath && c.Method == o.Method
}

// String returns a one-line human-readable representation. Used for
// creds.txt output and log lines. / String 返回单行可读表示；用于
// creds.txt 输出和日志。
func (c Cred) String() string {
	switch c.Method {
	case AuthKey:
		return c.User + " (key:" + c.KeyPath + ")"
	default:
		return c.User + " / " + c.Pass
	}
}

// Hit represents a successful authentication. / Hit 表示一次认证成功。
type Hit struct {
	Host    string
	Port    int
	Service string
	Cred    Cred
	// RTT is the wall-clock time spent on this single auth attempt.
	// RTT 是本次认证尝试的耗时。
	RTT time.Duration
	// Attempts is how many (user, pass) pairs were tried before this hit.
	// 命中前已尝试的 (user, pass) 对数。
	Attempts int
	Time     time.Time
}
