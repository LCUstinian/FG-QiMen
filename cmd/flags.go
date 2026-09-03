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
	flagHTTPFormURL     string
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

	// v0.4: output rotation. / v0.4：输出轮转。
	flagOutputRotateBytes int64 // per-file size cap; 0 = no rotation
	flagOutputRotateFiles int   // total files (active + .1 .2 ...); 0 = no rotation

	// v0.5: scheduled scan. --at (RFC3339 absolute), --in
	// (Go duration), --cron (5-field expr via robfig/cron/v3)
	// are mutually exclusive; --tz sets the IANA zone for cron
	// evaluation; --daemon loops cron forever; --dry-run prints
	// the next fire time and exits. / v0.5：定时扫描。--at
	// (RFC3339 绝对)、--in (Go duration)、--cron (5 字段，robfig
	// /cron/v3) 互斥；--tz 设 cron 求值的 IANA 时区；--daemon
	// 循环跑 cron；--dry-run 打印下次执行时间后退出。
	flagScheduleAt     string
	flagScheduleIn     string
	flagScheduleCron   string
	flagScheduleTZ     string
	flagScheduleDaemon bool
	flagScheduleDryRun bool

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
//
// Short-flag design (v0.5.1):
//
//	**Principle**: short flags must be mnemonic and only exist where
//	they help. Awkward 1- or 2-letter shorts are worse than no short
//	at all — operators have to memorise a non-obvious mapping. We
//	prefer long-form over confusing shorts.
//
//	**Case**: lowercase only, with -H as the single documented
//	uppercase exception (it avoids the -h/--help collision that
//	cobra reserves).
//
//	**Shape**: single-letter shorts for unique concepts (user, pass,
//	threads, host, resume, alive-only, verbose); two-letter shorts
//	for namespaced or paired concepts (output-* 4 files; user/pass-
//	file wordlist pair). Follows nmap's `-oN/-oX/-oG/-oA` precedent
//	for the output namespace.
//
//	**Common pairs that "just work"**:
//	  -H 1.0.0.0/8 -u admin -p root,toor
//	  -H 1.0.0.0/8 -uf users.txt -pf passes.txt
//	  -H ... -f targets.txt -a
//	  -H ... -ot results.txt -oj results.json -oc results.csv
//
//	Dropped shorts (use long form): --project, --mode, --proxy,
//	--output-sarif. Old shorts -p/-M/-X/-o/-j/-U/-W were either
//	workarounds for collision, arbitrary uppercase letters, or
//	collision-prone single letters in a namespace that needed two.
//
//	短参数设计（v0.5.1）：
//	**原则**：短参数必须 mnemonic，存在才有意义。无语义的 1-2 字
//	母短参比没有更糟——操作员得记无显式映射。我们宁用长形式也不
//	要混淆的短参。
//	**大小写**：全小写，仅 -H 是文档化的大写例外（避 -h/--help
//	冲突）。
//	**形态**：单字母给唯一概念（user / pass / threads / host 等）；
//	双字母给命名空间 / 成对概念（output-* 4 文件、user/pass-file
//	wordlist 配对）。output 命名空间沿用 nmap `-oN/-oX/-oG/-oA`
//	先例。
//	**常用搭配**：见上。
//	去掉的短参（用长形式）：--project、--mode、--proxy、
//	--output-sarif。旧 -p/-M/-X/-o/-j/-U/-W 要么是冲突 workaround，
//	要么是随意的随机大写字母，要么是命名空间里易撞车的单字母。
//
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
	// --project has no short flag. Use long form (`--project corp`)
	// to free up -p for --pass (the standard Unix mnemonic for
	// password, matches sshpass / passwd / openssl).
	// / --project 没有短参数。用长形式（--project corp），把 -p
	// 让给 --pass（Unix 标准 mnemonic）。
	pf.StringVar(&flagProject, "project", "",
		"project name (empty = ephemeral oneshot mode)")
	pf.StringVar(&flagProjectKey, "project-key", "",
		"passphrase to encrypt the project DB at rest (AES-256-GCM, Argon2id-derived v0.4+). Falls back to env FG_QIMEN_PROJECT_KEY. Empty = plaintext (v0.2.x compatible).")
	pf.StringVar(&flagMode, "mode", "scan",
		"run mode: scan | crack | linked")
	// -r for --resume: high-frequency operation (resuming an
	// interrupted scan is a daily operator task); mnemonic R.
	pf.BoolVarP(&flagResume, "resume", "r", false,
		"resume from bbolt seen-set (project mode only)")
	pf.BoolVar(&flagNoState, "no-state", false,
		"disable bbolt, use in-memory dedup only")

	// 3. Port selection / 端口选择
	pf.StringVar(&flagPorts, "ports", "",
		"port specification: port groups (web/db/service/common/main), ranges (80-85), or comma-separated (22,80,443). Empty = default 133 ports.")
	pf.StringVar(&flagExcludePorts, "exclude-ports", "",
		"ports to exclude (same format as --ports)")
	pf.BoolVarP(&flagAliveOnly, "alive-only", "a", false,
		"only run host discovery; skip port scan and plugins")

	// 4. Network / 网络
	// --proxy has no short flag. Old -X had no mnemonic link.
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
	pf.DurationVar(&flagTimeout, "timeout", 3*time.Second,
		"per-operation timeout (e.g. 3s, 500ms)")
	pf.DurationVar(&flagShutdownTime, "shutdown-timeout", 5*time.Second,
		"graceful shutdown drain timeout")
	pf.IntVar(&flagMaxPluginWorkers, "max-workers", 16,
		"maximum number of plugin workers (overrides --threads if lower)")

	// 6. Credentials / 凭据
	pf.StringSliceVarP(&flagUser, "user", "u", nil,
		"credential testing usernames (repeatable)")
	// -p for --pass is the Unix-standard mnemonic for password
	// (matches sshpass / passwd / openssl). The previous -P was an
	// arbitrary uppercase workaround to avoid the (now-removed)
	// -p on --project.
	pf.StringSliceVarP(&flagPass, "pass", "p", nil,
		"credential testing passwords (repeatable)")
	// Two-letter shorts -uf / -pf for the wordlist pair: high-
	// frequency combo in real scans, compresses the common
	// `-u user --user-file users.txt -p pass --pass-file passes.txt`
	// into `-u user -uf users.txt -p pass -pf passes.txt` and makes
	// the user/pass symmetry visible at a glance. Note: shorthand
	// is empty ("") because pflag v1.0.9 rejects >1-char shorthands
	// at registration time; the rewrite is done in cmd/multishort.go
	// before cobra sees the args. / 双字母 -uf / -pf 给 wordlist
	// 配对：高频组合……shorthand 为空是因为 pflag 拒绝多字母，注册
	// 时会 panic；重写在 cmd/multishort.go 里做。
	pf.StringVar(&flagUserFile, "user-file", "",
		"usernames dictionary file")
	pf.StringVar(&flagPassFile, "pass-file", "",
		"passwords dictionary file")

	// 7. Output files / 输出文件
	// Two-letter shorts -ot / -oj / -oc for the output namespace,
	// following nmap's `-oN / -oX / -oG / -oA` precedent. -os
	// (SARIF) is intentionally omitted — SARIF is a niche GitHub
	// Code Scanning integration, not worth a short. Note:
	// shorthand is empty ("") because pflag v1.0.9 rejects >1-char
	// shorthands at registration time; the rewrite is done in
	// cmd/multishort.go before cobra sees the args.
	pf.StringVar(&flagOutputTXT, "output-txt", "",
		"path to TXT result file (default: <project>/<YYYY-MM-DD>/fgqm_result.txt or ./runs/default/<YYYY-MM-DD>/fgqm_result.txt — bucketed by local date so daily runs don't clobber each other. The fgqm_ prefix flags the file as fg-qimen's in mixed directories.)")
	pf.StringVar(&flagOutputJSON, "output-json", "",
		"path to NDJSON result file (default: <project>/<YYYY-MM-DD>/fgqm_result.json or ./runs/default/<YYYY-MM-DD>/fgqm_result.json — bucketed by local date so daily runs don't clobber each other. The fgqm_ prefix flags the file as fg-qimen's in mixed directories.)")
	pf.StringVar(&flagOutputCSV, "output-csv", "",
		"path to CSV result file (one row per result; column order stable for awk/pandas). Default: not written. Falls under the same <YYYY-MM-DD>/ bucket as fgqm_result.txt/json unless explicitly overridden.")
	pf.StringVar(&flagOutputSARIF, "output-sarif", "",
		"path to SARIF 2.1.0 JSON file (one document, for GitHub Code Scanning). Default: not written.")
	// v0.4: --output-rotate NMB,N rotates TXT/JSON/CSV/SARIF outputs
	// when the active file crosses NMB megabytes, keeping N
	// total files (active + .1 .2 ...). / v0.4：--output-rotate NMB,N
	// 在现行文件跨过 NMB 兆字节时轮转 TXT/JSON/CSV/SARIF 输出，
	// 保留 N 个文件（active + .1 .2 ...）。
	pf.Int64Var(&flagOutputRotateBytes, "rotate-bytes", 0,
		"per-file size cap in bytes for output rotation (0 = no rotation). Shorthand: v0.4 shortened --output-rotate-bytes → --rotate-bytes (output-* is the only rotate-prefixed flag).")
	pf.IntVar(&flagOutputRotateFiles, "rotate-files", 0,
		"total number of output files to keep (0 = no rotation). Shorthand: v0.4 shortened --output-rotate-files → --rotate-files.")
	pf.StringVar(&flagScheduleAt, "at", "",
		"absolute start time as RFC3339 (e.g. \"2026-12-25T09:00:00+08:00\"). Time zone is embedded in the timestamp. Mutually exclusive with --in and --cron.")
	pf.StringVar(&flagScheduleIn, "in", "",
		"relative delay (Go duration syntax, e.g. \"2h30m\"). Mutually exclusive with --at and --cron.")
	pf.StringVar(&flagScheduleCron, "cron", "",
		"5-field cron expression (minute hour dom month dow). Evaluated in the --tz zone (or system local if --tz is empty). robfig/cron/v3 syntax; --at and --in are mutually exclusive with this. Requires --daemon to loop.")
	pf.StringVar(&flagScheduleTZ, "tz", "",
		"IANA time zone for cron expression evaluation (e.g. \"America/New_York\"). Defaults to the system local zone if empty. Cross-timezone scans are the main use case for this flag.")
	pf.BoolVar(&flagScheduleDaemon, "daemon", false,
		"loop the scan on the cron schedule indefinitely. Only meaningful with --cron. Press Ctrl-C to exit.")
	pf.BoolVar(&flagScheduleDryRun, "schedule-dry-run", false,
		"print the next scheduled fire time and exit without waiting. Useful for verifying --at / --in / --cron / --tz without committing to the wait.")

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
	annotate(pf, []string{"output-txt", "output-json", "output-csv", "output-sarif", "rotate-bytes", "rotate-files"}, groupOutput)
	annotate(pf, []string{"silent", "no-tui", "no-batch", "no-icmp", "verbose", "plugins"}, groupBehavior)
	annotate(pf, []string{"at", "in", "cron", "tz", "daemon", "schedule-dry-run"}, groupSchedule)
	annotate(pf, []string{"show-creds", "insecure-tls", "insecure-ssh", "known-hosts"}, groupSafety)
}
