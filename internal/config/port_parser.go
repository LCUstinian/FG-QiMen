// Package config provides port parsing utilities.
// Package config 提供端口解析工具。
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePortSpec parses a port specification string into a list of ports.
// ParsePortSpec 解析端口规格字符串为端口列表。
//
// Supported formats / 支持的格式：
//   - Port group name: "web", "db", "service", "common", "main"
//   - Single port: "80"
//   - Port range: "80-85"
//   - Comma-separated: "22,80,443"
//   - Mixed: "web,3306,8000-8010"
//   - Special: "all" for full range 1-65535
//
// Examples / 示例：
//   ParsePortSpec("web")           → 284 web ports
//   ParsePortSpec("22,80,443")     → [22, 80, 443]
//   ParsePortSpec("8000-8005")     → [8000, 8001, 8002, 8003, 8004, 8005]
//   ParsePortSpec("db,3306")       → database ports + 3306
//   ParsePortSpec("all")           → 1-65535
func ParsePortSpec(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty port specification")
	}

	// Special case: "all" means full range / 特殊情况："all" 表示全范围
	if spec == "all" {
		return makeRange(1, 65535), nil
	}

	// Split by comma / 按逗号分割
	parts := strings.Split(spec, ",")
	seen := make(map[int]bool)
	var result []int

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Try port group first / 先尝试端口组
		if ports, ok := GetPortGroup(part); ok {
			for _, p := range ports {
				if !seen[p] {
					seen[p] = true
					result = append(result, p)
				}
			}
			continue
		}

		// Try port range (e.g., "80-85") / 尝试端口范围（如 "80-85"）
		if strings.Contains(part, "-") {
			rangePorts, err := parsePortRange(part)
			if err != nil {
				return nil, fmt.Errorf("invalid port range %q: %w", part, err)
			}
			for _, p := range rangePorts {
				if !seen[p] {
					seen[p] = true
					result = append(result, p)
				}
			}
			continue
		}

		// Try single port / 尝试单个端口
		port, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid port specification %q: not a number or port group", part)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("port %d out of range (1-65535)", port)
		}
		if !seen[port] {
			seen[port] = true
			result = append(result, port)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid ports in specification %q", spec)
	}

	return result, nil
}

// parsePortRange parses a port range like "80-85" into [80, 81, 82, 83, 84, 85].
// parsePortRange 解析端口范围如 "80-85" 为 [80, 81, 82, 83, 84, 85]。
func parsePortRange(rangeSpec string) ([]int, error) {
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format (expected start-end)")
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid start port: %w", err)
	}

	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid end port: %w", err)
	}

	if start < 1 || start > 65535 {
		return nil, fmt.Errorf("start port %d out of range (1-65535)", start)
	}
	if end < 1 || end > 65535 {
		return nil, fmt.Errorf("end port %d out of range (1-65535)", end)
	}
	if start > end {
		return nil, fmt.Errorf("start port %d > end port %d", start, end)
	}

	// Limit range size to prevent memory exhaustion / 限制范围大小防止内存耗尽
	if end-start > 10000 {
		return nil, fmt.Errorf("port range too large (%d ports, max 10000)", end-start+1)
	}

	return makeRange(start, end), nil
}

// makeRange creates a slice [start, start+1, ..., end].
// makeRange 创建切片 [start, start+1, ..., end]。
func makeRange(start, end int) []int {
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// DefaultPorts returns the default port list (MainPorts).
// DefaultPorts 返回默认端口列表（MainPorts）。
func DefaultPorts() []int {
	return MainPorts
}
