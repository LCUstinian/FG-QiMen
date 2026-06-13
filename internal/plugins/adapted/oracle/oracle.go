// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Oracle Identify plugin. Sends a TNS Connect packet (per Oracle
// networking spec) and checks the server's response for an
// Accept (type 2) or Refuse (type 4) packet. Accept + auth-pending
// status is a hit. This is a thin probe — full auth flow lives in
// core/cred/protocols/oracle.go (OracleAuthenticator via go-ora).
//
// Oracle 识别插件。发 TNS Connect 包（按 Oracle 网络规范）并检查
// 服务器响应是否 Accept（type 2）或 Refuse（type 4）包。Accept
// + auth-pending 即命中。这是薄探针——完整 auth 流程在 core/cred/
// protocols/oracle.go（OracleAuthenticator via go-ora）。
package oracle

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// TNS Connect packet layout (Oracle networking spec): / TNS Connect
// 包布局（Oracle 网络规范）：
//   2 bytes: packet length (BE, includes these 2 bytes)
//   2 bytes: packet checksum (BE)
//   1 byte:  packet type (1 = Connect)
//   1 byte:  reserved (0)
//   2 bytes: header checksum (BE)
//   1 byte:  version (0x0A = "10g+")
//   1 byte:  version minor
//   1 byte:  service options
//   1 byte:  session data unit size (high)
//   1 byte:  session data unit size (low)
//   1 byte:  max transmission size (high)
//   1 byte:  max transmission size (low)
//   1 byte:  NT protocol characteristics
//   1 byte:  line turnaround
//   1 byte:  value of 1 in Oracle (charset ID high)
//   1 byte:  value of 0x01 (charset ID low)
//   1 byte:  line turnaround
//   1 byte:  connect flags 1
//   2 bytes: connect flags 2 (BE)
//   1 byte:  connect flags 3
//   ... then length-prefixed connect data
// See: https://docs.oracle.com/en/database/oracle/oracle-database/19/netrf/oracle-net-services-protocol.html

const (
	tnsPacketTypeConnect byte = 1
	tnsPacketTypeAccept  byte = 2
	tnsPacketTypeRefuse  byte = 4
)

// Plugin identifies Oracle TNS listeners. / Plugin 识别 Oracle TNS 监听器。
type Plugin struct{}

// New returns a new oracle plugin. / New 返回一个新的 oracle 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "oracle" }

// Ports returns default Oracle TNS ports. / Ports 返回默认 Oracle TNS 端口。
func (p *Plugin) Ports() []int { return []int{1521, 1526, 2483} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends a TNS Connect packet and reads the response type.
// / Identify 发 TNS Connect 包并读响应类型。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Build a minimal TNS Connect packet that asks for service
	// "ORCL" with TCP protocol. / 构造一个最小 TNS Connect 包，
	// 请求 service "ORCL" + TCP 协议。
	pkt := buildTNSConnect("ORCL")
	if _, err := conn.Write(pkt); err != nil {
		return nil
	}
	// Read TNS response header: 8 bytes (length 2 + checksum 2 +
	// type 1 + reserved 1 + header-checksum 2). The Accept packet
	// is at least 12 bytes (8 header + version byte + some more).
	// / 读 TNS 响应头：8 字节（length 2 + checksum 2 + type 1 +
	// reserved 1 + header-checksum 2）。Accept 包至少 12 字节
	// （8 头 + version 字节 + 一些别的）。
	hdr := make([]byte, 8)
	if _, err := readFull(conn, hdr); err != nil {
		return nil
	}
	pktType := hdr[4]
	if pktType == tnsPacketTypeAccept {
		return &common.Result{
			Host: host, Port: port, Service: "oracle",
			Banner: "Oracle TNS (Accept)", Time: time.Now(),
		}
	}
	if pktType == tnsPacketTypeRefuse {
		// Server replied Refuse — still proves it's an Oracle TNS
		// listener (just one we can't auth to). / 服务器返 Refuse
		// ——仍证明它是 Oracle TNS 监听器（只是我们没权限连）。
		return &common.Result{
			Host: host, Port: port, Service: "oracle",
			Banner: "Oracle TNS (Refuse)", Time: time.Now(),
		}
	}
	return nil
}

// buildTNSConnect builds a minimal TNS Connect packet for the
// given service name. / buildTNSConnect 构造给定 service 名的最小
// TNS Connect 包。
func buildTNSConnect(service string) []byte {
	// Connect data: <length-prefixed service name>. / Connect data：
	// <长度前缀> + service 名。
	data := []byte{byte(len(service) + 1), 0x01} // length + "service name" type
	data = append(data, []byte(service)...)

	// Header: 2 length + 2 checksum + 1 type + 1 reserved + 2 hdr-checksum
	//   + 1 version major + 1 version minor + 1 service options +
	//   2 SDU + 2 MTU + 1 NT char + 1 line turnaround + 1 charset high
	//   + 1 charset low (0x01) + 1 line turnaround + 1 connect flags 1
	//   + 2 connect flags 2 + 1 connect flags 3
	// = 22 bytes header. / 头共 22 字节。
	hdr := make([]byte, 22)
	hdr[0] = 0 // length placeholder
	hdr[1] = 0
	hdr[2] = 0 // checksum placeholder
	hdr[3] = 0
	hdr[4] = tnsPacketTypeConnect
	hdr[5] = 0 // reserved
	hdr[6] = 0 // header checksum placeholder
	hdr[7] = 0
	hdr[8] = 0x0A  // version major (10g+)
	hdr[9] = 0x02  // version minor
	hdr[10] = 0x41 // service options
	binary.BigEndian.PutUint16(hdr[11:13], 8192) // SDU
	binary.BigEndian.PutUint16(hdr[13:15], 8192) // MTU
	hdr[15] = 0x07 // NT protocol characteristics
	hdr[16] = 0x00 // line turnaround
	hdr[17] = 0x00 // charset high
	hdr[18] = 0x01 // charset low
	hdr[19] = 0x00 // line turnaround (again)
	hdr[20] = 0x04 // connect flags 1
	binary.BigEndian.PutUint16(hdr[21:23], 0) // connect flags 2
	// Total: 22 hdr + len(data) bytes. / 共 22 + len(data) 字节。
	total := uint16(22 + len(data))
	hdr[0] = byte(total >> 8)
	hdr[1] = byte(total)

	out := make([]byte, 0, total)
	out = append(out, hdr...)
	out = append(out, data...)
	return out
}

func readFull(c net.Conn, buf []byte) (int, error) {
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
