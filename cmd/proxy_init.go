// proxy_init.go — proxy initialization helpers for cmd package.
// proxy_init.go — cmd 包的代理初始化辅助函数。
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/network/proxy"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// initProxyManager initializes the global proxy manager based on config.
// initProxyManager 根据配置初始化全局代理管理器。
//
// This must be called BEFORE any network operations. It converts the
// user's proxy flags (--proxy, --socks5, --iface) into a ProxyConfig
// and initializes the singleton manager.
//
// 必须在任何网络操作前调用。它把用户的代理 flag（--proxy、--socks5、
// --iface）转换为 ProxyConfig 并初始化单例管理器。
func initProxyManager(cfg *types.Config) error {
	proxyConfig := buildProxyConfig(cfg)
	proxy.InitGlobalManager(proxyConfig)

	// Log proxy status / 记录代理状态
	if proxyConfig.IsEnabled() {
		if !cfg.Silent {
			fmt.Fprintf(os.Stderr, "[*] Proxy enabled: %s (%s)\n",
				proxyConfig.Type, proxyConfig.Address)
		}
	}

	return nil
}

// buildProxyConfig converts Config flags to ProxyConfig.
// buildProxyConfig 把 Config flag 转换为 ProxyConfig。
func buildProxyConfig(cfg *types.Config) *proxy.ProxyConfig {
	pc := proxy.DefaultProxyConfig()
	pc.Timeout = cfg.Timeout
	pc.LocalAddr = cfg.Iface

	// Priority: SOCKS5 > HTTP/HTTPS > None
	// 优先级：SOCKS5 > HTTP/HTTPS > 无
	if cfg.Socks5 != "" {
		pc.Type = proxy.ProxyTypeSOCKS5
		pc.Address = normalizeSocks5Address(cfg.Socks5)
		// Extract username/password from socks5://user:pass@host:port
		// 从 socks5://user:pass@host:port 提取用户名密码
		if strings.HasPrefix(cfg.Socks5, "socks5://") {
			pc.Username, pc.Password = extractSocks5Auth(cfg.Socks5)
		}
	} else if cfg.Proxy != "" {
		if strings.HasPrefix(cfg.Proxy, "https://") {
			pc.Type = proxy.ProxyTypeHTTPS
		} else {
			pc.Type = proxy.ProxyTypeHTTP
		}
		pc.Address = normalizeHTTPAddress(cfg.Proxy)
		pc.Username, pc.Password = extractHTTPAuth(cfg.Proxy)
	}

	return pc
}

// normalizeSocks5Address normalizes SOCKS5 address format.
// normalizeSocks5Address 规范化 SOCKS5 地址格式。
func normalizeSocks5Address(addr string) string {
	// Remove socks5:// prefix if present
	// 移除 socks5:// 前缀（如有）
	addr = strings.TrimPrefix(addr, "socks5://")

	// Remove credentials if present (already extracted)
	// 移除凭据部分（已提取）
	if idx := strings.Index(addr, "@"); idx != -1 {
		addr = addr[idx+1:]
	}

	return addr
}

// extractSocks5Auth extracts username and password from socks5://user:pass@host:port
// extractSocks5Auth 从 socks5://user:pass@host:port 提取用户名和密码
func extractSocks5Auth(addr string) (string, string) {
	// Remove socks5:// prefix
	addr = strings.TrimPrefix(addr, "socks5://")

	// Find @ separator
	idx := strings.Index(addr, "@")
	if idx == -1 {
		return "", ""
	}

	// Extract user:pass part
	authPart := addr[:idx]
	parts := strings.SplitN(authPart, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	return "", ""
}

// normalizeHTTPAddress normalizes HTTP/HTTPS proxy address.
// normalizeHTTPAddress 规范化 HTTP/HTTPS 代理地址。
func normalizeHTTPAddress(addr string) string {
	// Remove http:// or https:// prefix
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")

	// Remove credentials if present
	if idx := strings.Index(addr, "@"); idx != -1 {
		addr = addr[idx+1:]
	}

	return addr
}

// extractHTTPAuth extracts username and password from http://user:pass@host:port
// extractHTTPAuth 从 http://user:pass@host:port 提取用户名和密码
func extractHTTPAuth(addr string) (string, string) {
	// Remove protocol prefix
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")

	// Find @ separator
	idx := strings.Index(addr, "@")
	if idx == -1 {
		return "", ""
	}

	// Extract user:pass part
	authPart := addr[:idx]
	parts := strings.SplitN(authPart, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	return "", ""
}
