// Package protocols: NFS authenticator.
//
// Strategy: send an RPC NULL call (program 0, version 0, procedure
// 0 — used for "ping" in ONC RPC). A response = NFS server
// reachable. v0.1 doesn't negotiate AUTH_UNIX / AUTH_GSS / etc. —
// we just confirm the port speaks RPC.
//
// HARD RULE: on a hit we return. We do NOT issue any NFS
// operation (no MOUNT, no LOOKUP, no READDIR, no READ).
//
// 包 protocols：NFS 认证器。
// 策略：发 RPC NULL 调用（program 0，version 0，procedure 0——ONC RPC
// 的"ping"）。响应 = NFS 服务器可达。v0.1 不协商 AUTH_UNIX /
// AUTH_GSS 等——只确认端口说 RPC。
//
// 硬性原则：命中即返回。不发任何 NFS 操作（不 MOUNT、不 LOOKUP、不
// READDIR、不 READ）。
package filestorage

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// NFSAuthenticator authenticates against NFS via RPC NULL call.
// / NFSAuthenticator 通过 RPC NULL 调用对 NFS 认证。
//
// DefaultPort returns 2049 (standard NFS). / DefaultPort 返 2049（标准
// NFS）。
type NFSAuthenticator struct{}

// NewNFSAuthenticator returns a default NFS authenticator.
// NewNFSAuthenticator 返回默认配置的 NFS 认证器。
func NewNFSAuthenticator() *NFSAuthenticator { return &NFSAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *NFSAuthenticator) Name() string { return "nfs" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *NFSAuthenticator) DefaultPorts() []int {
	return []int{2049}
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *NFSAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, timeout)
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

// attempt sends an RPC NULL call. / attempt 发 RPC NULL 调用。
func (a *NFSAuthenticator) attempt(ctx context.Context, addr string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Build RPC NULL call:
	//   Fragment header (4 bytes BE):
	//     bit 31: 1 = last fragment
	//     bit 30: 0 = request
	//     bits 0-29: fragment length
	//   XID (4 bytes BE): 0x12345678
	//   Message type (4 bytes BE): 0 = CALL
	//   RPC version (4 bytes BE): 2
	//   Program (4 bytes BE): 0 (NULL)
	//   Program version (4 bytes BE): 0
	//   Procedure (4 bytes BE): 0 (NULL)
	//   Credentials: AUTH_NULL flavor (4 bytes BE = 0) + length (4 bytes BE = 0) + body (0 bytes)
	//   Verifier: AUTH_NULL flavor (4 bytes BE = 0) + length (4 bytes BE = 0) + body (0 bytes)
	// / 构造 RPC NULL 调用：
	//   Fragment 头（4 字节 BE）：
	//     bit 31: 1 = 最后一个片段
	//     bit 30: 0 = 请求
	//     bits 0-29: 片段长度
	//   XID (4 字节 BE): 0x12345678
	//   Message type (4 字节 BE): 0 = CALL
	//   RPC version (4 字节 BE): 2
	//   Program (4 字节 BE): 0 (NULL)
	//   Program version (4 字节 BE): 0
	//   Procedure (4 字节 BE): 0 (NULL)
	//   Credentials: AUTH_NULL flavor (4 字节 BE = 0) + length (4 字节 BE = 0) + body (0 字节)
	//   Verifier: AUTH_NULL flavor (4 字节 BE = 0) + length (4 字节 BE = 0) + body (0 字节)
	//
	// Total = 4 (frag hdr) + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 = 48 bytes
	// / 共 4 (frag 头) + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 + 4 = 48 字节
	body := make([]byte, 0, 48)
	// Fragment header (last fragment, request, length). We fill length
	// at the end. / Fragment 头（最后片段、请求、长度）。长度最后填。
	fragHdr := make([]byte, 4)
	body = append(body, fragHdr...)
	// XID. / XID。
	var xid [4]byte
	binary.BigEndian.PutUint32(xid[:], 0x12345678)
	body = append(body, xid[:]...)
	// Message type = CALL. / Message type = CALL。
	body = append(body, 0, 0, 0, 0)
	// RPC version. / RPC version。
	body = append(body, 0, 0, 0, 2)
	// Program = 0 (NULL). / Program = 0 (NULL)。
	body = append(body, 0, 0, 0, 0)
	// Program version = 0. / Program version = 0。
	body = append(body, 0, 0, 0, 0)
	// Procedure = 0 (NULL). / Procedure = 0 (NULL)。
	body = append(body, 0, 0, 0, 0)
	// Credentials: AUTH_NULL (0) + length 0. / Credentials：AUTH_NULL
	// (0) + 长度 0。
	body = append(body, 0, 0, 0, 0, 0, 0, 0, 0)
	// Verifier: AUTH_NULL (0) + length 0. / Verifier：AUTH_NULL
	// (0) + 长度 0。
	body = append(body, 0, 0, 0, 0, 0, 0, 0, 0)
	// Set fragment header: bit 31 = 1 (last), bit 30 = 0 (request),
	// bits 0-29 = length.
	// / 设 fragment 头：bit 31 = 1（最后），bit 30 = 0（请求），
	// bits 0-29 = 长度。
	fragLen := uint32(len(body)) | 0x80000000 // bit 31 set
	binary.BigEndian.PutUint32(body[0:4], fragLen)
	if _, err := conn.Write(body); err != nil {
		return false, err
	}
	// Read response: fragment header (4) + XID (4) + msg type (4)
	// = at least 12 bytes for a valid reply.
	// / 读响应：fragment 头 (4) + XID (4) + msg type (4)
	// = 至少 12 字节。
	buf := make([]byte, 64)
	n, err := readFullNFS(conn, buf)
	if err != nil {
		return false, nil
	}
	if n < 12 {
		return false, nil
	}
	// Reply msg type = 1. / Reply msg type = 1。
	if buf[8] != 0 || buf[9] != 0 || buf[10] != 0 || buf[11] != 1 {
		return false, nil
	}
	return true, nil
}

func readFullNFS(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// init registers the NFS authenticator. / init 注册 NFS 认证器。
func init() {
	credential.Register(NewNFSAuthenticator())
}

// Keep fmt import alive. / fmt 保留。
var _ = fmt.Sprintf
