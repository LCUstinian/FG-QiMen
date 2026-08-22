// flags.go — global CLI flags for the fg-qimen command tree.
//
// flags.go — fg-qimen 命令树的全局 flag。
//
// All persistent flags live here (not in root.go) so root.go stays
// a pure Cobra-scaffolding file: the command-tree definition lives
// there, and everything flag-related lives here. scan.go and
// resume.go consume the flag values via buildConfig(), which is the
// only function that reads them.
//
// 持久化 flag 都在这里（不在 root.go），让 root.go 保持纯 Cobra
// 脚手架形态：命令树定义在 root.go，flag 相关都在这里。scan.go 和
// resume.go 通过 buildConfig() 消费 flag 值——buildConfig 是唯一
// 读取 flag 的函数。
//
// Flag groups (v0.3.0, rendered as section headers in --help):
//  1. Target    — host, hosts-file
//  2. Workspace — project, project-key, mode, resume, no-state
//  3. Ports     — ports, exclude-ports, alive-only
//  4. Network   — proxy, socks5, iface, port-timeout, web-timeout
//  5. Concurrency — threads, timeout, shutdown-timeout, max-workers
//  6. Credentials — user, pass, user-file, pass-file
//  7. Output    — output-txt, output-json, output-csv
//  8. Behavior  — silent, no-tui, no-icmp, verbose, plugins
//  9. Safety    — show-creds, insecure-tls, insecure-ssh, known-hosts
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
	flagProject    string
	flagProjectKey string
	flagMode       string
	flagResume     bool
	flagNoState    bool

	// 3. Port selection / 端口选择
	flagPorts        string
	flagExcludePorts string
	flagAliveOnly    bool

	// 4. Network / 网络
	flagProxy          string
	flagSocks5         string
	flagIface          string
	flagPortTimeout    time.Duration
	flagWebTimeout     time.Duration
	flagWebFingerprint string

	// HTTP form brute (opt-in; default empty = no-op). / HTTP form 爆破
	// （opt-in；默认空 = no-op）。
	flagHTTPFormURL      string
	flagHTTPFormFields  string
	flagHTTPFormSuccess string
	flagHTTPFormFailure string
	flagHTTPFormRedir   string

	// 5. Concurrency & timing / 并发与超时
	flagThreads          int
	flagTimeout          time.Duration
	flagShutdownTime     time.Duration
	flagMaxPluginWorkers int

	// 6. Credentials / 凭据
	flagUser     []string
	flagPass     []string
	flagUserFile string
	flagPassFile string

	// 7. Output files / 输出文件
	flagOutputTXT   string
	flagOutputJSON  string
	flagOutputCSV   string
	flagOutputSARIF string // v0.4: SARIF for GitHub Code Scanning

	// 8. Behaviour / 行为
	flagSilent        bool
	flagNoTUI         bool
	flagNoICMP        bool
	flagNoBatch       bool
	flagVerbose       bool
	flagShowCleartext bool
	flagInsecureTLS   bool
	flagInsecureSSH   bool
	flagKnownHosts    string
	flagPlugins       string
)

// registerGlobalFlags wires all persistent flags into pf (which is
// rootCmd.PersistentFlags()). Called from root.go's init(); kept here
// so root.go stays a Cobra-scaffolding file.
//
// registerGlobalFlags 把所有持久化 flag 绑定到 pf（rootCmd 的
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
// annotate flags with their group for --help output. Cobra renders
// annotations["cobra_annotation_group_name"] as section headers.
//
// 用 pflag.Annotations 标记分组，cobra 会用 "group" 注解渲染
// --help 中的分组小节。
func annotate(pf *pflag.FlagSet, names []string, group string) {
	for _, n := range names {
		f := pf.Lookup(n)
		if f == nil {
			continue
		}
		if f.Annotations == nil {
			f.Annotations = map[string][]string{}
		}
		f.Annotations["group"] = []string{group}
	}
}

