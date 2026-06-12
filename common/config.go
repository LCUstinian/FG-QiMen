// Package common holds shared types and helpers for FG-QiMen.
// Package common 存放 FG-QiMen 共享的类型与辅助函数。
//
// All terminal output strings in this package are English-only; comments
// are bilingual (Chinese + English).
//
// 本包的所有终端输出字符串均为纯英文；注释为中英双语。
package common

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// RunMode selects the pipeline execution strategy.
// RunMode 选择管线执行策略。
type RunMode string

const (
	// ModeScan runs only the port scanner + Identify plugins.
	// ModeScan 仅跑端口扫描 + Identify 插件。
	ModeScan RunMode = "scan"
	// ModeCrack skips port scanning and runs only Credential plugins
	// against ports already known (loaded from project DB or supplied
	// via -ports).
	// ModeCrack 跳过端口扫描，对已知端口（从项目 DB 加载或 -ports 提供）
	// 仅跑 Credential 插件。
	ModeCrack RunMode = "crack"
	// ModeLinked runs scan first, then triggers Credential on services
	// that declared ModeCredential.
	// ModeLinked 先跑 scan，对声明 ModeCredential 的服务再触发凭据测试。
	ModeLinked RunMode = "linked"
)

// Config is the immutable, fully-validated configuration for a single
// fg-qimen invocation. It is built from CLI flags by cmd.BuildConfig.
//
// Config 是单次 fg-qimen 调用的不可变、已校验配置。由 cmd.BuildConfig 从 CLI flag
// 构造。
type Config struct {
	// Target selection / 目标选择
	Host      string // IP / CIDR / range / comma-list
	HostsFile string // optional file of targets

	// Workspace / 工作区
	Project string // empty = ephemeral; non-empty = persistent project
	Mode    RunMode
	Resume  bool
	NoState bool // disable bbolt; in-memory dedup only

	// Port selection / 端口选择
	Ports        string
	ExcludePorts string

	// Behavior / 行为
	AliveOnly bool
	Threads   int
	Timeout   time.Duration
	NoICMP    bool
	Plugins   string // comma-separated plugin names; empty = all

	// Credentials / 凭据
	Users    []string
	Passes   []string
	UserFile string
	PassFile string

	// Output / 输出
	OutputTXT  string
	OutputJSON string

	// UI / 界面
	Silent  bool
	NoTUI   bool
	Verbose bool

	// Lifecycle / 生命周期
	ShutdownTimeout time.Duration
}

// Validate checks the Config for required fields and mutually-exclusive
// constraints. Returns nil on success.
//
// Validate 校验 Config 的必填字段和互斥约束。成功时返回 nil。
func (c *Config) Validate() error {
	if c.Threads <= 0 {
		return errors.New("threads must be > 0")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be > 0")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown-timeout must be > 0")
	}
	switch c.Mode {
	case ModeScan, ModeCrack, ModeLinked:
		// ok
	case "":
		// default to scan
		c.Mode = ModeScan
	default:
		return fmt.Errorf("invalid mode %q (expected scan|crack|linked)", c.Mode)
	}
	if c.Project == "" && c.Resume {
		return errors.New("-resume requires -p <project>")
	}
	if c.Host == "" && c.HostsFile == "" {
		// Empty is allowed for subcommands like `projects list`.
		// 子命令（如 `projects list`）允许为空。
	}
	return nil
}

// ParsePorts parses the comma-separated Ports / ExcludePorts strings
// into int slices. Empty input returns nil.
//
// ParsePorts 把逗号分隔的 Ports / ExcludePorts 字符串解析为 int 切片。
// 空输入返回 nil。
func (c *Config) ParsePorts() ([]int, error) {
	return parsePortList(c.Ports)
}

func (c *Config) ParseExcludePorts() ([]int, error) {
	return parsePortList(c.ExcludePorts)
}

// parsePortList is a small helper shared by ParsePorts / ParseExcludePorts.
// parsePortList 是 ParsePorts / ParseExcludePorts 共享的小工具。
func parsePortList(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", p, err)
		}
		if n < 1 || n > 65535 {
			return nil, fmt.Errorf("port out of range: %d", n)
		}
		out = append(out, n)
	}
	return out, nil
}
