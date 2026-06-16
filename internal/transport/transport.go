// Package transport — process-wide insecure-mode flags for TLS / SSH
// probes, plus small constructor helpers that consult them.
//
// Why this exists: the v0.2 audit (P1#3, P1#4) found 8 sites that
// hard-coded tls.Config{InsecureSkipVerify: true} and 1 site with
// ssh.InsecureIgnoreHostKey() — each comment-marked with
// `//nolint:gosec`. The right fix is "default to verifying, opt in
// via a runtime flag", not "rewrite every site's signature".
//
// To avoid changing the Authenticator / plugin method signatures
// (which would touch 27+ protocols), we expose package-level atomic
// flags set once at process startup from cobra. Sites that need a
// transport call transport.TLSConfig(insecureOverride) which returns
// a *tls.Config respecting the flag, or transport.SSHHostKeyCallback
// for the SSH equivalent.
//
// Thread-safety: atomic.Bool is the lock-free Go 1.19+ primitive.
// All reads are race-free; the single write happens at process start
// before any scan goroutine, so there's no torn read.
//
// Package transport — TLS / SSH 探测用的进程级 insecure-mode 标志，
// 加上读取这些标志的小构造 helper。
//
// 存在原因：v0.2 审计（P1#3、P1#4）发现 8 处硬编码
// tls.Config{InsecureSkipVerify: true} 和 1 处 ssh.InsecureIgnoreHostKey()，
// 每处都带 //nolint:gosec。正确修法是"默认校验，运行时 opt-in"，不是
// "改 27+ 协议的接口签名"。
//
// 为避免改 Authenticator / plugin 方法签名（会触及 27+ 协议），我们
// 暴露进程级 atomic 标志，cobra 启动时一次性设置。需要 transport 的
// 站点调 transport.TLSConfig(insecureOverride) 返回尊重该 flag 的
// *tls.Config，或 SSH 用 transport.SSHHostKeyCallback。
//
// 线程安全：用 atomic.Bool（Go 1.19+ 无锁原语）。所有读无竞争；单次写
// 在进程启动、任何扫描 goroutine 之前，所以不会有撕裂读。
package transport

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// InsecureTLS is the process-wide gate for tls.Config.InsecureSkipVerify.
// Set true at startup only when the operator passes --insecure-tls.
// Read by TLSConfig() at probe construction time.
//
// InsecureTLS 是 tls.Config.InsecureSkipVerify 的进程级门。仅在操作员
// 传 --insecure-tls 时于启动时设 true。probe 构造时由 TLSConfig() 读。
var InsecureTLS atomic.Bool

// InsecureSSH is the process-wide gate for SSH host-key verification.
// Set true at startup only when the operator passes --insecure-ssh.
// Read by SSHHostKeyCallback() at probe construction time.
//
// InsecureSSH 是 SSH 主机密钥校验的进程级门。仅在操作员传 --insecure-ssh
// 时于启动时设 true。probe 构造时由 SSHHostKeyCallback() 读。
var InsecureSSH atomic.Bool

// KnownHostsFile is the operator-supplied known_hosts file path. Empty
// means "no known_hosts" — in that case SSHHostKeyCallback falls back
// to the InsecureSSH flag (so the default with no flag and no file is
// the v0.2 behavior: InsecureIgnoreHostKey, with a clear warning).
//
// KnownHostsFile 是操作员提供的 known_hosts 文件路径。空表示"无 known_hosts"——
// 此时 SSHHostKeyCallback 退到 InsecureSSH flag（所以"无 flag 且无文
// 件"的默认是 v0.2 行为：InsecureIgnoreHostKey，带清晰警告）。
var KnownHostsFile atomic.Pointer[string]

// TLSConfig returns a *tls.Config for HTTPS / TLS-wrapped probes.
// Default: chain + hostname verification ON. If --insecure-tls was
// passed, verification is disabled.
//
// override: pass true to force insecure (rare; intended for one-off
// unit tests that need to talk to a self-signed test server). The
// --insecure-tls flag is still respected: passing override=true with
// the flag unset is fine; passing override=false with the flag set
// is silently downgraded to insecure (we don't want a caller to be
// able to "opt out" of an explicit operator decision).
//
// TLSConfig 为 HTTPS / TLS-wrapped 探测返回 *tls.Config。
// 默认：链 + 主机名校验开。若传了 --insecure-tls，禁用校验。
//
// override：传 true 强制 insecure（少见；供对接自签测试 server 的
// 单测用）。--insecure-tls flag 始终被尊重：flag 未设时传
// override=true 是 OK 的；flag 设了但 override=false 会被静默降级到
// insecure（不允许调用者"opt out"操作员的显式决定）。
func TLSConfig(override bool) *tls.Config {
	cfg := &tls.Config{}
	if override || InsecureTLS.Load() {
		cfg.InsecureSkipVerify = true //nolint:gosec // operator-opt-in
	}
	return cfg
}

