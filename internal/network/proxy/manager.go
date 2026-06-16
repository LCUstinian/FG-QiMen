// manager.go — global proxy manager with singleton pattern.
// manager.go — 单例模式全局代理管理器。
//
// Inspired by fscan/common/network.go L27-47, this manager ensures:
// - Only one proxy dialer instance per process
// - Connection reuse (avoid repeated proxy handshakes)
// - Thread-safe initialization via sync.Once
//
// 借鉴 fscan/common/network.go L27-47，本管理器确保：
// - 每进程仅一个代理拨号器实例
// - 连接复用（避免重复代理握手）
// - sync.Once 保证线程安全初始化
package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Manager manages the global proxy dialer.
// Manager 管理全局代理拨号器。
type Manager struct {
	config *ProxyConfig
	once   sync.Once
	dialer Dialer
	err    error
}

// Dialer is the proxy dialer interface.
// Dialer 是代理拨号器接口。
type Dialer interface {
	// DialContext connects to the target address through the proxy.
	// DialContext 通过代理连接目标地址。
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// NewManager creates a new Manager.
// NewManager 创建新的 Manager。
func NewManager(config *ProxyConfig) *Manager {
	if config == nil {
		config = DefaultProxyConfig()
	}
	return &Manager{config: config}
}

// GetDialer returns the global dialer (thread-safe, initialized once).
// GetDialer 返回全局拨号器（线程安全，仅初始化一次）。
func (m *Manager) GetDialer() (Dialer, error) {
	m.once.Do(func() {
		m.dialer, m.err = m.createDialer()
	})
	return m.dialer, m.err
}

// createDialer creates the appropriate dialer based on config.
// createDialer 根据配置创建适当的拨号器。
func (m *Manager) createDialer() (Dialer, error) {
	if !m.config.IsEnabled() {
		// Direct connection / 直连
		return &DirectDialer{
			timeout:   m.config.Timeout,
			localAddr: m.config.LocalAddr,
		}, nil
	}

	switch m.config.Type {
	case ProxyTypeSOCKS5:
		return NewSOCKS5Dialer(m.config)
	case ProxyTypeHTTP, ProxyTypeHTTPS:
		return NewHTTPDialer(m.config)
	default:
		return nil, fmt.Errorf("unsupported proxy type: %s", m.config.Type)
	}
}

// DirectDialer is a direct connection dialer (no proxy).
// DirectDialer 是直连拨号器（无代理）。
type DirectDialer struct {
	timeout   time.Duration
	localAddr string
}

// DialContext implements Dialer for direct connections.
// DialContext 实现直连的 Dialer。
func (d *DirectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: d.timeout,
	}

	// Bind to specific local interface if configured.
	// 如果配置了本地接口则绑定。
	if d.localAddr != "" {
		ip := net.ParseIP(d.localAddr)
		if ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	return dialer.DialContext(ctx, network, address)
}

// Global singleton manager instance (lazy initialization).
// 全局单例管理器实例（惰性初始化）。
var (
	globalManager     *Manager
	globalManagerOnce sync.Once
	globalManagerMu   sync.RWMutex
)

// InitGlobalManager initializes the global proxy manager.
// InitGlobalManager 初始化全局代理管理器。
//
// This should be called once at program startup with the user's proxy
// configuration. Subsequent calls are ignored.
//
// 应在程序启动时用用户的代理配置调用一次。后续调用被忽略。
func InitGlobalManager(config *ProxyConfig) {
	globalManagerOnce.Do(func() {
		globalManagerMu.Lock()
		defer globalManagerMu.Unlock()
		globalManager = NewManager(config)
	})
}

// GetGlobalDialer returns the global proxy dialer.
// GetGlobalDialer 返回全局代理拨号器。
//
// If InitGlobalManager was not called, returns a direct dialer.
// 如果未调用 InitGlobalManager，返回直连拨号器。
func GetGlobalDialer() (Dialer, error) {
	globalManagerMu.RLock()
	mgr := globalManager
	globalManagerMu.RUnlock()

	if mgr == nil {
		// Auto-initialize with default config (direct connection).
		// 自动用默认配置初始化（直连）。
		InitGlobalManager(DefaultProxyConfig())
		globalManagerMu.RLock()
		mgr = globalManager
		globalManagerMu.RUnlock()
	}

	return mgr.GetDialer()
}

// ResetGlobalManager resets the global manager (for testing).
// ResetGlobalManager 重置全局管理器（测试用）。
func ResetGlobalManager() {
	globalManagerMu.Lock()
	defer globalManagerMu.Unlock()
	globalManager = nil
	globalManagerOnce = sync.Once{}
}
