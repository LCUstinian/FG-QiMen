// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// NFS Identify plugin. Sends an RPC NULL call and waits for any
// reply. A reply = ONC RPC server = NFS endpoint (since NFS runs
// over RPC). / NFS 识别插件。发 RPC NULL 调用并等任何响应。响应 =
// ONC RPC server = NFS 端点（NFS 跑在 RPC 上）。
package nfs

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies NFS / ONC RPC servers. / Plugin 识别 NFS / ONC RPC
// 服务器。
type Plugin struct{}

// New returns a new nfs plugin. / New 返回一个新的 nfs 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "nfs" }

// Ports returns default NFS port. / Ports 返回默认 NFS 端口。
func (p *Plugin) Ports() []int { return []int{2049} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends RPC NULL call. / Identify 发 RPC NULL 调用。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// RPC NULL call (program 0, version 0, procedure 0). /
	// RPC NULL 调用（program 0，version 0，procedure 0）。
	body := make([]byte, 0, 48)
	fragHdr := make([]byte, 4)
	body = append(body, fragHdr...)
	var xid [4]byte
	binary.BigEndian.PutUint32(xid[:], 0x12345678)
	body = append(body, xid[:]...)
	body = append(body, 0, 0, 0, 0) // msg type = CALL
	body = append(body, 0, 0, 0, 2) // RPC version
	body = append(body, 0, 0, 0, 0) // program = NULL
	body = append(body, 0, 0, 0, 0) // program version
	body = append(body, 0, 0, 0, 0) // procedure = NULL
	body = append(body, 0, 0, 0, 0, 0, 0, 0, 0) // AUTH_NULL cred
	body = append(body, 0, 0, 0, 0, 0, 0, 0, 0) // AUTH_NULL verifier
	fragLen := uint32(len(body)) | 0x80000000
	binary.BigEndian.PutUint32(body[0:4], fragLen)
	if _, err := conn.Write(body); err != nil {
		return nil
	}
	buf := make([]byte, 64)
	if _, err := readFullNFSP(conn, buf); err != nil {
		return nil
	}
	if len(buf) < 12 || buf[11] != 1 {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "nfs",
		Banner: "NFS (ONC RPC)", Time: time.Now(),
	}
}

func readFullNFSP(c net.Conn, buf []byte) (int, error) {
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
