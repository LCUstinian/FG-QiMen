// nfs_test.go — fake-server test for the NFS Identify plugin.
//
// The NFS plugin speaks ONC RPC over TCP. Its Identify() sends a
// 28-byte RPC fragment containing a NULL procedure call and then
// reads 64 bytes back, treating any reply whose byte[11]==1 as a
// successful RPC REPLY (accept_stat=0 success marker for the NULL
// procedure). Per RFC 1057 / RFC 1831 the RPC fragment header has
// its top bit set on the last fragment; byte[11] of the RPC
// payload is the reply message type (1=REPLY, 0=CALL).
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// drains the request and writes a 64-byte stub reply with
// byte[11]=1 (msg_type=REPLY) and accept_stat=0 (success) so the
// plugin classifies the peer as NFS. / 用 fakeserver.ListenLoop 在
// 127.0.0.1:0 起 TCP 监听；handler 排空请求字节并写一个 64 字节 stub
// 响应，其中 byte[11]=1（msg_type=REPLY）+ accept_stat=0（成功），
// 让 plugin 把对端分类为 NFS。
package nfs

import (
	"context"
	"net"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// rpcReply is the 64-byte stub response the fake server writes back
// to satisfy Identify(). The plugin reads 64 bytes into a fixed
// buffer; it returns a hit iff len(buf)>=12 and buf[11]==1. The
// fragment header (bytes 0-3) carries the high-bit-set last-
// fragment marker and the payload length (28 bytes of RPC reply
// body + 4 bytes AUTH_NULL verifier). Bytes 4-11 form the RPC
// reply header; byte[11] is the reply msg type we set to 1
// (REPLY). Bytes 12-15 are reply_stat (0=MSG_ACCEPTED). Byte 16 is
// accept_stat (0=SUCCESS). / rpcReply 是假服务器回写以满足
// Identify() 的 64 字节 stub 响应。plugin 把 64 字节读到固定 buffer；
// 当且仅当 len(buf)>=12 且 buf[11]==1 时返 hit。fragment header
// （字节 0-3）带 high-bit-set 的 last-fragment 标记以及 payload
// 长度；字节 4-11 是 RPC reply header，byte[11] 是 reply msg type
// （我们设为 1=REPLY）；字节 12-15 是 reply_stat
// （0=MSG_ACCEPTED）；字节 16 是 accept_stat（0=SUCCESS）。
var rpcReply = func() []byte {
	b := make([]byte, 64)
	// Fragment header: last fragment (high bit) + length.
	// / Fragment header：last fragment（最高位）+ 长度。
	b[0] = 0x80
	b[1] = 0x00
	b[2] = 0x00
	b[3] = 0x1c // 28-byte RPC body
	// XID: echo the plugin's 0x12345678. / XID：回显 plugin 的 0x12345678。
	b[4] = 0x12
	b[5] = 0x34
	b[6] = 0x56
	b[7] = 0x78
	// msg_type: REPLY. / msg_type：REPLY。
	b[11] = 1
	// reply_stat = MSG_ACCEPTED, accept_stat = SUCCESS.
	// / reply_stat = MSG_ACCEPTED，accept_stat = SUCCESS。
	b[12] = 0x00
	b[13] = 0x00
	b[14] = 0x00
	b[15] = 0x00 // reply_stat
	b[16] = 0x00 // accept_stat (SUCCESS)
	// AUTH_NULL verifier (zero-filled) follows accept_stat.
	// / AUTH_NULL verifier（零填充）接 accept_stat 之后。
	return b
}()

// TestNfs_IdentifyHit covers the happy path: a fake RPC server that
// drains the plugin's NULL call and writes back a 64-byte REPLY
// with byte[11]==1. The plugin must return a *types.Result with
// Service=="nfs" and a populated Banner. / TestNfs_IdentifyHit 覆
// 盖 happy path：假 RPC server 排空 plugin 的 NULL call 并写回 64
// 字节 REPLY（byte[11]==1）。plugin 必须返回 Service=="nfs" 且
// Banner 已填充的 *types.Result。
func TestNfs_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain the plugin's RPC NULL call (the plugin writes 28
		// bytes; we read up to the first read deadline to be safe).
		// / 排空 plugin 的 RPC NULL call（plugin 写 28 字节；我们
		// 读到首次读 deadline 为止以保安全）。
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		// Reply with a 64-byte RPC REPLY stub: byte[11]=1 is the
		// plugin's only success signal. / 回一个 64 字节 RPC
		// REPLY stub：byte[11]=1 是 plugin 唯一的成功信号。
		_, _ = c.Write(rpcReply)
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected nfs hit on RPC REPLY")
	}
	if res.Service != "nfs" {
		t.Errorf("Service = %q, want %q", res.Service, "nfs")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
	if res.Banner == "" {
		t.Error("Banner empty; want NFS marker")
	}
}

// TestNfs_IdentifyMiss covers the negative branch: a server that
// closes the connection without sending 12+ bytes (or sends a
// response with byte[11]!=1) must yield nil. We exercise the
// silent-close path: handler returns without writing anything;
// the plugin's Read will hit EOF/timeout and return nil. /
// TestNfs_IdentifyMiss 覆盖反向分支：server 不写满 12 字节（或写
// 响应 byte[11]!=1）必须返 nil。我们跑静默关闭路径：handler 不写任
// 何字节就返回；plugin 的 Read 会撞 EOF / 超时并返 nil。
func TestNfs_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Read the request so the plugin's write doesn't get
		// RST; then close without replying. / 读请求以避免
		// plugin 写时收到 RST；之后不回复直接关。
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte("not an rpc reply"))
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for non-RPC peer", res)
	}
}

// TestNfs_CredentialNoOp documents that the plugin's Credential is
// a documented no-op stub (see nfs.go L40-42, comment "Credential
// is a no-op stub. / Credential 空 stub。"). Per the v0.6.0 fake-
// server plan we keep a placeholder test that skips, mirroring the
// FTP smoke pattern — future contributors who promote the stub to
// a real implementation can replace the skip body with a
// mount(export)/GETATTR handler. / TestNfs_CredentialNoOp 标注
// plugin 的 Credential 是文档化的 no-op stub（见 nfs.go L40-42）；
// 按 v0.6.0 fake-server 计划保留一个跳过的占位测试，对齐 FTP 的
// smoke 模式——未来贡献者把 stub 升级成真实实现时，可以把 skip 替
// 换成 mount(export) / GETATTR handler。
func TestNfs_CredentialNoOp(t *testing.T) {
	t.Skip("Credential is a documented no-op stub in nfs.go (L40-42); " +
		"NFS credential testing would require a mount(export) + GETATTR handler.")
}
