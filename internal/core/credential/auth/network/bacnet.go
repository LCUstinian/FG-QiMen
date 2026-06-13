// Package protocols: BACnet authenticator.
//
// Strategy: send a BACnet/IP Who-Is request (BVLC type 0x10, NPDU
// type 0x01, APDU Confirmed Service Choice 0x10) and wait for an
// I-Am response. A response = device reachable = hit (BACnet
// itself has no auth; v0.1 treats reachability as the credential
// "proof").
//
// HARD RULE: on a hit we return. We do NOT issue any write
// services (no WriteProperty, no AddListElement, no
// CreateObject).
//
// 包 protocols：BACnet 认证器。
// 策略：发 BACnet/IP Who-Is 请求（BVLC type 0x10，NPDU type 0x01，
// APDU Confirmed Service Choice 0x10）并等 I-Am 响应。响应 = 设备可达
// = 命中（BACnet 本身无认证；v0.1 把可达性当作凭据"证明"）。
//
// 硬性原则：命中即返回。不发任何写服务（不 WriteProperty、不
// AddListElement、不 CreateObject）。
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

// BACnetAuthenticator authenticates against BACnet/IP devices via
// Who-Is / I-Am. / BACnetAuthenticator 通过 Who-Is / I-Am 对
// BACnet/IP 设备认证。
//
// DefaultPort returns 47808 (standard BACnet/IP). / DefaultPort
// 返 47808（标准 BACnet/IP）。
type BACnetAuthenticator struct{}

// NewBACnetAuthenticator returns a default BACnet authenticator.
// NewBACnetAuthenticator 返回默认配置的 BACnet 认证器。
func NewBACnetAuthenticator() *BACnetAuthenticator { return &BACnetAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *BACnetAuthenticator) Name() string { return "bacnet" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *BACnetAuthenticator) DefaultPorts() []int {
	return []int{47808}
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *BACnetAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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

// attempt sends Who-Is and waits for I-Am. / attempt 发 Who-Is 并等
// I-Am。
func (a *BACnetAuthenticator) attempt(ctx context.Context, addr string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// Build BACnet/IP Who-Is packet:
	//   BVLC (4 bytes): type=0x0b (Original-Unicast NPDU), func=0x10
	//     (Original-Unicast), length (BE 2 bytes)
	//   NPDU (1 byte): version=0x01, no DNET/ADR (control 0x00)
	//   APDU Confirmed Service Choice = ReadProperty (3) with
	//   object-id=8 (device), property=70 (object-list).
	// / BACnet/IP Who-Is 包：
	//   BVLC (4 字节)：type=0x0b (Original-Unicast NPDU)，func=0x10
	//     (Original-Unicast)，length (BE 2 字节)
	//   NPDU (1 字节)：version=0x01，无 DNET/ADR (control 0x00)
	//   APDU Confirmed Service Choice = ReadProperty (3) + object-id=8
	//     (device) + property=70 (object-list)。
	//
	// For v0.1 we use a simpler "I-Am probe" — send the APDU and
	// see if we get any I-Am back. / v0.1 用更简"I-Am 探针"——发 APDU
	// 看是否回 I-Am。
	apdu := []byte{
		0x10, 0x00, // pdu type (0x10 = Confirmed Request)
		0x00,       // service choice = 0x00 (I-Am — but for Unconfirmed, it's 0x10; we use 0x10)
	}
	// Real Who-Is is Unconfirmed Request (PDU type 0x10), service
	// choice 0x10, no body. / 真 Who-Is 是 Unconfirmed Request（PDU
	// type 0x10），service choice 0x10，无 body。
	apdu = []byte{0x10, 0x10}
	// Build NPDU: 1 byte (version 0x01, no DNET/ADR). / 构造 NPDU：
	// 1 字节（version 0x01，无 DNET/ADR）。
	npdu := []byte{0x01}
	// Build BVLC: type=0x0a (Original-Unicast NPDU), func=0x10
	// (Original-Unicast), length = total.
	// / 构造 BVLC：type=0x0a (Original-Unicast NPDU)，func=0x10
	// (Original-Unicast)，length = total。
	body := append(npdu, apdu...)
	length := uint16(4 + len(body))
	hdr := []byte{0x0a, 0x10, 0x00, 0x00}
	binary.BigEndian.PutUint16(hdr[2:4], length)
	pkt := append(hdr, body...)
	if _, err := conn.Write(pkt); err != nil {
		return false, err
	}
	// Wait for I-Am (0x10 PDU + service choice 0x10 + device id +
	// vendor + ...). / 等 I-Am (0x10 PDU + service choice 0x10 + 设备
	// id + 厂商 + ...)。
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return false, nil
	}
	if n < 8 {
		return false, nil
	}
	// First 4 bytes BVLC: type 0x0a (Original-Unicast NPDU).
	// / 前 4 字节 BVLC：type 0x0a (Original-Unicast NPDU)。
	if buf[0] != 0x0a {
		return false, nil
	}
	// 5th byte NPDU: version 0x01. / 第 5 字节 NPDU：version 0x01。
	if buf[4] != 0x01 {
		return false, nil
	}
	// 6th byte APDU PDU type 0x10 (Unconfirmed Request).
	// / 第 6 字节 APDU PDU type 0x10 (Unconfirmed Request)。
	if buf[5] != 0x10 {
		return false, nil
	}
	// 7th byte APDU service choice 0x10 (I-Am). / 第 7 字节 APDU
	// service choice 0x10 (I-Am)。
	if buf[6] != 0x10 {
		return false, nil
	}
	return true, nil
}

// init registers the BACnet authenticator. / init 注册 BACnet 认证器。
func init() {
	credential.Register(NewBACnetAuthenticator())
}

// Keep fmt import alive for future debug. / fmt 保留供将来 debug。
var _ = fmt.Sprintf
