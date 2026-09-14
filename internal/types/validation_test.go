// Package types: validation tests.
// Package types: 验证测试。
package types

import (
	"testing"
	"time"
)

// TestValidateUDPThreadOverrides covers the A4 UDP pool injection
// surface's validation loop: zero means "shipped default" and must be
// accepted; negatives and >10000 are rejected exactly like Threads.
// / TestValidateUDPThreadOverrides 覆盖 A4 UDP 池注入表面的校验循环：
// 零值表示"出厂默认"必须接受；负值与 >10000 与 Threads 同界拒绝。
func TestValidateUDPThreadOverrides(t *testing.T) {
	build := func(udpThreads, udpMax int) *Config {
		return &Config{
			Threads:         100,
			Timeout:         time.Second,
			ShutdownTimeout: 5 * time.Second,
			UDPThreads:      udpThreads,
			UDPMaxThreads:   udpMax,
		}
	}
	tests := []struct {
		name       string
		udpThreads int
		udpMax     int
		wantErr    bool
	}{
		{"zero values = shipped defaults", 0, 0, false},
		{"a4 knee 800/800", 800, 800, false},
		{"max valid", 10000, 10000, false},
		{"negative target", -1, 0, true},
		{"negative cap", 0, -1, true},
		{"too large target", 10001, 0, true},
		{"too large cap", 0, 10001, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := build(tt.udpThreads, tt.udpMax).Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(udp-threads=%d, udp-max=%d) error = %v, wantErr %v",
					tt.udpThreads, tt.udpMax, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		// Valid cases / 有效案例
		{"valid IP", "192.168.1.1", false},
		{"valid CIDR", "192.168.1.0/24", false},
		{"valid range", "192.168.1.1-192.168.1.254", false},
		{"valid hostname", "example.com", false},
		{"valid subdomain", "www.example.com", false},
		{"valid comma list", "192.168.1.1,192.168.1.2", false},

		// Invalid cases / 无效案例
		{"empty", "", true},
		{"invalid CIDR", "192.168.1.0/33", true},
		{"invalid range", "192.168.1.1-999.999.999.999", true},
		{"invalid IP", "999.999.999.999", true},
		{"invalid hostname", "invalid..hostname", true},
		// Note: "invalid" is treated as a valid hostname, so comma list with it is valid
		// 注意："invalid" 被视为有效主机名，所以包含它的逗号列表是有效的
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHost(tt.host)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHost(%q) error = %v, wantErr %v", tt.host, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"valid port 80", 80, false},
		{"valid port 1", 1, false},
		{"valid port 65535", 65535, false},
		{"invalid port 0", 0, true},
		{"invalid port -1", -1, true},
		{"invalid port 65536", 65536, true},
		{"invalid port 99999", 99999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePort(%d) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePortString(t *testing.T) {
	tests := []struct {
		name    string
		portStr string
		want    int
		wantErr bool
	}{
		{"valid 80", "80", 80, false},
		{"valid with spaces", "  443  ", 443, false},
		{"invalid empty", "", 0, true},
		{"invalid non-numeric", "abc", 0, true},
		{"invalid negative", "-1", 0, true},
		{"invalid too large", "99999", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidatePortString(tt.portStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePortString(%q) error = %v, wantErr %v", tt.portStr, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ValidatePortString(%q) = %d, want %d", tt.portStr, got, tt.want)
			}
		})
	}
}

func TestValidateThreads(t *testing.T) {
	tests := []struct {
		name    string
		threads int
		wantErr bool
	}{
		{"valid 100", 100, false},
		{"valid 1", 1, false},
		{"valid 10000", 10000, false},
		{"invalid 0", 0, true},
		{"invalid -1", -1, true},
		{"invalid 10001", 10001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateThreads(tt.threads)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateThreads(%d) error = %v, wantErr %v", tt.threads, err, tt.wantErr)
			}
		})
	}
}

// (TestSanitizeFilePath removed in v0.3.1 — see Phase 1.10 of the
// optimization roadmap. SanitizeFilePath was dead code with no callers;
// path-traversal coverage now lives in the workspace package tests.
// / TestSanitizeFilePath 在 v0.3.1 移除——见优化路线图 Phase 1.10。
// SanitizeFilePath 是无调用方的 dead code；路径遍历覆盖现在在
// workspace 包测试里。)

// TestValidateTimeout covers the upper + lower bounds of the
// per-op timeout validator. The upper bound (3600s = 1h) exists
// because a misconfigured "timeout = 99999999" would block
// workers for hours and effectively DoS the operator's
// machine. / 测每操作 timeout 验证器的上下界。上界（3600s = 1h）
// 存在是因为错配的"timeout = 99999999"会把 worker 卡数小时，等于
// 把操作员的机器 DoS 掉。
func TestValidateTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout int64
		wantErr bool
	}{
		{"valid 1ns", 1, false},
		{"valid 3s", 3, false},
		{"valid 1h", 3600, false},
		{"invalid 0", 0, true},
		{"invalid -1", -1, true},
		{"invalid 3601", 3601, true},
		{"invalid 99999999", 99999999, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTimeout("test", tt.timeout)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTimeout(%d) error = %v, wantErr %v", tt.timeout, err, tt.wantErr)
			}
		})
	}
}
