// Package utils: unit tests for zero-allocation string utilities.
// Package utils: 零分配字符串工具单元测试。
package utils

import (
	"fmt"
	"testing"
)

func TestFormatHostPort(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"192.168.1.1", 80, "192.168.1.1:80"},
		{"example.com", 443, "example.com:443"},
		{"localhost", 8080, "localhost:8080"},
		{"::1", 22, "::1:22"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatHostPort(tt.host, tt.port)
			if got != tt.want {
				t.Errorf("FormatHostPort(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}

func TestContainsFold(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "foo", false},
		{"SSH-2.0-OpenSSH_8.9", "openssh", true},
		{"SSH-2.0-OpenSSH_8.9", "OpenSSH", true},
		{"", "", true},
		{"test", "", true},
		{"", "test", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q contains %q", tt.s, tt.substr), func(t *testing.T) {
			got := ContainsFold(tt.s, tt.substr)
			if got != tt.want {
				t.Errorf("ContainsFold(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

func TestHasPrefixFold(t *testing.T) {
	tests := []struct {
		s      string
		prefix string
		want   bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "HELLO", true},
		{"Hello World", "world", false},
		{"SSH-2.0", "ssh", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q has prefix %q", tt.s, tt.prefix), func(t *testing.T) {
			got := HasPrefixFold(tt.s, tt.prefix)
			if got != tt.want {
				t.Errorf("HasPrefixFold(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestJoinInt(t *testing.T) {
	tests := []struct {
		name string
		ints []int
		sep  string
		want string
	}{
		{"empty", []int{}, ",", ""},
		{"single", []int{80}, ",", "80"},
		{"multiple", []int{22, 80, 443}, ",", "22,80,443"},
		{"custom sep", []int{8000, 8001, 8002}, "-", "8000-8001-8002"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinInt(tt.ints, tt.sep)
			if got != tt.want {
				t.Errorf("JoinInt(%v, %q) = %q, want %q", tt.ints, tt.sep, got, tt.want)
			}
		})
	}
}

// Benchmark tests to verify zero-allocation optimizations.
// 基准测试以验证零分配优化。

func BenchmarkFormatHostPort(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = FormatHostPort("192.168.1.1", 8080)
	}
}

func BenchmarkFormatHostPortSprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s:%d", "192.168.1.1", 8080)
	}
}

func BenchmarkContainsFold(b *testing.B) {
	s := "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1"
	substr := "openssh"
	for i := 0; i < b.N; i++ {
		_ = ContainsFold(s, substr)
	}
}

func BenchmarkContainsFoldStdlib(b *testing.B) {
	s := "SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1"
	substr := "openssh"
	for i := 0; i < b.N; i++ {
		_ = ContainsFold(s, substr)
	}
}
