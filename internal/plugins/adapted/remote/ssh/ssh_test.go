// ssh_test.go — fake-server tests for the ssh Identify plugin.
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// speaks first on connect with an "SSH-2.0-<software> <comments>\r\n"
// banner (RFC 4253 §4.2), and the plugin's Identify reads one line,
// validates the "SSH-" prefix, sends back its own minimal client
// banner, parses the software version, classifies it, and returns
// a Result.
//
// / ssh_test.go — ssh Identify 插件的 fake-server 测试。
// fakeserver.ListenLoop 在 127.0.0.1:0 起 TCP 监听；handler 在
// connect 后先发 "SSH-2.0-<software> <comments>\r\n" banner
// （RFC 4253 §4.2）；plugin Identify 读一行，校验 "SSH-" 前缀，
// 回发自己的最小 client banner，解析软件版本，分类，返回 Result。
//
// This is a stateful TCP plugin (Tier 3 in the v0.6.0 fake-server
// plan). Per RFC 4253 the SSH server speaks first on connect; the
// plugin only needs the banner line — the deeper key-exchange /
// user-auth flow is not exercised by Identify (and Credential on
// this plugin is a documented no-op stub — see ssh.go:48-53 and
// the TestSsh_CredentialHit skip below).
//
// / 这是一个 stateful TCP 插件（v0.6.0 fake-server 计划 Tier 3）。
// 按 RFC 4253，SSH server 在 connect 后先说话；plugin 只需要 banner
// 这一行——深层 key-exchange / user-auth 流程不在 Identify 范围（且
// 此插件的 Credential 是文档化的 no-op stub——见 ssh.go:48-53 和
// 下面的 TestSsh_CredentialHit skip）。
package ssh

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// sshBanner is a typical RFC 4253 §4.2 SSH server banner. The
// plugin reads one line terminated by '\n', trims trailing
// "\r\n", and parses the "SSH-protoversion-softwareversion
// [comments]" prefix.
//
// / sshBanner 是 RFC 4253 §4.2 风格的 SSH server banner。plugin 读
// 到一个以 '\n' 结尾的行，剥掉尾部的 "\r\n"，然后解析
// "SSH-protoversion-softwareversion [comments]" 前缀。
const sshBanner = "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1\r\n"

// TestSsh_IdentifyHit covers the happy path: server speaks first
// with a valid SSH-2.0 banner; plugin reads one line, validates
// the "SSH-" prefix, sends its own client banner back (which the
// fake server discards), parses "2.0-OpenSSH_8.9p1 …" into
// proto="2.0" / software="OpenSSH_8.9p1 Ubuntu-3ubuntu0.1",
// classifies as "OpenSSH", and returns a Result with
// Service="ssh" and a non-empty Banner.
//
// / TestSsh_IdentifyHit 覆盖 happy path：server 先发合法 SSH-2.0
// banner；plugin 读一行，校验 "SSH-" 前缀，回发自己的 client banner
// （假服务器丢弃），把 "2.0-OpenSSH_8.9p1 …" 解析成 proto="2.0" /
// software="OpenSSH_8.9p1 Ubuntu-3ubuntu0.1"，分类为 "OpenSSH"，
// 返回 Service="ssh"、Banner 非空的 Result。
func TestSsh_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Per RFC 4253 §4.2 the server speaks the version string
		// first on connect. / 按 RFC 4253 §4.2，server 在 connect
		// 后先发版本字符串。
		_, _ = c.Write([]byte(sshBanner))

		// The plugin writes its own banner ("SSH-2.0-FG-QiMen_0.3.1\r\n")
		// back. Drain it so the kernel buffers stay clean and the
		// goroutine can exit cleanly. / plugin 会回发自己的 banner
		// （"SSH-2.0-FG-QiMen_0.3.1\r\n"）。把它读走，避免内核
		// 缓冲区堆积，让 goroutine 干净退出。
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')

		// Hold the conn open briefly so the plugin's read on the
		// server side has time to complete before the test
		// finishes. / 短时保持 conn 打开，让 plugin 端的读有
		// 时间完成再让测试结束。
		time.Sleep(50 * time.Millisecond)
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected ssh hit")
	}
	if res.Service != "ssh" {
		t.Errorf("Service = %q, want %q", res.Service, "ssh")
	}
	if res.Host != host {
		t.Errorf("Host = %q, want %q", res.Host, host)
	}
	if res.Port != port {
		t.Errorf("Port = %d, want %d", res.Port, port)
	}
	if res.Banner == "" {
		t.Error("Banner is empty")
	}
	// OpenSSH classification: banner should contain the
	// "OpenSSH" tag. / OpenSSH 分类：banner 应含 "OpenSSH" 标
	// 签。
	if !strings.Contains(res.Banner, "OpenSSH") {
		t.Errorf("Banner = %q, want it to contain %q", res.Banner, "OpenSSH")
	}
}

// TestSsh_IdentifyHit_Dropbear exercises the classify() branch
// for "dropbear" — a different SSH implementation tag. The
// banner format / wire format is identical to OpenSSH; only the
// software string differs.
//
// / TestSsh_IdentifyHit_Dropbear 验证 classify() 对 "dropbear" 的
// 分支——另一个 SSH 实现标签。banner 格式 / wire 格式与 OpenSSH
// 相同，只有 software 串不同。
func TestSsh_IdentifyHit_Dropbear(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("SSH-2.0-dropbear_2020.81\r\n"))
		// Drain the plugin's reply banner. / 读走 plugin 的回
		// 复 banner。
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		time.Sleep(50 * time.Millisecond)
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected ssh hit for dropbear")
	}
	if res.Service != "ssh" {
		t.Errorf("Service = %q, want %q", res.Service, "ssh")
	}
	if !strings.Contains(res.Banner, "Dropbear") {
		t.Errorf("Banner = %q, want it to contain %q", res.Banner, "Dropbear")
	}
}

