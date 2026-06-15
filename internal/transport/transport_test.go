// transport_test.go — pin the process-wide gate semantics so a
// future refactor can't accidentally re-enable InsecureSkipVerify
// (P1#3) or silently re-accept any SSH host key (P1#4) on the
// default-verify path.
//
// We exercise:
//   - TLSConfig(false) returns Verify ON by default
//   - TLSConfig(true) returns Verify OFF (override path)
//   - InsecureTLS flag toggles the default
//   - SSHHostKeyCallback: known_hosts wins when file loads, falls
//     back to InsecureSSH flag, then to insecure+warning
//   - KnownHostsFile with a missing path: stderr warning, fall back
//     to the next branch (no panic)
package transport

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// resetGates sets all process-wide flags to known-clean state.
// Must be called at the start of every test that mutates a flag
// because the flags are global.
//
// resetGates 把所有进程级 flag 置回已知干净状态。修改 flag 的每个测试
// 都必须先调它，因为 flag 是全局的。
func resetGates(t *testing.T) {
	t.Helper()
	InsecureTLS.Store(false)
	InsecureSSH.Store(false)
	KnownHostsFile.Store(nil)
}

// TestTLSConfigDefaultsToVerifying — the P1#3 contract. The
// no-override path must return a *tls.Config with InsecureSkipVerify
// false. A future contributor who flips the default (e.g. for
// "convenience") breaks the security guarantee.
func TestTLSConfigDefaultsToVerifying(t *testing.T) {
	resetGates(t)
	cfg := TLSConfig(false)
	if cfg.InsecureSkipVerify {
		t.Error("TLSConfig(false) with InsecureTLS=false: InsecureSkipVerify = true; want false (P1#3)")
	}
}

// TestTLSConfigOverrideForcesInsecure — the override path is for
// one-off unit tests that need a self-signed test server. It must
// always produce InsecureSkipVerify=true regardless of the flag.
func TestTLSConfigOverrideForcesInsecure(t *testing.T) {
	resetGates(t)
	cfg := TLSConfig(true)
	if !cfg.InsecureSkipVerify {
		t.Error("TLSConfig(true) with InsecureTLS=false: InsecureSkipVerify = false; want true (override)")
	}
}

// TestTLSConfigFlagTogglesDefault — when the operator passes
// --insecure-tls, the no-override path must respect it.
func TestTLSConfigFlagTogglesDefault(t *testing.T) {
	resetGates(t)
	InsecureTLS.Store(true)
	cfg := TLSConfig(false)
	if !cfg.InsecureSkipVerify {
		t.Error("TLSConfig(false) with InsecureTLS=true: InsecureSkipVerify = false; want true (flag respected)")
	}
}

// TestSSHCallbackFallsBackToFlag — when no known_hosts file is set
// but --insecure-ssh is, the callback should accept any key (no
// error from the callback itself). The audit's P1#4 fix.
//
// We don't actually complete an SSH handshake here — we just check
// the callback's return value against a generated public key.
func TestSSHCallbackFallsBackToFlag(t *testing.T) {
	resetGates(t)
	InsecureSSH.Store(true)
	cb := SSHHostKeyCallback()

	// Build a throwaway host key. The content doesn't matter — the
	// insecure callback accepts anything.
	//
	// 构造一个临时主机密钥。内容不重要——insecure callback 接受任何
	// 东西。
	k := mustHostKey(t)
	if err := cb("example.com", &net.TCPAddr{}, k); err != nil {
		t.Errorf("InsecureSSH=true: callback rejected key: %v", err)
	}
}

