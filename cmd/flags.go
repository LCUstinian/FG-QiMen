// flags.go — global CLI flags for the fg-qimen command tree.
//
// flags.go — fg-qimen 命令树的全局 flag。
//
// All 23 persistent flags live here (not in root.go) so root.go stays
// a pure Cobra-scaffolding file: the command-tree definition lives
// there, and everything flag-related lives here. scan.go and
// resume.go consume the flag values via buildConfig(), which is the
// only function that reads them.
//
// 23 个持久化 flag 都在这里（不在 root.go），让 root.go 保持纯
// Cobra 脚手架形态：命令树定义在 root.go，flag 相关都在这里。scan.go
// 和 resume.go 通过 buildConfig() 消费 flag 值——buildConfig 是唯一
// 读取 flag 的函数。
//
// Flag groups:
//   1. Target selection (host, hosts-file)
//   2. Workspace (project, mode, resume, no-state)
//   3. Port selection (ports, exclude-ports, alive-only)
//   4. Concurrency & timing (threads, timeout, shutdown-timeout)
//   5. Credentials (user, pass, user-file, pass-file)
//   6. Output files (output-txt, output-json)
//   7. Behaviour (silent, no-tui, no-icmp, verbose, plugins)
package cmd

import (
	"time"

	"github.com/spf13/pflag"
)

// Global flag values, populated by Cobra and consumed by buildConfig
// (in scan.go). The split into "var declarations here, registration
// via registerGlobalFlags below" is deliberate: keeping all flag
// plumbing in one file makes adding a new flag a one-file edit.
//
// 全局 flag 值，由 Cobra 填充、由 scan.go 的 buildConfig 消费。
// "变量声明在此、注册在 registerGlobalFlags 下方"是刻意的：把 flag
// 管线集中在一文件里，新增 flag 只需改一个文件。
var (
	// 1. Target selection / 目标选择
	flagHost      string
	flagHostsFile string

	// 2. Workspace / 工作区
	flagProject string
	flagMode    string
	flagResume  bool
	flagNoState bool

	// 3. Port selection / 端口选择
	flagPorts        string
	flagExcludePorts string
	flagAliveOnly    bool

	// 4. Concurrency & timing / 并发与超时
	flagThreads      int
	flagTimeout      time.Duration
	flagShutdownTime time.Duration

	// 5. Credentials / 凭据
	flagUser     []string
	flagPass     []string
	flagUserFile string
	flagPassFile string

	// 6. Output files / 输出文件
	flagOutputTXT  string
	flagOutputJSON string

	// 7. Behaviour / 行为
	flagSilent  bool
	flagNoTUI   bool
	flagNoICMP  bool
	flagVerbose bool
	flagPlugins string
)

// registerGlobalFlags wires all 23 persistent flags into pf (which is
// rootCmd.PersistentFlags()). Called from root.go's init(); kept here
// so root.go stays a Cobra-scaffolding file.
//
// registerGlobalFlags 把 23 个持久化 flag 绑定到 pf（rootCmd 的
// PersistentFlags()）。由 root.go 的 init() 调用；放在这里以保持
// root.go 是纯 Cobra 脚手架文件。
//
// All flags are PersistentFlags so every subcommand (scan, resume,
// projects, version) inherits them. Subcommands read them via the
// same package-level vars above (e.g. flagProject, flagOutputTXT).
//
// 所有 flag 都是 PersistentFlags，确保每个子命令（scan / resume /
// projects / version）继承它们。子命令通过上述包级变量读取
// （如 flagProject、flagOutputTXT）。
func registerGlobalFlags(pf *pflag.FlagSet) {
	// 1. Target selection / 目标选择
	pf.StringVarP(&flagHost, "host", "H", "",
		"target IP / CIDR / range / comma-list (e.g. 192.168.1.0/24)")
	pf.StringVarP(&flagHostsFile, "hosts-file", "f", "",
		"load targets from a file (one per line)")

	// 2. Workspace / 工作区
	pf.StringVarP(&flagProject, "project", "p", "",
		"project name (empty = ephemeral oneshot mode)")
	pf.StringVar(&flagMode, "mode", "scan",
		"run mode: scan | crack | linked")
	pf.BoolVarP(&flagResume, "resume", "", false,
		"resume from bbolt seen-set (project mode only)")
	pf.BoolVarP(&flagNoState, "no-state", "", false,
		"disable bbolt, use in-memory dedup only")

	// 3. Port selection / 端口选择
	pf.StringVar(&flagPorts, "ports", "22,80,3306,3389,6379,8080",
		"comma-separated port list to scan")
	pf.StringVar(&flagExcludePorts, "exclude-ports", "",
		"comma-separated ports to exclude")
	pf.BoolVarP(&flagAliveOnly, "alive-only", "a", false,
		"only run host discovery; skip port scan and plugins")

	// 4. Concurrency & timing / 并发与超时
	pf.IntVarP(&flagThreads, "threads", "t", 200,
		"concurrent worker count")
	pf.DurationVarP(&flagTimeout, "timeout", "", 3*time.Second,
		"per-operation timeout (e.g. 3s, 500ms)")
	pf.DurationVar(&flagShutdownTime, "shutdown-timeout", 5*time.Second,
		"graceful shutdown drain timeout")

	// 5. Credentials / 凭据
	pf.StringSliceVarP(&flagUser, "user", "u", nil,
		"credential testing usernames (repeatable)")
	pf.StringSliceVarP(&flagPass, "pass", "P", nil,
		"credential testing passwords (repeatable)")
	pf.StringVar(&flagUserFile, "user-file", "",
		"usernames dictionary file")
	pf.StringVar(&flagPassFile, "pass-file", "",
		"passwords dictionary file")

	// 6. Output files / 输出文件
	pf.StringVarP(&flagOutputTXT, "output-txt", "o", "",
		"path to TXT result file (default: <project>/result.txt or ./result.txt)")
	pf.StringVarP(&flagOutputJSON, "output-json", "j", "",
		"path to NDJSON result file (default: <project>/result.json or ./result.json)")

	// 7. Behaviour / 行为
	pf.BoolVar(&flagSilent, "silent", false,
		"suppress info log to console; file output still works")
	pf.BoolVar(&flagNoTUI, "no-tui", false,
		"force plain-text mode even when stdout is a TTY")
	pf.BoolVar(&flagNoICMP, "no-icmp", false,
		"skip ICMP probe, use TCP-ping fallback only")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false,
		"verbose debug logging")
	pf.StringVar(&flagPlugins, "plugins", "",
		"comma-separated plugin names to enable (default: all)")
}
