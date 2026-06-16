// http.go — HTTP/HTTPS CONNECT proxy dialer implementation.
// http.go — HTTP/HTTPS CONNECT 代理拨号器实现。
package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// HTTPDialer implements HTTP/HTTPS CONNECT proxy dialing.
// HTTPDialer 实现 HTTP/HTTPS CONNECT 代理拨号。
type HTTPDialer struct {
	config *ProxyConfig
}

// NewHTTPDialer creates a new HTTP/HTTPS dialer.
// NewHTTPDialer 创建新的 HTTP/HTTPS 拨号器。
func NewHTTPDialer(config *ProxyConfig) (*HTTPDialer, error) {
	if config.Address == "" {
		return nil, errors.New("HTTP proxy address is empty")
	}
	return &HTTPDialer{config: config}, nil
}

// DialContext connects to the target through HTTP CONNECT proxy.
// DialContext 通过 HTTP CONNECT 代理连接目标。
func (d *HTTPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
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
		return nil, fmt.Errorf("failed to connect to HTTP proxy %s: %w", d.config.Address, err)
	}

	// Upgrade to TLS if HTTPS proxy / 如果是 HTTPS 代理则升级到 TLS
	if d.config.Type == ProxyTypeHTTPS {
		host, _, _ := net.SplitHostPort(d.config.Address)
		// M8 audit fix: respect the global --insecure-tls flag instead
		// of hard-coding InsecureSkipVerify: true. The previous code
		// bypassed certificate verification unconditionally, exposing
		// every HTTPS proxy user to MITM even when they explicitly
		// wanted verification. / M8 审计修法：尊重全局 --insecure-tls
		// flag，而非硬编码 InsecureSkipVerify: true。旧代码无条件绕过
		// 证书校验，让每个 HTTPS 代理用户即使显式要校验也暴露于 MITM。
		tlsCfg := transport.TLSConfig(false)
		tlsCfg.ServerName = host
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake failed: %w", err)
		}
		conn = tlsConn
	}

	// Send CONNECT request / 发送 CONNECT 请求
	if err := d.connect(conn, address); err != nil {
		conn.Close()
		return nil, fmt.Errorf("HTTP CONNECT failed: %w", err)
	}

	return conn, nil
}

// connect sends HTTP CONNECT request to establish tunnel.
// connect 发送 HTTP CONNECT 请求建立隧道。
func (d *HTTPDialer) connect(conn net.Conn, address string) error {
	// Build CONNECT request / 构建 CONNECT 请求
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", address, address)

	// Add Proxy-Authorization header if credentials provided.
	// 如果提供了凭据则添加 Proxy-Authorization 头。
	if d.config.Username != "" || d.config.Password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(
			d.config.Username + ":" + d.config.Password,
		))
		req += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)
	}

	req += "\r\n"

	// Send request / 发送请求
	if _, err := conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("failed to send CONNECT request: %w", err)
	}

	// Read response / 读取响应
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return fmt.Errorf("failed to read CONNECT response: %w", err)
	}
	defer resp.Body.Close()

	// Check status code / 检查状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CONNECT request failed: %s", resp.Status)
	}

	return nil
}

// ValidateHTTPAddress validates HTTP proxy address format.
// ValidateHTTPAddress 验证 HTTP 代理地址格式。
func ValidateHTTPAddress(address string) error {
	if address == "" {
		return errors.New("HTTP proxy address is empty")
	}

	// Ensure format is host:port / 确保格式是 host:port
	if !strings.Contains(address, ":") {
		return fmt.Errorf("invalid HTTP proxy address (missing port): %s", address)
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid HTTP proxy address: %w", err)
	}

	if host == "" {
		return errors.New("HTTP proxy host is empty")
	}

	return nil
}
