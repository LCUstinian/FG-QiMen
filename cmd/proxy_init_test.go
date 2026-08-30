// proxy_init_test.go — unit tests for cmd/proxy_init.go helper
// functions. These were 0% covered before because the integration
// path (buildProxyConfig → InitGlobalManager) requires the cobra
// root command to be invoked. / cmd/proxy_init.go 辅助函数的单元
// 测试。集成路径 (buildProxyConfig → InitGlobalManager) 需要
// 调用 cobra root command 才能触发，单独跑 helper 之前是 0% 覆盖。
package cmd

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/network/proxy"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func TestNormalizeSocks5Address(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no scheme", "127.0.0.1:1080", "127.0.0.1:1080"},
		{"with scheme", "socks5://127.0.0.1:1080", "127.0.0.1:1080"},
		{"with creds", "socks5://user:pass@127.0.0.1:1080", "127.0.0.1:1080"},
		{"with scheme and creds", "socks5://user:pass@vpn.example.com:9050", "vpn.example.com:9050"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeSocks5Address(c.in); got != c.want {
				t.Errorf("normalizeSocks5Address(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExtractSocks5Auth(t *testing.T) {
	cases := []struct {
		name     string
		addr     string
		envUser  string
		envPass  string
		wantUser string
		wantPass string
	}{
		{
			name:     "from url",
			addr:     "socks5://alice:secret@127.0.0.1:1080",
			wantUser: "alice",
			wantPass: "secret",
		},
		{
			name:     "no creds",
			addr:     "socks5://127.0.0.1:1080",
			wantUser: "",
			wantPass: "",
		},
		{
			name:     "no scheme",
			addr:     "user:pass@127.0.0.1:1080",
			wantUser: "user",
			wantPass: "pass",
		},
		{
			name:     "no @ separator",
			addr:     "socks5://127.0.0.1:1080",
			wantUser: "",
			wantPass: "",
		},
		{
			name:     "user only no pass",
			addr:     "socks5://alice@127.0.0.1:1080",
			wantUser: "",
			wantPass: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.envUser != "" {
				t.Setenv("FG_QIMEN_SOCKS5_USER", c.envUser)
				t.Setenv("FG_QIMEN_SOCKS5_PASS", c.envPass)
			} else {
				t.Setenv("FG_QIMEN_SOCKS5_USER", "")
				t.Setenv("FG_QIMEN_SOCKS5_PASS", "")
			}
			u, p := extractSocks5Auth(c.addr)
			if u != c.wantUser || p != c.wantPass {
				t.Errorf("extractSocks5Auth(%q) = (%q, %q), want (%q, %q)",
					c.addr, u, p, c.wantUser, c.wantPass)
			}
		})
	}
}

func TestExtractSocks5Auth_EnvOverride(t *testing.T) {
	// Env vars FG_QIMEN_SOCKS5_USER/PASS take precedence over URL creds.
	// / 环境变量 FG_QIMEN_SOCKS5_USER/PASS 优先于 URL 里的凭据。
	t.Setenv("FG_QIMEN_SOCKS5_USER", "env_user")
	t.Setenv("FG_QIMEN_SOCKS5_PASS", "env_pass")
	u, p := extractSocks5Auth("socks5://url_user:url_pass@127.0.0.1:1080")
	if u != "env_user" || p != "env_pass" {
		t.Errorf("env override not applied: got (%q, %q), want (env_user, env_pass)", u, p)
	}
}

func TestNormalizeHTTPAddress(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no scheme", "127.0.0.1:8080", "127.0.0.1:8080"},
		{"http", "http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"https", "https://127.0.0.1:8443", "127.0.0.1:8443"},
		{"with creds", "http://user:pass@proxy.example.com:8080", "proxy.example.com:8080"},
		{"https with creds", "https://u:p@proxy.example.com:443", "proxy.example.com:443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeHTTPAddress(c.in); got != c.want {
				t.Errorf("normalizeHTTPAddress(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExtractHTTPAuth(t *testing.T) {
	cases := []struct {
		name               string
		addr               string
		wantUser, wantPass string
	}{
		{"http with creds", "http://alice:secret@127.0.0.1:8080", "alice", "secret"},
		{"https with creds", "https://alice:secret@127.0.0.1:8443", "alice", "secret"},
		{"no creds", "http://127.0.0.1:8080", "", ""},
		{"no scheme with creds", "user:pass@127.0.0.1:8080", "user", "pass"},
		{"user only", "http://alice@127.0.0.1:8080", "", ""},
		{"no @ separator", "http://127.0.0.1:8080", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, p := extractHTTPAuth(c.addr)
			if u != c.wantUser || p != c.wantPass {
				t.Errorf("extractHTTPAuth(%q) = (%q, %q), want (%q, %q)",
					c.addr, u, p, c.wantUser, c.wantPass)
			}
		})
	}
}

func TestBuildProxyConfig_None(t *testing.T) {
	cfg := &types.Config{Timeout: 0, Iface: ""}
	pc := buildProxyConfig(cfg)
	if pc.IsEnabled() {
		t.Errorf("expected proxy disabled, got enabled: %+v", pc)
	}
	if pc.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", pc.Timeout)
	}
}

func TestBuildProxyConfig_SOCKS5Priority(t *testing.T) {
	// Both Socks5 and Proxy set; SOCKS5 should win (priority order).
	// / Socks5 和 Proxy 都设；SOCKS5 优先。
	cfg := &types.Config{
		Timeout: 5 * 1e9, // 5s in nanoseconds
		Socks5:  "socks5://alice:secret@127.0.0.1:1080",
		Proxy:   "http://bob:pass@127.0.0.1:8080",
		Iface:   "10.0.0.1",
	}
	t.Setenv("FG_QIMEN_SOCKS5_USER", "")
	t.Setenv("FG_QIMEN_SOCKS5_PASS", "")
	pc := buildProxyConfig(cfg)
	if pc.Type != proxy.ProxyTypeSOCKS5 {
		t.Errorf("Type = %v, want socks5", pc.Type)
	}
	if pc.Address != "127.0.0.1:1080" {
		t.Errorf("Address = %q, want 127.0.0.1:1080", pc.Address)
	}
	if pc.Username != "alice" || pc.Password != "secret" {
		t.Errorf("Auth = (%q, %q), want (alice, secret)", pc.Username, pc.Password)
	}
	if pc.LocalAddr != "10.0.0.1" {
		t.Errorf("LocalAddr = %q, want 10.0.0.1", pc.LocalAddr)
	}
}

func TestBuildProxyConfig_HTTP(t *testing.T) {
	cfg := &types.Config{
		Proxy: "http://bob:pass@127.0.0.1:8080",
	}
	pc := buildProxyConfig(cfg)
	if !pc.IsEnabled() {
		t.Errorf("expected proxy enabled, got disabled")
	}
	if pc.Type != proxy.ProxyTypeHTTP {
		t.Errorf("Type = %v, want http", pc.Type)
	}
	if pc.Address != "127.0.0.1:8080" {
		t.Errorf("Address = %q, want 127.0.0.1:8080", pc.Address)
	}
}

func TestBuildProxyConfig_HTTPS(t *testing.T) {
	cfg := &types.Config{
		Proxy: "https://bob:pass@127.0.0.1:8443",
	}
	pc := buildProxyConfig(cfg)
	if pc.Type != proxy.ProxyTypeHTTPS {
		t.Errorf("Type = %v, want https", pc.Type)
	}
}