func registerGlobalFlags(pf *pflag.FlagSet) {
	// 1. Target selection / 目标选择
	pf.StringVarP(&flagHost, "host", "H", "",
		"target IP / CIDR / range / comma-list (e.g. 192.168.1.0/24)")
	pf.StringVarP(&flagHostsFile, "hosts-file", "f", "",
		"load targets from a file (one per line)")

	// 2. Workspace / 工作区
	pf.StringVarP(&flagProject, "project", "p", "",
		"project name (empty = ephemeral oneshot mode)")
	pf.StringVar(&flagProjectKey, "project-key", "",
		"passphrase to encrypt the project DB at rest (AES-256-GCM, Argon2id-derived v0.4+). Falls back to env FG_QIMEN_PROJECT_KEY. Empty = plaintext (v0.2.x compatible).")
	pf.StringVar(&flagMode, "mode", "scan",
		"run mode: scan | crack | linked")
	pf.BoolVarP(&flagResume, "resume", "", false,
		"resume from bbolt seen-set (project mode only)")
	pf.BoolVarP(&flagNoState, "no-state", "", false,
		"disable bbolt, use in-memory dedup only")

	// 3. Port selection / 端口选择
	pf.StringVar(&flagPorts, "ports", "",
		"port specification: port groups (web/db/service/common/main), ranges (80-85), or comma-separated (22,80,443). Empty = default 133 ports.")
	pf.StringVar(&flagExcludePorts, "exclude-ports", "",
		"ports to exclude (same format as --ports)")
	pf.BoolVarP(&flagAliveOnly, "alive-only", "a", false,
		"only run host discovery; skip port scan and plugins")

	// 4. Network / 网络
	pf.StringVar(&flagProxy, "proxy", "",
		"HTTP/HTTPS proxy URL (e.g. http://127.0.0.1:8080)")
	pf.StringVar(&flagSocks5, "socks5", "",
		"SOCKS5 proxy address (e.g. 127.0.0.1:1080 or socks5://user:pass@host:port)")
	pf.StringVar(&flagIface, "iface", "",
		"local interface IP to bind (for VPN scenarios, e.g. 192.168.2.100)")
	pf.DurationVar(&flagPortTimeout, "port-timeout", 0,
		"port scan timeout (default: same as --timeout)")
	pf.DurationVar(&flagWebTimeout, "web-timeout", 0,
		"web probe timeout (default: same as --timeout)")
	pf.StringVar(&flagWebFingerprint, "web-fingerprint", "",
		"load additional web-fingerprint ruleset (FG-QiMen native JSON or EHole format); merged with the built-in FingerprintHub rules (Phase D). Pass an empty string for the default built-ins only.")

	// 5. Concurrency & timing / 并发与超时
	pf.IntVarP(&flagThreads, "threads", "t", 200,
		"concurrent worker count")
	pf.DurationVarP(&flagTimeout, "timeout", "", 3*time.Second,
		"per-operation timeout (e.g. 3s, 500ms)")
	pf.DurationVar(&flagShutdownTime, "shutdown-timeout", 5*time.Second,
		"graceful shutdown drain timeout")
	pf.IntVar(&flagMaxPluginWorkers, "max-workers", 16,
		"maximum number of plugin workers (overrides --threads if lower)")

	// 6. Credentials / 凭据
	pf.StringSliceVarP(&flagUser, "user", "u", nil,
		"credential testing usernames (repeatable)")
	pf.StringSliceVarP(&flagPass, "pass", "P", nil,
		"credential testing passwords (repeatable)")
	pf.StringVar(&flagUserFile, "user-file", "",
		"usernames dictionary file")
	pf.StringVar(&flagPassFile, "pass-file", "",
		"passwords dictionary file")

	// 7. Output files / 输出文件
	pf.StringVarP(&flagOutputTXT, "output-txt", "o", "",
		"path to TXT result file (default: <project>/result.txt or ./result.txt)")
	pf.StringVarP(&flagOutputJSON, "output-json", "j", "",
		"path to NDJSON result file (default: <project>/result.json or ./result.json)")
	pf.StringVar(&flagOutputCSV, "output-csv", "",
		"path to CSV result file (one row per result; column order stable for awk/pandas). Default: not written.")
	pf.StringVar(&flagOutputSARIF, "output-sarif", "",
		"path to SARIF 2.1.0 JSON file (one document, for GitHub Code Scanning). Default: not written.")

	// HTTP form brute (opt-in; --http-form-url empty = no-op).
	// / HTTP form 爆破（opt-in；--http-form-url 空 = no-op）。
	pf.StringVar(&flagHTTPFormURL, "http-form-url", "",
		"target URL for HTTP form brute (e.g. http://target/login). Empty disables the httpform authenticator.")
	pf.StringVar(&flagHTTPFormFields, "http-form-fields", "user=$user$,pass=$pass$",
		"form fields spec for --http-form-url (k1=v1,k2=v2). $user$ / $pass$ placeholders are substituted.")
	pf.StringVar(&flagHTTPFormSuccess, "http-form-success", "",
		"substring present in the response body on successful login (e.g. \"Welcome\"). If empty, only redirect-based detection is used.")
	pf.StringVar(&flagHTTPFormFailure, "http-form-failure", "invalid",
		"substring present in the response body on failed login (default \"invalid\").")
	pf.StringVar(&flagHTTPFormRedir, "http-form-redirect", "",
		"path substring present in 3xx Location header on successful login (e.g. \"/dashboard\").")

	// 8. Behavior / 行为
	pf.BoolVar(&flagSilent, "silent", false,
		"suppress info log to console; file output still works")
	pf.BoolVar(&flagNoTUI, "no-tui", false,
		"force plain-text mode even when stdout is a TTY")
	pf.BoolVar(&flagNoBatch, "no-batch", false,
		"disable bbolt batched writes; fall back to per-write fsync")
	pf.BoolVar(&flagNoICMP, "no-icmp", false,
		"skip ICMP probe, use TCP-ping fallback only")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false,
		"verbose debug logging")
	pf.StringVar(&flagPlugins, "plugins", "",
		"comma-separated plugin names to enable (default: all)")

	// 9. Safety / 安全
	pf.BoolVar(&flagShowCleartext, "show-creds", false,
		"render discovered credentials in cleartext on TUI, stderr, result.txt, result.json, and result.csv (default: redacted to length-only fingerprint — see types.RedactUser / types.RedactPassword). NOTE: creds.txt is ALWAYS cleartext regardless of this flag — that's the operator's working file.")
	pf.BoolVar(&flagInsecureTLS, "insecure-tls", false,
		"disable TLS certificate verification (chain + hostname) on HTTPS probes (P1#3). Default verifies — opt in only for known-trusted self-signed test environments.")
	pf.BoolVar(&flagInsecureSSH, "insecure-ssh", false,
		"disable SSH host-key verification (accept any key) (P1#4). Default is v0.2-compatible insecure-ignore with a stderr warning; use -o KnownHostsFile=<path> for real verification.")
	pf.StringVar(&flagKnownHosts, "known-hosts", "",
		"path to SSH known_hosts file for host-key verification (sets transport.KnownHostsFile; takes precedence over --insecure-ssh when set)")

	// Group annotations (rendered by cobra's "group" annotation key
	// when SetUsageTemplate is configured in root.go). This is a
	// single source of truth — keep the flag name list aligned with
	// the StringVarP/Var calls above.
	//
	// 分组标注（root.go 的 SetUsageTemplate 用 "group" 注解渲染）。
	// 这是单一真源——flag 名列表要与上面的 StringVarP/Var 调用对齐。
	annotate(pf, []string{"host", "hosts-file"}, groupTarget)
	annotate(pf, []string{"project", "project-key", "mode", "resume", "no-state"}, groupWorkspace)
	annotate(pf, []string{"ports", "exclude-ports", "alive-only"}, groupPorts)
	annotate(pf, []string{"proxy", "socks5", "iface", "port-timeout", "web-timeout", "web-fingerprint"}, groupNetwork)
	annotate(pf, []string{"threads", "timeout", "shutdown-timeout", "max-workers"}, groupConcurrency)
	annotate(pf, []string{"user", "pass", "user-file", "pass-file",
		"http-form-url", "http-form-fields", "http-form-success", "http-form-failure", "http-form-redirect"}, groupCreds)
	annotate(pf, []string{"output-txt", "output-json", "output-csv", "output-sarif"}, groupOutput)
	annotate(pf, []string{"silent", "no-tui", "no-batch", "no-icmp", "verbose", "plugins"}, groupBehavior)
	annotate(pf, []string{"show-creds", "insecure-tls", "insecure-ssh", "known-hosts"}, groupSafety)
}