// TestSsh_IdentifyHit_Generic covers the fallback classify()
// branch: when software doesn't match any known prefix / substring
// (OpenSSH / dropbear / libssh / paramiko / putty / winscp / cisco
// / ruijie / h3c / huawei), classify returns "SSH" and fmtSSH
// produces "SSH-2.0 <software>".
//
// / TestSsh_IdentifyHit_Generic 验证 classify() 的兜底分支：software
// 不匹配任何已知前缀 / 子串时，classify 返 "SSH"，fmtSSH 输出
// "SSH-2.0 <software>"。
func TestSsh_IdentifyHit_Generic(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("SSH-2.0-FakeSSH_1.0\r\n"))
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		time.Sleep(50 * time.Millisecond)
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected ssh hit for generic SSH")
	}
	if res.Service != "ssh" {
		t.Errorf("Service = %q, want %q", res.Service, "ssh")
	}
	// For the generic branch, fmtSSH returns
	// "SSH-<proto> <software>" (no parenthetical tag). / 兜底分
	// 支，fmtSSH 返 "SSH-<proto> <software>"（无括号标签）。
	if !strings.HasPrefix(res.Banner, "SSH-2.0 ") {
		t.Errorf("Banner = %q, want it to start with %q", res.Banner, "SSH-2.0 ")
	}
}

// TestSsh_IdentifyMiss covers the negative branch: server
// greets with something that doesn't start with "SSH-". The
// plugin's ReadString('\n') returns the line, but the prefix
// check fails and Identify returns nil.
//
// / TestSsh_IdentifyMiss 覆盖反向分支：server greeting 不以 "SSH-"
// 开头。plugin 的 ReadString('\n') 返该行，但前缀校验失败，
// Identify 返 nil。
func TestSsh_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// "RFB 003.008\n" is the VNC banner; plugin should
		// reject it because it lacks the "SSH-" prefix. /
		// "RFB 003.008\n" 是 VNC banner；plugin 应拒绝（缺
		// "SSH-" 前缀）。
		_, _ = c.Write([]byte("RFB 003.008\n"))
		// Hold the conn open briefly so the plugin's Read has
		// time to return before the test exits. / 短时保持
		// conn 打开，让 plugin 的 Read 有时间返回。
		time.Sleep(50 * time.Millisecond)
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for non-SSH greeting", res)
	}
}

// TestSsh_IdentifyReadError covers the bufio.ReadString('\n')
// error branch: server accepts the connection but closes it
// without sending any bytes. The plugin's ReadString returns an
// error (io.EOF or similar) and Identify returns nil.
//
// / TestSsh_IdentifyReadError 覆盖 bufio.ReadString('\n') 的错误分
// 支：server accept 连接后不发任何字节就关闭。plugin 的
// ReadString 返错误（io.EOF 等），Identify 返 nil。
func TestSsh_IdentifyReadError(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Close immediately without writing. The plugin's
		// ReadString('\n') will return an error. / 立刻关
		// 闭不写任何东西。plugin 的 ReadString('\n') 会返
		// 错误。
		_ = c.Close()
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil on read error", res)
	}
}

// TestSsh_IdentifyHit_PuTTY exercises another classify() branch
// (vendor substring match: "putty") to broaden coverage of the
// switch. Banner format is unchanged from the OpenSSH case.
//
// / TestSsh_IdentifyHit_PuTTY 验证 classify() 的另一个分支（厂商子
// 串匹配："putty"），扩展 switch 的覆盖率。banner 格式与 OpenSSH
// 一致。
func TestSsh_IdentifyHit_PuTTY(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("SSH-2.0-PuTTY_Release_0.78\r\n"))
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		time.Sleep(50 * time.Millisecond)
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected ssh hit for PuTTY")
	}
	if res.Service != "ssh" {
		t.Errorf("Service = %q, want %q", res.Service, "ssh")
	}
	if !strings.Contains(res.Banner, "PuTTY") {
		t.Errorf("Banner = %q, want it to contain %q", res.Banner, "PuTTY")
	}
}

// TestSsh_CredentialHit is skipped: ssh.Credential is a
// documented no-op stub (see ssh.go:48-53). The real SSH
// credential flow lives in
// internal/core/credential/protocols/ssh.go (via the golang.org/x/crypto/ssh
// client — completes the SSH_MSG_USERAUTH_REQUEST with
// "password" method, reads SSH_MSG_USERAUTH_SUCCESS). /
// TestSsh_CredentialHit 跳过：ssh.Credential 是文档化的 no-op
// stub（见 ssh.go:48-53）。真正的 SSH 凭据流程在
// internal/core/credential/protocols/ssh.go（用
// golang.org/x/crypto/ssh 客户端——完成 "password" 方法的
// SSH_MSG_USERAUTH_REQUEST，读取 SSH_MSG_USERAUTH_SUCCESS）。
func TestSsh_CredentialHit(t *testing.T) {
	t.Skip("ssh.Credential is a no-op stub; see ssh.go Credential()")
}
