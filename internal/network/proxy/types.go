// Package proxy provides global proxy management for FG-QiMen.
// Package proxy 为 FG-QiMen 提供全局代理管理。
//
// Inspired by fscan's proxy architecture, this package implements:
// - Single-instance proxy dialer (connection reuse)
// - SOCKS5/HTTP/HTTPS support
// - 4-stage connection validation
// - Transparent proxy detection
//
// 借鉴 fscan 的代理架构，本包实现：
// - 单例代理拨号器（连接复用）
// - SOCKS5/HTTP/HTTPS 支持
// - 4 阶段连接验证
// - 透明代理检测
package proxy

import (
	"time"
)

// ProxyType defines the proxy protocol type.
// ProxyType 定义代理协议类型。
type ProxyType string

const (
	// ProxyTypeNone means no proxy (direct connection).
	// ProxyTypeNone 表示无代理（直连）。
	ProxyTypeNone ProxyType = "none"

	// ProxyTypeSOCKS5 is SOCKS5 proxy (with optional username/password auth).
	// ProxyTypeSOCKS5 是 SOCKS5 代理（可选用户名密码认证）。
	ProxyTypeSOCKS5 ProxyType = "socks5"

	// ProxyTypeHTTP is HTTP CONNECT proxy.
	// ProxyTypeHTTP 是 HTTP CONNECT 代理。
	ProxyTypeHTTP ProxyType = "http"

	// ProxyTypeHTTPS is HTTPS CONNECT proxy.
	// ProxyTypeHTTPS 是 HTTPS CONNECT 代理。
	ProxyTypeHTTPS ProxyType = "https"
)

// ProxyConfig holds proxy configuration.
// ProxyConfig 存放代理配置。
type ProxyConfig struct {
	// Type is the proxy protocol type.
	// Type 是代理协议类型。
	Type ProxyType

	// Address is the proxy server address (host:port).
	// Address 是代理服务器地址（host:port）。
	Address string

	// Username is the optional proxy authentication username.
	// Username 是可选的代理认证用户名。
	Username string

	// Password is the optional proxy authentication password.
	// Password 是可选的代理认证密码。
	Password string

	// Timeout is the connection timeout.
	// Timeout 是连接超时。
	Timeout time.Duration

	// LocalAddr is the optional local interface IP to bind.
	// LocalAddr 是可选的本地接口 IP（网卡绑定）。
	LocalAddr string
}

// DefaultProxyConfig returns a ProxyConfig with sensible defaults.
// DefaultProxyConfig 返回带合理默认值的 ProxyConfig。
func DefaultProxyConfig() *ProxyConfig {
	return &ProxyConfig{
		Type:    ProxyTypeNone,
		Timeout: 5 * time.Second,
	}
}

// IsEnabled returns true if proxy is configured.
// IsEnabled 在配置了代理时返回 true。
func (c *ProxyConfig) IsEnabled() bool {
	return c.Type != ProxyTypeNone && c.Address != ""
}
