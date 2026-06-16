// Package types: input validation utilities.
// Package types: 输入验证工具。
package types

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// ValidateHost validates a host specification (IP/CIDR/range).
// ValidateHost 验证主机规格（IP/CIDR/范围）。
func ValidateHost(host string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host cannot be empty")
	}

	// Check for CIDR notation / 检查CIDR表示法
	if strings.Contains(host, "/") {
		_, _, err := net.ParseCIDR(host)
		if err != nil {
			return fmt.Errorf("invalid CIDR notation %q: %w", host, err)
		}
		return nil
	}

	// Check for IP range (e.g., 192.168.1.1-192.168.1.254) / 检查IP范围
	if strings.Contains(host, "-") {
		parts := strings.SplitN(host, "-", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid IP range format %q", host)
		}
		startIP := net.ParseIP(strings.TrimSpace(parts[0]))
		endIP := net.ParseIP(strings.TrimSpace(parts[1]))
		if startIP == nil || endIP == nil {
			return fmt.Errorf("invalid IP range %q: start or end is not a valid IP", host)
		}
		return nil
	}

	// Check for comma-separated list / 检查逗号分隔列表
	if strings.Contains(host, ",") {
		hosts := strings.Split(host, ",")
		for _, h := range hosts {
			if err := ValidateHost(strings.TrimSpace(h)); err != nil {
				return err
			}
		}
		return nil
	}

	// Single IP or hostname / 单个IP或主机名
	ip := net.ParseIP(host)
	if ip != nil {
		return nil // Valid IP
	}

	// Check if it looks like an invalid IP (contains only digits and dots but isn't valid)
	// 检查是否看起来像无效IP（仅包含数字和点但无效）
	if isIPLike(host) {
		return fmt.Errorf("invalid IP address %q", host)
	}

	// Validate hostname / 验证主机名
	if !isValidHostname(host) {
		return fmt.Errorf("invalid hostname %q", host)
	}

	return nil
}

// isIPLike checks if a string looks like an IP address (contains digits and dots).
// isIPLike 检查字符串是否看起来像IP地址（包含数字和点）。
func isIPLike(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c == '.') {
			return false
		}
	}
	return strings.Contains(s, ".")
}

// isValidHostname checks if a string is a valid hostname.
// isValidHostname 检查字符串是否为有效主机名。
func isValidHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}

	// Hostname regex: alphanumeric and hyphens, no leading/trailing hyphens
	// 主机名正则：字母数字和连字符，无前导/尾随连字符
	hostnameRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
	return hostnameRegex.MatchString(hostname)
}

// ValidatePort validates a port number.
// ValidatePort 验证端口号。
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range (1-65535)", port)
	}
	return nil
}

// ValidatePortString validates a port string and returns the port number.
// ValidatePortString 验证端口字符串并返回端口号。
func ValidatePortString(portStr string) (int, error) {
	portStr = strings.TrimSpace(portStr)
	if portStr == "" {
		return 0, fmt.Errorf("port string is empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q: not a number", portStr)
	}

	if err := ValidatePort(port); err != nil {
		return 0, err
	}

	return port, nil
}

// ValidateTimeout validates a timeout value.
// ValidateTimeout 验证超时值。
func ValidateTimeout(name string, timeout int64) error {
	if timeout <= 0 {
		return fmt.Errorf("%s must be positive, got %d", name, timeout)
	}
	if timeout > 3600 {
		return fmt.Errorf("%s too large (max 3600 seconds), got %d", name, timeout)
	}
	return nil
}

// ValidateThreads validates thread count.
// ValidateThreads 验证线程数量。
func ValidateThreads(threads int) error {
	if threads <= 0 {
		return fmt.Errorf("threads must be positive, got %d", threads)
	}
	if threads > 10000 {
		return fmt.Errorf("threads too large (max 10000), got %d", threads)
	}
	return nil
}

// SanitizeFilePath sanitizes a file path to prevent directory traversal.
// SanitizeFilePath 清理文件路径以防止目录遍历。
func SanitizeFilePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Check for directory traversal attempts / 检查目录遍历尝试
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path contains directory traversal: %q", path)
	}

	// Disallow absolute paths outside working directory (Unix) / 禁止工作目录外的绝对路径
	if strings.HasPrefix(path, "/etc") || strings.HasPrefix(path, "/root") {
		return "", fmt.Errorf("path outside working directory: %q", path)
	}

	return path, nil
}
