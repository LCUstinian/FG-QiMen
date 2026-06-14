// Package cmd implements the Cobra command tree for fg-qimen.
// Package cmd 实现 fg-qimen 的 Cobra 命令树。
//
// The command tree:
//
//	fg-qimen (root)
//	├── scan        — default scan (ephemeral or -p <project>)
//	├── resume      — resume a project from bbolt state
//	├── projects    — manage project workspaces
//	│   ├── list
//	│   ├── create
//	│   ├── delete
//	│   └── info
//	└── version     — show version
//
// File layout:
//   - root.go     — rootCmd, persistent flags, Execute() entry point
//   - scan.go     — runScan + helpers (also serves as rootCmd.RunE
//                   and the explicit `scan` subcommand)
//   - resume.go   — `resume` subcommand (alias that forces --resume)
//   - projects.go — `projects {list,create,delete,info}`
//   - version.go  — `version` subcommand
//
// All terminal output (banner, help, log, error) is English-only.
// Comments are bilingual (Chinese + English) for international collaborator
// readability.
//
// 所有终端输出（banner、help、日志、错误）均为纯英文。
// 注释为中英双语，便于国际协作者阅读。
package cmd

import (
	"time"

	"github.com/spf13/cobra"

	// Register all credential authenticators via their init() funcs.
	// 通过 init() 注册所有凭据测试器。
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/database"    // register PG/MySQL/MSSQL/Oracle/MongoDB/ES/Redis/Memcached
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/email"       // register POP3/IMAP
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/filestorage" // register NFS/SMB/Rsync
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/messaging"   // register RabbitMQ
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/network"     // register SNMP/LDAP/Modbus/BACnet/Docker/SOCKS5
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/remote"      // register SSH/FTP/Telnet/VNC/WinRM/IPMI

	// Register all built-in identification plugins via their init() funcs.
	// 通过 init() 注册所有内置识别插件。
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted"
)

// Global flags, populated by Cobra and consumed by buildConfig (in scan.go).
// 全局 flag，由 Cobra 填充，由 scan.go 中的 buildConfig 消费。
var (
	flagHost         string
	flagHostsFile    string
	flagProject      string
	flagMode         string
	flagResume       bool
	flagNoState      bool
	flagPorts        string
	flagExcludePorts string
	flagAliveOnly    bool
	flagThreads      int
	flagTimeout      time.Duration
	flagUser         []string
	flagPass         []string
	flagUserFile     string
	flagPassFile     string
	flagOutputTXT    string
	flagOutputJSON   string
	flagSilent       bool
	flagNoTUI        bool
	flagNoICMP       bool
	flagVerbose      bool
	flagShutdownTime time.Duration
	flagPlugins      string
)

// rootCmd is the top-level fg-qimen command.
// rootCmd 是 fg-qimen 的顶级命令。
var rootCmd = &cobra.Command{
	Use:   "fg-qimen",
	Short: "FG-QiMen — pipeline scanner with project workspaces",
	Long: `FG-QiMen is a CLI scanner that decouples the port scanner (producer)
from the plugin workers (consumer) via a Go channel pipeline. It supports
three run modes (scan / crack / linked) and two work modes (ephemeral
oneshot or persistent project workspace with bbolt state).

Examples / 用例:
  fg-qimen -H 192.168.1.0/24                          # ephemeral scan
  fg-qimen -p corp -H 10.0.0.0/24 -mode linked       # project mode
  fg-qimen -p corp -H 10.0.0.0/24 -resume            # resume
  fg-qimen projects list                              # list projects`,
	SilenceUsage:  true,
	SilenceErrors: false,
	// Default behavior: run a scan (implementation lives in scan.go).
	// 默认行为：执行扫描（实现位于 scan.go）。
	RunE: runScan,
}

// Execute is the entry point invoked by main.go.
// Execute 是 main.go 调用的入口。
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Persistent flags are inherited by every subcommand.
	// 持久化 flag 会被每个子命令继承。
	pf := rootCmd.PersistentFlags()

	pf.StringVarP(&flagHost, "host", "H", "",
		"target IP / CIDR / range / comma-list (e.g. 192.168.1.0/24)")
	pf.StringVarP(&flagHostsFile, "hosts-file", "f", "",
		"load targets from a file (one per line)")
	pf.StringVarP(&flagProject, "project", "p", "",
		"project name (empty = ephemeral oneshot mode)")
	pf.StringVar(&flagMode, "mode", "scan",
		"run mode: scan | crack | linked")
	pf.BoolVarP(&flagResume, "resume", "", false,
		"resume from bbolt seen-set (project mode only)")
	pf.BoolVarP(&flagNoState, "no-state", "", false,
		"disable bbolt, use in-memory dedup only")
	pf.StringVar(&flagPorts, "ports", "22,80,3306,3389,6379,8080",
		"comma-separated port list to scan")
	pf.StringVar(&flagExcludePorts, "exclude-ports", "",
		"comma-separated ports to exclude")
	pf.BoolVarP(&flagAliveOnly, "alive-only", "a", false,
		"only run host discovery; skip port scan and plugins")
	pf.IntVarP(&flagThreads, "threads", "t", 200,
		"concurrent worker count")
	pf.DurationVarP(&flagTimeout, "timeout", "", 3*time.Second,
		"per-operation timeout (e.g. 3s, 500ms)")
	pf.StringSliceVarP(&flagUser, "user", "u", nil,
		"credential testing usernames (repeatable)")
	pf.StringSliceVarP(&flagPass, "pass", "P", nil,
		"credential testing passwords (repeatable)")
	pf.StringVar(&flagUserFile, "user-file", "",
		"usernames dictionary file")
	pf.StringVar(&flagPassFile, "pass-file", "",
		"passwords dictionary file")
	pf.StringVarP(&flagOutputTXT, "output-txt", "o", "",
		"path to TXT result file (default: <project>/result.txt or ./result.txt)")
	pf.StringVarP(&flagOutputJSON, "output-json", "j", "",
		"path to NDJSON result file (default: <project>/result.json or ./result.json)")
	pf.BoolVar(&flagSilent, "silent", false,
		"suppress info log to console; file output still works")
	pf.BoolVar(&flagNoTUI, "no-tui", false,
		"force plain-text mode even when stdout is a TTY")
	pf.BoolVar(&flagNoICMP, "no-icmp", false,
		"skip ICMP probe, use TCP-ping fallback only")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false,
		"verbose debug logging")
	pf.DurationVar(&flagShutdownTime, "shutdown-timeout", 5*time.Second,
		"graceful shutdown drain timeout")
	pf.StringVar(&flagPlugins, "plugins", "",
		"comma-separated plugin names to enable (default: all)")

	// Subcommands are registered from their own files via init().
	// 子命令由各自文件的 init() 注册。
}
