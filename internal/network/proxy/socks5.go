// socks5.go — SOCKS5 proxy dialer implementation.
// socks5.go — SOCKS5 代理拨号器实现。
package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// SOCKS5 protocol constants.
const (
	socks5Version = 0x05

	// Authentication methods / 认证方法
	socks5AuthNone     = 0x00
	socks5AuthPassword = 0x02
	socks5AuthNoAccept = 0xFF

	// Commands / 命令
	socks5CmdConnect = 0x01

	// Address types / 地址类型
	socks5AddrIPv4   = 0x01
	socks5AddrDomain = 0x03
	socks5AddrIPv6   = 0x04

	// Reply codes / 响应码
	socks5ReplySuccess = 0x00
)

// SOCKS5Dialer implements SOCKS5 proxy dialing.
// SOCKS5Dialer 实现 SOCKS5 代理拨号。
type SOCKS5Dialer struct {
	config *ProxyConfig
}

// NewSOCKS5Dialer creates a new SOCKS5 dialer.
// NewSOCKS5Dialer 创建新的 SOCKS5 拨号器。
func NewSOCKS5Dialer(config *ProxyConfig) (*SOCKS5Dialer, error) {
	if config.Address == "" {
		return nil, errors.New("SOCKS5 proxy address is empty")
	}
	return &SOCKS5Dialer{config: config}, nil
}

// DialContext connects to the target through SOCKS5 proxy.
// DialContext 通过 SOCKS5 代理连接目标。
func (d *SOCKS5Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Connect to proxy server / 连接代理服务器
	dialer := &net.Dialer{
		Timeout: d.config.Timeout,
	}
	if d.config.LocalAddr != "" {
		ip := net.ParseIP(d.config.LocalAddr)
		if ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", d.config.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SOCKS5 proxy %s: %w", d.config.Address, err)
	}

	// SOCKS5 handshake / SOCKS5 握手
	if err := d.handshake(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 handshake failed: %w", err)
	}

	// Send CONNECT request / 发送 CONNECT 请求
	if err := d.connect(conn, address); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 CONNECT failed: %w", err)
	}

	return conn, nil
}

// handshake performs SOCKS5 authentication handshake.
// handshake 执行 SOCKS5 认证握手。
func (d *SOCKS5Dialer) handshake(conn net.Conn) error {
	// Determine authentication method / 确定认证方法
	var authMethod byte
	if d.config.Username != "" || d.config.Password != "" {
		authMethod = socks5AuthPassword
	} else {
		authMethod = socks5AuthNone
	}

	// Send authentication methods / 发送认证方法
	// Format: [VER, NMETHODS, METHODS...]
	req := []byte{socks5Version, 1, authMethod}
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("failed to send auth methods: %w", err)
	}

	// Receive server's chosen method / 接收服务器选择的方法
	// Format: [VER, METHOD]
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if resp[0] != socks5Version {
		return fmt.Errorf("invalid SOCKS version: %d", resp[0])
	}

	if resp[1] == socks5AuthNoAccept {
		return errors.New("no acceptable authentication methods")
	}

	// Perform username/password authentication if needed / 如需则执行用户名密码认证
	if resp[1] == socks5AuthPassword {
		return d.authenticatePassword(conn)
	}

	return nil
}

// authenticatePassword performs username/password authentication.
// authenticatePassword 执行用户名密码认证。
func (d *SOCKS5Dialer) authenticatePassword(conn net.Conn) error {
	// Format: [VER, ULEN, UNAME, PLEN, PASSWD]
	ulen := len(d.config.Username)
	plen := len(d.config.Password)
	if ulen > 255 || plen > 255 {
		return errors.New("username or password too long (max 255 bytes)")
	}

	buf := make([]byte, 0, 3+ulen+plen)
	buf = append(buf, 0x01) // Sub-negotiation version
	buf = append(buf, byte(ulen))
	buf = append(buf, []byte(d.config.Username)...)
	buf = append(buf, byte(plen))
	buf = append(buf, []byte(d.config.Password)...)

	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("failed to send credentials: %w", err)
	}

	// Read response / 读取响应
	// Format: [VER, STATUS]
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read auth status: %w", err)
	}

	if resp[1] != 0x00 {
		return fmt.Errorf("authentication failed (status: %d)", resp[1])
	}

	return nil
}

// connect sends CONNECT request to target address.
// connect 向目标地址发送 CONNECT 请求。
func (d *SOCKS5Dialer) connect(conn net.Conn, address string) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid address %s: %w", address, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %s", portStr)
	}

	// Build CONNECT request / 构建 CONNECT 请求
	// Format: [VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT]
	req := make([]byte, 0, 4+1+len(host)+2)
	req = append(req, socks5Version, socks5CmdConnect, 0x00)

	// Encode destination address / 编码目标地址
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			// IPv4
			req = append(req, socks5AddrIPv4)
			req = append(req, ip4...)
		} else {
			// IPv6
			req = append(req, socks5AddrIPv6)
			req = append(req, ip...)
		}
	} else {
		// Domain name / 域名
		if len(host) > 255 {
			return fmt.Errorf("domain name too long: %s", host)
		}
		req = append(req, socks5AddrDomain)
		req = append(req, byte(len(host)))
		req = append(req, []byte(host)...)
	}

	// Append port / 追加端口
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	req = append(req, portBytes...)

	// Send CONNECT request / 发送 CONNECT 请求
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("failed to send CONNECT request: %w", err)
	}

	// Read response / 读取响应
	// Format: [VER, REP, RSV, ATYP, BND.ADDR, BND.PORT]
	resp := make([]byte, 4)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read CONNECT response: %w", err)
	}

	if resp[0] != socks5Version {
		return fmt.Errorf("invalid SOCKS version in response: %d", resp[0])
	}

	if resp[1] != socks5ReplySuccess {
		return fmt.Errorf("CONNECT request failed (reply code: %d)", resp[1])
	}

	// Read bound address (discard) / 读取绑定地址（丢弃）
	addrType := resp[3]
	switch addrType {
	case socks5AddrIPv4:
		io.CopyN(io.Discard, conn, 4+2) // IP + port
	case socks5AddrIPv6:
		io.CopyN(io.Discard, conn, 16+2) // IP + port
	case socks5AddrDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return err
		}
		io.CopyN(io.Discard, conn, int64(lenBuf[0])+2) // domain + port
	default:
		return fmt.Errorf("unknown address type: %d", addrType)
	}

	return nil
}

// ValidateSOCKS5Address validates SOCKS5 proxy address format.
// ValidateSOCKS5Address 验证 SOCKS5 代理地址格式。
func ValidateSOCKS5Address(address string) error {
	if address == "" {
		return errors.New("SOCKS5 address is empty")
	}

	// Ensure format is host:port / 确保格式是 host:port
	if !strings.Contains(address, ":") {
		return fmt.Errorf("invalid SOCKS5 address (missing port): %s", address)
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid SOCKS5 address: %w", err)
	}

	if host == "" {
		return errors.New("SOCKS5 host is empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid SOCKS5 port: %s", portStr)
	}

	return nil
}
