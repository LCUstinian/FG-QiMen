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
//   - root.go     — rootCmd, Execute() entry point
//   - scan.go     — runScan + helpers (also serves as rootCmd.RunE
//                   and the explicit `scan` subcommand)
//   - resume.go   — `resume` subcommand (alias that forces --resume)
//   - projects.go — `projects {list,create,delete,info}`
//   - version.go  — `version` subcommand
//   - flags.go    — global flag vars + registerGlobalFlags helper
//
// All terminal output (banner, help, log, error) is English-only.
// Comments are bilingual (Chinese + English) for international collaborator
// readability.
//
// 所有终端输出（banner、help、日志、错误）均为纯英文。
// 注释为中英双语，便于国际协作者阅读。
package cmd

import (
	"github.com/spf13/cobra"

	// Register all credential authenticators via their init() funcs.
	// 通过 init() 注册所有凭据测试器。
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/database"    // register PG/MySQL/MSSQL/Oracle/MongoDB/ES/Redis/Memcached
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/email"       // register POP3/IMAP
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/filestorage" // register NFS/SMB/Rsync
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/messaging"   // register RabbitMQ
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/network"     // register SNMP/LDAP/Modbus/BACnet/Docker/SOCKS5
	_ "github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/remote"      // register SSH/FTP/Telnet/VNC/WinRM/IPMI

	// Register LAN-only host discovery probes (ARP + NetBIOS) into
	// alive.DefaultOptions(). Omitting this import would yield an
	// internet-only scan (ICMP + TCP + system-ping only).
	// 注册 LAN-only 主机发现 probe（ARP + NetBIOS）到 alive.DefaultOptions()。
	// 不 import 则得到仅互联网扫描（仅 ICMP + TCP + system-ping）。
	_ "github.com/LCUstinian/FG-QiMen/internal/discovery"

	// Register all built-in identification plugins via their init() funcs.
	// 通过 init() 注册所有内置识别插件。
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted"
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
	// Persistent flags (23 of them) are defined in flags.go and
	// inherited by every subcommand.
	// 持久化 flag（共 23 个）定义在 flags.go，被每个子命令继承。
	registerGlobalFlags(rootCmd.PersistentFlags())

	// Subcommands are registered from their own files via init().
	// 子命令由各自文件的 init() 注册。
}
