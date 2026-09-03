// Package cmd implements the Cobra command tree for fg-qimen.
// Package cmd 实现 fg-qimen 的 Cobra 命令树。
//
// The command tree:
//
//	fg-qimen (root)
//	├── scan        — default scan (ephemeral or --project <name>)
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
//     and the explicit `scan` subcommand)
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
	"os"

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

// Flag group identifiers. Used by both the custom usage template and the
// flag-group annotations below. Keep in sync with FlagGroupIDs() in flags.go.
//
// Flag 分组标识符。同时用于自定义 usage 模板和下方分组标注。
// 与 flags.go 中的 FlagGroupIDs() 保持同步。
const (
	groupTarget      = "Target"
	groupWorkspace   = "Workspace"
	groupPorts       = "Ports"
	groupNetwork     = "Network"
	groupConcurrency = "Concurrency"
	groupCreds       = "Credentials"
	groupOutput      = "Output"
	groupBehavior    = "Behavior"
	groupSafety      = "Safety"
	groupSchedule    = "Schedule"
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
  fg-qimen -H 192.168.1.0/24                            # ephemeral scan
  fg-qimen --project corp -H 10.0.0.0/24 --mode linked  # project mode
  fg-qimen --project corp -H 10.0.0.0/24 -r            # resume
  fg-qimen projects list                                # list projects`,
	SilenceUsage:  true,
	SilenceErrors: false,
	// Default behavior: run a scan (implementation lives in scan.go).
	// 默认行为：执行扫描（实现位于 scan.go）。
	RunE: runScan,
}

// Execute is the entry point invoked by main.go.
// Execute 是 main.go 调用的入口。
func Execute() error {
	// Rewrite 2-letter short flags (-ot / -oj / -oc / -uf / -pf)
	// to their long form before cobra parses. pflag v1.0.9 panics
	// on multi-char shorthands at registration time, so we
	// implement the rewrite here. See cmd/multishort.go for the
	// rewrite logic and the supported alias map.
	// / 把 2 字母短参（-ot / -oj / -oc / -uf / -pf）在 cobra 解析
	// 前重写为长形式。pflag v1.0.9 在注册时拒绝多字符 shorthand
	// 会 panic，所以重写放在这里。详见 cmd/multishort.go。
	rootCmd.SetArgs(expandMultiCharShorts(os.Args[1:]))
	return rootCmd.Execute()
}

func init() {
	// Persistent flags are defined in flags.go and inherited by every
	// subcommand.
	// 持久化 flag 定义在 flags.go，被每个子命令继承。
	registerGlobalFlags(rootCmd.PersistentFlags())

	// Subcommands are registered from their own files via init().
	// 子命令由各自文件的 init() 注册。

	// Custom usage template adds a "Flag Groups" reference section
	// above the default alphabetical flags list. This is opt-in:
	// callers wanting the stock template can call
	// rootCmd.SetUsageTemplate(rootCmd.UsageTemplate()).
	//
	// 自定义 usage 模板在默认字母序 flag 列表之上加了"Flag Groups"
	// 参考小节。这是 opt-in：需要默认模板的调用方可以
	// rootCmd.SetUsageTemplate(rootCmd.UsageTemplate())。
	rootCmd.SetUsageTemplate(usageTemplate)
}

// usageTemplate adds a grouped reference list above cobra's default
// usage block. We render the same flag set twice on purpose: the
// grouped list is the navigation aid, the alphabetical list is the
// canonical one. Operators who only need to find a flag by name still
// have the default lookup; operators who don't know the flag name
// (e.g. "where do I set the proxy?") can scan the groups.
//
// usageTemplate 在 cobra 默认 usage 块之上加了分组参考列表。我们故意
// 渲染两次同一 flag 集：分组列表是导航辅助，字母序列表是规范的。
// 只需按名找 flag 的操作员有默认查找；不知 flag 名（比如"代理在哪
// 设？"）的操作员可扫分组。
const usageTemplate = `Usage:
  {{.UseLine}}

{{.Long}}

Flag groups (alphabetical list below) / 分组参考（下方有字母序列表）:
  Target       -H, -f / --host, --hosts-file
  Workspace    --project, --project-key, --mode, -r / --resume, --no-state
  Ports        --ports, --exclude-ports, -a / --alive-only
  Network      --proxy, --socks5, --iface, --port-timeout, --web-timeout
  Concurrency  -t / --threads, --timeout, --shutdown-timeout, --max-workers
  Credentials  -u / --user, -p / --pass, -uf / --user-file, -pf / --pass-file
  Output       -ot / --output-txt, -oj / --output-json, -oc / --output-csv, --output-sarif
  Behavior     --silent, --no-tui, --no-icmp, -v / --verbose, --plugins
  Safety       --show-creds, --insecure-tls, --insecure-ssh, --known-hosts

{{.Flags.FlagUsages | trimTrailingWhitespaces}}
`
