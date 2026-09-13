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
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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

Examples:
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

	// Management subcommands opt back into the stock cobra template:
	// the inherited 60+ scan flags drown `projects --help` / `version
	// --help`. Capture the stock template BEFORE replacing root's —
	// rootCmd.UsageTemplate() returns the custom one once set.
	// / 管理子命令回退 cobra 原生模板：继承的 60+ 扫描 flags 会
	// 淹没 `projects --help` / `version --help`。必须在替换 root
	// 模板之前取原生模板——rootCmd.UsageTemplate() 一旦设置返回
	// 的是自定义版。
	stock := rootCmd.UsageTemplate()
	projectsCmd.SetUsageTemplate(stock)
	versionCmd.SetUsageTemplate(stock)
	rootCmd.SetUsageTemplate(buildUsageTemplate())

	return rootCmd.Execute()
}

func init() {
	// Persistent flags are defined in flags.go and inherited by every
	// subcommand.
	// 持久化 flag 定义在 flags.go，被每个子命令继承。
	registerGlobalFlags(rootCmd.PersistentFlags())

	// Subcommands are registered from their own files via init().
	// 子命令由各自文件的 init() 注册。
	//
	// Usage templates are composed in Execute() (buildUsageTemplate +
	// stock-template opt-out for management subcommands) — all init()
	// funcs have run by then, so command registration order across
	// files does not matter.
	// / usage 模板在 Execute() 中组装（buildUsageTemplate + 管理子
	// 命令回退原生模板）——届时所有 init() 已跑完，跨文件注册顺序
	// 无关紧要。
}

// usageTemplateStr is the shape of the custom usage template. The
// group summary (%s) is filled at build time by buildUsageTemplate.
// NOTE: no {{.Long}} here — cobra's help template already prints
// Long above the usage block; duplicating it here made every help
// screen render the description twice.
//
// usageTemplateStr 是自定义 usage 模板的形状。分组摘要（%s）由
// buildUsageTemplate 在构建期填入。注意：这里不放 {{.Long}}——
// cobra 的 help 模板已经在 usage 块之上打印过 Long；在这里重复
// 导致所有 help 页面把描述打印两遍。
const usageTemplateStr = `Usage:
  {{.UseLine}}
{{if .HasAvailableSubCommands}}
Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}
{{end}}
Flag groups (full alphabetical list below):
%s

{{.Flags.FlagUsages | trimTrailingWhitespaces}}
`

// buildUsageTemplate returns the custom usage template with the group
// summary rendered from the live flag annotations. Must run AFTER
// registerGlobalFlags (it reads the annotations annotate() wrote).
// / buildUsageTemplate 返回填好分组摘要的自定义 usage 模板，摘要
// 从活的 flag 注解渲染。必须在 registerGlobalFlags 之后运行
// （要读 annotate() 写入的注解）。
func buildUsageTemplate() string {
	return fmt.Sprintf(usageTemplateStr, flagGroupSummary(rootCmd.PersistentFlags()))
}

// flagGroupSummary renders the grouped flag reference from the
// "group" annotations written by flags.go. Groups with no annotated
// flags are skipped; flags within a group come out in alphabetical
// order (pflag sorts by default). Rendering lives here so adding a
// flag + its annotate() line is all it takes to appear in --help.
// / flagGroupSummary 按 flags.go 写入的 "group" 注解渲染分组参考。
// 无标注 flag 的组跳过；组内 flag 按字母序（pflag 默认排序）。
// 渲染放这里，新增 flag 只需加 annotate() 一行就会出现在 --help。
func flagGroupSummary(pf *pflag.FlagSet) string {
	order := []string{
		groupTarget, groupWorkspace, groupPorts, groupNetwork,
		groupConcurrency, groupCreds, groupOutput, groupBehavior,
		groupSchedule, groupSafety,
	}
	byGroup := make(map[string][]string, len(order))
	pf.VisitAll(func(f *pflag.Flag) {
		g, ok := f.Annotations["group"]
		if !ok || len(g) == 0 {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + " / " + name
		}
		byGroup[g[0]] = append(byGroup[g[0]], name)
	})
	var b strings.Builder
	for _, g := range order {
		names := byGroup[g]
		if len(names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %-12s %s\n", g, strings.Join(names, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
