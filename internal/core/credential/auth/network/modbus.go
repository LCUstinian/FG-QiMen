// Package protocols: Modbus authenticator.
//
// Modbus TCP itself is credential-less (it's an ICS/SCADA protocol
// designed for trusted local networks, no auth). Many gateways
// (Schneider, Siemens, ABB) wrap it with HTTP/Basic or vendor auth.
// v0.1 treats the bare Modbus TCP probe as a hit when the device
// responds to a Read Device Identification request (function
// code 43/14, MEI type 14).
//
// HARD RULE: on a hit we return. We do NOT write to any registers
// or coils (no Read Holding Registers 03, no Write Single Coil 05,
// no Read Coils 01). Read-only identification only.
//
// 包 protocols：Modbus 认证器。
// Modbus TCP 本身无凭据（ICS/SCADA 协议，为可信内网设计，无认证）。
// 很多网关（施耐德、西门子、ABB）套了 HTTP/Basic 或厂商认证。v0.1
// 把裸 Modbus TCP 探针当作"设备响应了 Read Device Identification
// 请求（function code 43/14，MEI type 14）即命中"处理。
//
// 硬性原则：命中即返回。不写任何寄存器或线圈（不 Read Holding
// Registers 03、不 Write Single Coil 05、不 Read Coils 01）。只读
// 不写。
package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// ModbusAuthenticator authenticates against Modbus TCP devices via
// Read Device Identification (function code 43/14). /
// ModbusAuthenticator 通过 Read Device Identification（function code
// 43/14）对 Modbus TCP 设备认证。
//
// DefaultPort returns 502 (standard Modbus TCP). / DefaultPort 返 502
// （标准 Modbus TCP）。
type ModbusAuthenticator struct{}

// NewModbusAuthenticator returns a default Modbus authenticator.
// NewModbusAuthenticator 返回默认配置的 Modbus 认证器。
func NewModbusAuthenticator() *ModbusAuthenticator { return &ModbusAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *ModbusAuthenticator) Name() string { return "modbus" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *ModbusAuthenticator) DefaultPorts() []int {
	return []int{502}
}

// Modbus TCP MBAP header (7 bytes): / Modbus TCP MBAP 头（7 字节）：
//   bytes 0-1: transaction ID
//   bytes 2-3: protocol ID (0x0000 for Modbus)
//   bytes 4-5: length (BE, bytes after this field including unit ID)
//   byte 6:    unit ID
//   then PDU: function code (1) + data
//
// Modbus PDU for Read Device Identification (function code 43 = 0x2B):
//   - MEI type 14 (0x0E)
//   - Read Device ID code 01
//   - Object ID 00 (basic info)
// / Modbus PDU for Read Device Identification（function code 43 = 0x2B）：
//   - MEI type 14 (0x0E)
//   - Read Device ID code 01
//   - Object ID 00 (basic info）

// Authenticate implements credential.Authenticator. Tries each cred;
// Modbus TCP is technically credential-less so cred.User / cred.Pass
// are ignored — we just probe the device. We still iterate creds to
// keep the contract uniform. / Authenticate 实现 credential.Authenticator。
// 按顺序尝试每个 cred；Modbus TCP 本身无凭据所以忽略 cred.User /
// cred.Pass——只探针。仍迭代 creds 保持契约统一。
func (a *ModbusAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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

// attempt runs one Modbus TCP probe. / attempt 跑一次 Modbus TCP
// 探针。
func (a *ModbusAuthenticator) attempt(ctx context.Context, addr string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// Build Modbus TCP request: MBAP(7) + PDU. / 构造 Modbus TCP 请求：
	// MBAP(7) + PDU。
	// PDU: function code 43 (0x2B), MEI type 14 (0x0E), Read Device
	// ID code 01, Object ID 00.
	// / PDU：function code 43 (0x2B)，MEI type 14 (0x0E)，Read Device
	// ID code 01，Object ID 00。
	pdu := []byte{0x2b, 0x0e, 0x01, 0x00}
	length := uint16(1 + 1 + 1 + 1 + len(pdu)) // unit_id(1) + pdu
	header := make([]byte, 7)
	binary.BigEndian.PutUint16(header[0:2], 1) // transaction ID
	binary.BigEndian.PutUint16(header[2:4], 0) // protocol ID
	binary.BigEndian.PutUint16(header[4:6], length)
	header[6] = 1 // unit ID
	out := append(header, pdu...)
	if _, err := conn.Write(out); err != nil {
		return false, err
	}
	// Read response: MBAP(7) + function code + MEI type + ... = at
	// least 10 bytes. / 读响应：MBAP(7) + function code + MEI type +
	// ... = 至少 10 字节。
	resp := make([]byte, 256)
	n, err := readFullMB(conn, resp)
	if err != nil {
		return false, err
	}
	if n < 10 {
		return false, nil
	}
	// Verify response: function code is 0x2B, MEI type is 0x0E.
	// / 验证响应：function code 是 0x2B，MEI type 是 0x0E。
	if resp[7] != 0x2b || resp[8] != 0x0e {
		return false, nil
	}
	return true, nil
}

func readFullMB(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// init registers the Modbus authenticator. / init 注册 Modbus 认证器。
func init() {
	credential.Register(NewModbusAuthenticator())
}

// Keep fmt import alive for future debug logging. / fmt 保留供将来
// debug 日志。
var _ = fmt.Sprintf
