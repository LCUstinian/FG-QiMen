// Package protocols: Rsync authenticator.
//
// Strategy: rsync protocol (RFC-like, no official RFC). Server
// greeting: "@RSYNCD: <ver>\n". Server asks "USERNAME\n" or
// "PASSWORD\n" (depends on auth method). We send the username, the
// server replies with 16-byte challenge, we compute
// MD5(challenge || password) XOR'd with itself 16 times (a
// simplified version of rsync's older MD4 challenge-response; v0.1
// uses MD5 for compatibility with modern rsync). Server replies
// "@RSYNCD: OK\n" on hit, "@ERROR" on miss.
//
// HARD RULE: on a hit we return. We do NOT run any rsync command
// (no "rsync user@host::module" pull).
//
// 包 protocols：Rsync 认证器。
// 策略：rsync 协议（类 RFC，无正式 RFC）。服务器 greeting：
// "@RSYNCD: <ver>\n"。服务器问 "USERNAME\n" 或 "PASSWORD\n"
// （取决于 auth 方法）。我们发 username，服务器回 16 字节 challenge，
// 我们算 MD5(challenge || password) 与自身 XOR 16 次（rsync 老版
// MD4 challenge-response 的简化版；v0.1 用 MD5 以兼容现代 rsync）。
// 服务器命中回 "@RSYNCD: OK\n"，miss 回 "@ERROR"。
//
// 硬性原则：命中即返回。不跑任何 rsync 命令（不 "rsync
// user@host::module" 拉取）。
package filestorage

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// RsyncAuthenticator authenticates against rsync daemons via
// USERNAME + challenge-response (MD5). / RsyncAuthenticator 通过
// USERNAME + challenge-response (MD5) 对 rsync 守护进程认证。
//
// DefaultPorts returns 873 (standard rsync) / 8873 (alternate).
// / DefaultPorts 返 873（标准 rsync）/ 8873（备用）。
type RsyncAuthenticator struct{}

// NewRsyncAuthenticator returns a default rsync authenticator.
// NewRsyncAuthenticator 返回默认配置的 rsync 认证器。
func NewRsyncAuthenticator() *RsyncAuthenticator { return &RsyncAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *RsyncAuthenticator) Name() string { return "rsync" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *RsyncAuthenticator) DefaultPorts() []int {
	return []int{873, 8873}
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *RsyncAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
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

// attempt runs one rsync USERNAME + challenge-response attempt.
// / attempt 跑一次 rsync USERNAME + challenge-response 试连。
func (a *RsyncAuthenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	// Read server greeting "@RSYNCD: <ver>\n". / 读服务器 greeting。
	greet, err := br.ReadString('\n')
	if err != nil {
		return false, nil
	}
	if !strings.HasPrefix(greet, "@RSYNCD:") {
		return false, nil
	}
	// Send client greeting "@RSYNCD: 31.0\n". / 发客户端 greeting。
	if _, err := bw.WriteString("@RSYNCD: 31.0\n"); err != nil {
		return false, err
	}
	if err := bw.Flush(); err != nil {
		return false, err
	}
	// Server asks "USERNAME\n" or "PASSWORD\n". / 服务器问
	// "USERNAME\n" 或 "PASSWORD\n"。
	q, err := br.ReadString('\n')
	if err != nil {
		return false, nil
	}
	q = strings.TrimRight(q, "\r\n")
	// Some servers skip USERNAME and go straight to PASSWORD; handle
	// both. / 一些服务器跳过 USERNAME 直接 PASSWORD；两种都处理。
	if q == "USERNAME" || q == "USERNAME\n" {
		if _, err := bw.WriteString(user + "\n"); err != nil {
			return false, err
		}
		if err := bw.Flush(); err != nil {
			return false, err
		}
		// Server now sends the challenge (16 bytes, no newline).
		// / 服务器发 challenge（16 字节，无换行）。
		challenge := make([]byte, 16)
		if _, err := readFullRS(conn, challenge); err != nil {
			return false, err
		}
		// Compute response and send. / 算 response 并发。
		response := rsyncHash(pass, challenge)
		if _, err := conn.Write(response); err != nil {
			return false, err
		}
	} else if q == "PASSWORD" || q == "PASSWORD\n" {
		// Server sent 16-byte challenge already. / 服务器已发 16 字节
		// challenge。
		// Actually this case is rare; many servers do USERNAME first.
		// If they go straight to PASSWORD, the format is the same:
		// 16 bytes after the "PASSWORD\n" line. / 实际这种情况少；多数
		// 服务器先 USERNAME。如果直接 PASSWORD，格式一样： "PASSWORD\n"
		// 之后 16 字节。
		challenge := make([]byte, 16)
		if _, err := readFullRS(conn, challenge); err != nil {
			return false, err
		}
		response := rsyncHash(pass, challenge)
		if _, err := conn.Write(response); err != nil {
			return false, err
		}
	} else {
		return false, nil
	}
	// Server reply: "@RSYNCD: OK\n" on hit, "@ERROR" on miss.
	// / 服务器响应：命中 "@RSYNCD: OK\n"，miss "@ERROR"。
	reply, err := br.ReadString('\n')
	if err != nil {
		return false, nil
	}
	return strings.HasPrefix(strings.TrimRight(reply, "\r\n"), "@RSYNCD: OK"), nil
}

// rsyncHash computes the rsync MD5 challenge-response. / rsyncHash
// 算 rsync MD5 challenge-response。
//
// Per the legacy rsync protocol: out[i] = sum[i] ^ sum[i-1] (with
// sum[-1] = 0). So out is a rolling XOR of the MD5 sum. / 按老版
// rsync 协议：out[i] = sum[i] ^ sum[i-1]（sum[-1] = 0）。所以 out 是
// MD5 摘要的滚动 XOR。
func rsyncHash(pass string, challenge []byte) []byte {
	h := md5.New()
	h.Write(challenge)
	h.Write([]byte(pass))
	sum := h.Sum(nil)
	// rsync reads MD5 as 4 little-endian uint32 then writes as
	// big-endian. / rsync 把 MD5 当 4 个小端 uint32 读再大端写。
	swapped := make([]byte, 16)
	for i := 0; i < 4; i++ {
		v := binary.LittleEndian.Uint32(sum[i*4 : i*4+4])
		for j := 0; j < 4; j++ {
			swapped[i*4+j] = byte(v >> (24 - j*8))
		}
	}
	// Rolling XOR: out[i] = swapped[i] ^ swapped[i-1], with
	// swapped[-1] = 0. / 滚动 XOR：out[i] = swapped[i] ^ swapped[i-1]，
	// swapped[-1] = 0。
	out := make([]byte, 16)
	out[0] = swapped[0]
	for i := 1; i < 16; i++ {
		out[i] = swapped[i] ^ swapped[i-1]
	}
	return out
}

func readFullRS(c net.Conn, buf []byte) (int, error) {
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

// init registers the rsync authenticator. / init 注册 rsync 认证器。
func init() {
	credential.Register(NewRsyncAuthenticator())
}

// Keep fmt import alive. / fmt 保留。
var _ = fmt.Sprintf