// SSHHostKeyCallback returns an ssh.HostKeyCallback for SSH probes.
// Resolution order:
//  1. KnownHostsFile is set → load it, return FixedHostKey callback.
//     First-time hosts cause a connection error (TOFU, real verify).
//     M9 audit fix: if loading fails, return a callback that always
//     rejects (NOT InsecureIgnoreHostKey), so the operator's explicit
//     "verify" intent is honored rather than silently downgraded.
//  2. --insecure-ssh flag set → return InsecureIgnoreHostKey
//     (with the operator's understanding that MITM is possible).
//  3. Neither → return InsecureIgnoreHostKey BUT log a one-line
//     warning to stderr that the scan is unauthenticated w.r.t.
//     host identity. The v0.2 default behaviour (option 3) is
//     retained for backward compatibility; the v0.3+ plan is to
//     flip the default to "no known_hosts → refuse to spray" and
//     require either --insecure-ssh or -o KnownHostsFile=.
//
// SSHHostKeyCallback 为 SSH 探测返回 ssh.HostKeyCallback。
// 解析顺序：
//  1. 设了 KnownHostsFile → 加载，返回 FixedHostKey callback。首次见到
//     的主机导致连接错误（TOFU，真校验）。
//     M9 审计修法：若加载失败，返回始终拒绝的 callback（而非
//     InsecureIgnoreHostKey），尊重操作员显式的"校验"意图，而非静默降级。
//  2. 设了 --insecure-ssh flag → 返回 InsecureIgnoreHostKey（操作员
//     知晓可能 MITM）。
//  3. 都没有 → 返回 InsecureIgnoreHostKey，但向 stderr 输出一行警告：
//     本次扫描在主机身份层面是未认证的。v0.2 默认行为（选项 3）保
//     留以向后兼容；v0.3+ 计划把默认翻成"无 known_hosts → 拒绝喷
//     洒"，强制 --insecure-ssh 或 -o KnownHostsFile=。
func SSHHostKeyCallback() ssh.HostKeyCallback {
	// Read the known_hosts path atomically; the pointer load is
	// race-free in Go's memory model.
	// 原子读 known_hosts 路径；pointer load 在 Go 内存模型下无竞争。
	if p := KnownHostsFile.Load(); p != nil && *p != "" {
		// Try to load the file. M9 audit fix: on failure, return a
		// rejecting callback instead of silently downgrading to
		// InsecureIgnoreHostKey. The operator explicitly asked for
		// verification by providing --known-hosts; silently ignoring
		// that defeats the purpose and hides misconfiguration.
		//
		// 尝试加载文件。M9 审计修法：失败时返回拒绝 callback，而非
		// 静默降级到 InsecureIgnoreHostKey。操作员通过 --known-hosts
		// 显式要求校验；静默忽略违背意图并隐藏配置错误。
		if cb, err := loadKnownHostsCallback(*p); err == nil {
			return cb
		} else {
			// stderr write; not in hot path (called once at scan start).
			// stderr 写出；不在热路径（扫描启动时调一次）。
			_, _ = os.Stderr.WriteString(
				"[!] ssh: known_hosts load failed; rejecting all host keys to honor --known-hosts intent: " + err.Error() + "\n",
			)
			return func(_ string, _ net.Addr, _ ssh.PublicKey) error {
				return fmt.Errorf("ssh: known_hosts load failed; refusing to accept any host key (fix the --known-hosts file or remove the flag)")
			}
		}
	}
	if InsecureSSH.Load() {
		return ssh.InsecureIgnoreHostKey() //nolint:gosec // operator-opt-in
	}
	// v0.2-compatible default: insecure, with a one-line stderr
	// warning so an attentive operator sees it. Future versions
	// will flip this to refuse-by-default.
	//
	// v0.2 兼容默认：不安全，附 stderr 单行警告让留心操作员能看到。
	// 未来版本会把默认翻成"默认拒绝"。
	_, _ = os.Stderr.WriteString(
		"[!] ssh: no --insecure-ssh flag and no known_hosts file; accepting any host key (MITM possible). See --insecure-ssh / -o KnownHostsFile=<path>.\n",
	)
	return ssh.InsecureIgnoreHostKey() //nolint:gosec // v0.2 default; see warning above
}
