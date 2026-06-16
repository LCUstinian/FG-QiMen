// Package types: validation tests.
// Package types: 验证测试。
package types

import (
	"testing"
)

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

func TestSanitizeFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid relative", "result.txt", false},
		{"valid subdirectory", "output/result.txt", false},
		{"invalid empty", "", true},
		{"invalid traversal", "../../../etc/passwd", true},
		{"invalid absolute", "/etc/passwd", true},
		{"invalid root", "/root/.ssh/id_rsa", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SanitizeFilePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeFilePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
