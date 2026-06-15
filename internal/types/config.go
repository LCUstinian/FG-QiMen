// Package types holds shared data types for FG-QiMen.
// Package types 存放 FG-QiMen 共享的数据类型。
//
// All terminal output strings in this package are English-only; comments
// are bilingual (Chinese + English).
//
// 本包的所有终端输出字符串均为纯英文；注释为中英双语。
package types

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
	// ShowCleartext forces credentials to be rendered in cleartext
	// in the UI (stderr / TUI / result.txt). Default is OFF — passwords
	// and usernames are redacted to a length-only fingerprint (see
	// types.RedactUser / types.RedactPassword). Operators who actually
	// need to see the secret (e.g. for manual verification) can pass
	// `--show-creds` once. Note: this flag does NOT change the on-disk
	// creds.txt file (which always contains the cleartext) — only the
	// surface that risks being captured into shared logs.
	//
	// ShowCleartext 强制把凭据以明文渲染在 UI（stderr / TUI / result.txt）。
	// 默认关闭——口令与用户名脱敏为仅含长度的指纹（见
	// types.RedactUser / types.RedactPassword）。确实需要看明文
	//（如人工复核）的操作员可一次性传 `--show-creds`。注意：本 flag 不
	// 影响磁盘上的 creds.txt（那里始终是明文）——只影响可能被共享日志
	// 捕获的界面。
	ShowCleartext bool

	// InsecureTLS disables TLS chain + hostname verification on HTTPS
	// probes. Default is OFF (verify). Set true only for known-trusted
	// self-signed test environments — on a hostile network an attacker
	// with a self-signed cert can MITM the probe and capture the
	// Basic-auth credential. (P1#3)
	//
	// InsecureTLS 禁用 HTTPS 探测的 TLS 链 + 主机名校验。默认开启校验。
	// 仅在已知的自签测试环境开启——在恶意网络上攻击者可用自签证书
	// MITM 探测并捕获 Basic-auth 凭据。（P1#3）
	InsecureTLS bool

	// InsecureSSH disables SSH host-key verification. Default is the
	// v0.2-compatible insecure-ignore with a stderr warning. For real
	// verification, use KnownHostsFile instead — InsecureSSH is the
	// "I know I'm spraying onto a captured network" opt-in. (P1#4)
	//
	// InsecureSSH 禁用 SSH 主机密钥校验。默认是 v0.2 兼容的 insecure-
	// ignore 并附 stderr 警告。要真正校验请用 KnownHostsFile——
	// InsecureSSH 是"我知道自己在往被攻陷的网络喷洒"的 opt-in。
	// （P1#4）
	InsecureSSH bool

	// KnownHostsFile is the path to an SSH known_hosts file. When set
	// (and non-empty) it takes precedence over InsecureSSH — first-time
	// hosts cause a connection error, only previously-acknowledged
	// keys are accepted. (P1#4)
	//
	// KnownHostsFile 是 SSH known_hosts 文件路径。设了（非空）时优先级
	// 高于 InsecureSSH——首次见到的主机导致连接错误，仅接受此前已
	// 确认的 key。（P1#4）
	KnownHostsFile string

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