// TestSSHCallbackV0_2CompatDefault — when neither known_hosts nor
// the flag are set, the callback must (a) still accept the key for
// backward compat and (b) write a warning to stderr so an attentive
// operator sees it.
//
// v0.2 kept the insecure default; v0.3+ plan is to flip to
// "refuse by default". The v0.2 behavior must be preserved in this
// test (regression guard for the v0.2 → v0.3 transition).
func TestSSHCallbackV0_2CompatDefault(t *testing.T) {
	resetGates(t)

	// Capture stderr. / 捕获 stderr。
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	cb := SSHHostKeyCallback()
	k := mustHostKey(t)
	if err := cb("example.com", &net.TCPAddr{}, k); err != nil {
		t.Errorf("v0.2 default: callback rejected key: %v", err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "ssh:") {
		t.Errorf("v0.2 default: expected stderr warning containing 'ssh:'; got %q", buf.String())
	}
}

// TestSSHCallbackMissingKnownHostsFallsBack — operator passes
// --known-hosts=/path/that/does/not/exist. Must NOT panic; should
// fall through to the next branch (insecure default) and emit a
// stderr note. The operator's scan should still be able to proceed
// rather than crashing on startup.
func TestSSHCallbackMissingKnownHostsFallsBack(t *testing.T) {
	resetGates(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	missing := filepath.Join(t.TempDir(), "definitely-not-here")
	KnownHostsFile.Store(&missing)
	cb := SSHHostKeyCallback()
	k := mustHostKey(t)
	// Should still work (v0.2 default after fallback).
	//
	// 应该仍能工作（回退后是 v0.2 默认）。
	if err := cb("example.com", &net.TCPAddr{}, k); err != nil {
		t.Errorf("missing known_hosts: callback rejected key after fallback: %v", err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "known_hosts load failed") {
		t.Errorf("missing known_hosts: expected stderr warning; got %q", buf.String())
	}
}

// TestSSHCallbackValidKnownHostsRejectsUnknown — happy path: a real
// known_hosts file with one key; presenting a DIFFERENT key must
// produce a callback error. P1#4's real verification contract.
func TestSSHCallbackValidKnownHostsRejectsUnknown(t *testing.T) {
	resetGates(t)

	// Build a known_hosts file with the wire form of key A.
	// / 构造一个 known_hosts 文件，含 key A 的 wire 形态。
	keyA := mustHostKey(t)
	keyAEntry := formatKnownHostsEntry("example.com", keyA)
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(keyAEntry), 0o600); err != nil {
		t.Fatal(err)
	}
	KnownHostsFile.Store(&path)

	cb := SSHHostKeyCallback()
	keyB := mustHostKey(t) // different from keyA
	if err := cb("example.com", &net.TCPAddr{}, keyB); err == nil {
		t.Error("known_hosts path: unknown key accepted; want rejection")
	}
	if err := cb("example.com", &net.TCPAddr{}, keyA); err != nil {
		t.Errorf("known_hosts path: known key rejected: %v", err)
	}
}

// TestFlagsConcurrentSafe — process-wide atomic flags can be set
// and read from many goroutines without panic. We don't assert
// ordering (the gates are read at scan start, not concurrently);
// we just assert the race detector stays clean.
//
// 测试进程级 atomic flag 在多 goroutine 下可并发读写。运行 `go test
// -race` 时这是免费的；不写也跑 -race 也覆盖。
func TestFlagsConcurrentSafe(t *testing.T) {
	resetGates(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); InsecureTLS.Store(true) }()
		go func() { defer wg.Done(); InsecureTLS.Store(false) }()
	}
	wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────

// mustHostKey generates a throwaway RSA host key for testing. Two
// calls return two distinct keys (high probability; crypto/rand
// seeded by the OS).
//
// mustHostKey 生成一个临时 RSA 主机密钥用于测试。两次调用返回两个不
// 同的 key（高概率；crypto/rand 由 OS 播种）。
func mustHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	// We need an *ssh.Signer to derive a PublicKey. Use the
	// NewSignerFromKey helper.
	//
	// 我们需要 *ssh.Signer 来派生 PublicKey。用 NewSignerFromKey
	// helper。
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

// formatKnownHostsEntry renders a known_hosts line in the
// "pattern keytype base64key" form. We delegate to ssh's own
// MarshalAuthorizedKey, which produces the canonical OpenSSH wire
// form (which is also what known_hosts expects on the right-hand
// side). The pattern is prepended manually.
//
// formatKnownHostsEntry 以 "pattern keytype base64key" 形式渲染一
// 行 known_hosts。我们直接用 ssh 的 MarshalAuthorizedKey，它输出
// 标准 OpenSSH wire 形态（也是 known_hosts 右侧要求的形态）。pattern
// 是手动加上的。
func formatKnownHostsEntry(pattern string, key ssh.PublicKey) string {
	authKey := ssh.MarshalAuthorizedKey(key) // ends with \n
	// "ssh-rsa AAAA...\n" → "<pattern> ssh-rsa AAAA...\n"
	// Find the first space (separator after key type).
	sp := bytes.IndexByte(authKey, ' ')
	if sp < 0 {
		return string(authKey)
	}
	return pattern + " " + string(authKey[:sp+1]) + string(authKey[sp+1:])
}

// init silence linter for unused imports (we use net in the
// callback signature; pem / x509 are defensive for older Go
// versions that required them).
//
// init 静默未用 import 的 lint 警告（callback 签名里用了 net；
// pem / x509 留作老 Go 版本的防御）。
func init() {
	_ = pem.Encode
	_ = x509.MarshalPKCS1PublicKey
	_ = net.IPv4
}
