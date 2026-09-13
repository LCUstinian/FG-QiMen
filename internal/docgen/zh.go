// zh.go — Chinese rendering tables for the generated docs (FLAGS.zh-CN.md,
// PLUGINS.zh-CN.md). The English strings live in the pflag usage fields and
// plugin registry; the Chinese equivalents live HERE, keyed by flag long
// name / fixed copy. ValidateFlagDescZh enforces full coverage, and
// TestGeneratedDocsUpToDate pins the rendered artifacts — so adding a flag
// without translating it turns CI red with an explicit list, the same
// forced-sync philosophy as the rest of the docgen guards.
//
// zh.go — 生成文档中文版（FLAGS.zh-CN.md、PLUGINS.zh-CN.md）的渲染表。
// 英文原文存在 pflag usage 字段与插件 registry 里；中文对应物在本文件，
// 以 flag 长名 / 固定文案为键。ValidateFlagDescZh 强制全覆盖，
// TestGeneratedDocsUpToDate 把渲染产物钉死——新增 flag 不配翻译，CI 会
// 带着明确的缺失清单变红，与 docgen 其余守卫同一强制同步哲学。
package docgen

import (
	"fmt"

	"github.com/LCUstinian/FG-QiMen/cmd"
	"github.com/spf13/pflag"
)

// groupZh translates the flag group annotation for the zh artifact.
// / groupZh 为中文产物翻译 flag 分组标注。
var groupZh = map[string]string{
	"Target":      "目标",
	"Workspace":   "工作区",
	"Ports":       "端口",
	"Network":     "网络",
	"Concurrency": "并发",
	"Credentials": "凭据",
	"Output":      "输出",
	"Schedule":    "调度",
	"Behavior":    "行为",
	"Safety":      "安全",
	"Ungrouped":   "未分组",
}

// flagDescZh translates every persistent flag's usage text, keyed by
// long name. Coverage against the live registry is enforced by
// ValidateFlagDescZh; falling back to English at render time is a
// bug-catching safety net, not a feature.
//
// / flagDescZh 翻译全部持久化 flag 的 usage 文本，以长名为键。与活体
// registry 的覆盖关系由 ValidateFlagDescZh 强制；渲染时回退英文是
// 抓 bug 的保险丝，不是功能。
var flagDescZh = map[string]string{
	// Target / 目标
	"exclude-hosts":      "从所有探测中排除的主机（逗号分隔）：精确 IP、CIDR（10.0.0.0/8）、范围（192.168.1.1-192.168.1.9 或 192.168.1.1-9）、主机名，或 RFC1918 快捷写法 192/172/10",
	"exclude-hosts-file": "从文件加载排除条目（每行一条，允许 # 注释；语法同 --exclude-hosts）",
	"host":               "目标 IP / CIDR / 范围 / 逗号列表（如 192.168.1.0/24）",
	"hosts-file":         "从文件加载目标（每行一条）",

	// Workspace / 工作区
	"mode":        "运行模式：scan | crack | linked",
	"no-state":    "禁用 bbolt，仅用内存去重",
	"project":     "项目名（空 = 即扫即走的单次模式）",
	"project-key": "项目 DB 静态加密口令（AES-256-GCM，Argon2id 派生，v0.4+）。可回退读环境变量 FG_QIMEN_PROJECT_KEY。空 = 明文（与 v0.2.x 兼容）。",
	"resume":      "从 bbolt seen-set 恢复（仅项目模式）",
	"workspace":   "工作区根目录（默认 ./fgqm_workspace，也可用环境变量 FGQI_WORKSPACE 覆盖）。用于让扫描产物不落在仓库根。",

	// Ports / 端口
	"alive-only":    "只做主机存活发现；跳过端口扫描与插件",
	"exclude-ports": "排除的端口（格式同 --ports）",
	"ports":         "端口规格：端口组（web/db/service/common/main）、范围（80-85）或逗号分隔（22,80,443）。空 = 默认 133 个端口。",
	"udp":           "TCP 扫描之后额外用 nmap 风格服务 payload 探测常见 UDP 服务（DNS、NetBIOS、SNMP、NTP……）；每个静默端口要等满读超时（约 2s）",
	"udp-strict":    "配合 --udp：静默 UDP 端口报 filtered 并从结果丢弃，而非 open|filtered 噪声；在防火墙网段上用「漏掉空闲但开放服务」的召回换干净输出",
	"no-fp-probes":  "关闭 TCP 主动探针：沉默的开放端口不再发送 nmap 风格探针 payload（hint 探针 / GET / help）引出识别 banner；只做纯被动 banner 抓取",

	// Network / 网络
	"iface":           "绑定的本机网卡 IP（VPN 场景，如 192.168.2.100）",
	"port-timeout":    "端口扫描超时（默认同 --timeout）",
	"proxy":           "HTTP/HTTPS 代理 URL（如 http://127.0.0.1:8080）",
	"socks5":          "SOCKS5 代理地址（如 127.0.0.1:1080 或 socks5://user:pass@host:port）",
	"web-fingerprint": "加载额外 Web 指纹规则库（FG-QiMen 原生 JSON 或 EHole 格式），与内置 FingerprintHub 规则合并（Phase D）。传空串 = 仅用默认内置。",
	"web-timeout":     "Web 探测超时（默认同 --timeout）",

	// Concurrency / 并发
	"max-workers":      "插件 worker 最大数（低于 --threads 时以其为准）",
	"shutdown-timeout": "优雅关停的排水超时",
	"threads":          "并发 worker 数",
	"timeout":          "单次操作超时（如 3s、500ms）",

	// Credentials / 凭据
	"http-form-failure":  "登录失败时响应体中出现的子串（默认 \"invalid\"）。",
	"http-form-fields":   "--http-form-url 的表单字段规格（k1=v1,k2=v2）。$user$ / $pass$ 占位符会被替换。",
	"http-form-redirect": "登录成功时 3xx Location 头中出现的路径子串（如 \"/dashboard\"）。",
	"http-form-success":  "登录成功时响应体中出现的子串（如 \"Welcome\"）。为空则仅用重定向判定。",
	"http-form-url":      "HTTP 表单爆破的目标 URL（如 http://target/login）。空 = 禁用 httpform 测试器。",
	"pass":               "凭据测试的密码列表（可重复传）",
	"pass-file":          "密码字典文件",
	"user":               "凭据测试的用户名列表（可重复传）",
	"user-file":          "用户名字典文件",

	// Output / 输出
	"alive-format": "存活主机列表文件的格式。可选：txt（每行一个主机，默认；便于管道接 `nmap -iL`）、json（NDJSON 每行一个对象：{host,port,service,time}）、csv（CSV 表头 + 每主机一行：host,port,service,time）。",
	"output-csv":   "CSV 结果文件路径（每条结果一行；列序稳定，便于 awk/pandas）。默认不写。未显式覆盖时与 fgqm_result.txt/json 落同一 <YYYY-MM-DD>/ 日桶。",
	"output-json":  "NDJSON 结果文件路径（默认 <project>/<YYYY-MM-DD>/fgqm_result.json 或 ./fgqm_workspace/default/<YYYY-MM-DD>/fgqm_result.json —— 按本地日期分桶，同日多次扫描互不覆盖。fgqm_ 前缀让该文件在混合目录里可识别为 fg-qimen 产物。）",
	"output-sarif": "SARIF 2.1.0 JSON 文件路径（单文档，供 GitHub Code Scanning）。默认不写。",
	"output-txt":   "TXT 结果文件路径（默认 <project>/<YYYY-MM-DD>/fgqm_result.txt 或 ./fgqm_workspace/default/<YYYY-MM-DD>/fgqm_result.txt —— 按本地日期分桶，同日多次扫描互不覆盖。fgqm_ 前缀让该文件在混合目录里可识别为 fg-qimen 产物。）",
	"rotate-bytes": "输出轮转的单文件字节上限（0 = 不轮转）。备注：v0.4 把 --output-rotate-bytes 缩短为 --rotate-bytes（rotate- 前缀的只有 output 这一组）。",
	"rotate-files": "输出轮转保留的文件总数（0 = 不轮转）。备注：v0.4 把 --output-rotate-files 缩短为 --rotate-files。",

	// Schedule / 调度
	"at":               "RFC3339 绝对启动时间（如 \"2026-12-25T09:00:00+08:00\"）。时区内嵌在时间戳里。与 --in、--cron 互斥。",
	"cron":             "5 字段 cron 表达式（分 时 日 月 周）。在 --tz 时区（--tz 为空则系统本地时区）求值。robfig/cron/v3 语法；与 --at、--in 互斥。循环执行需配 --daemon。",
	"daemon":           "按 cron 计划无限循环扫描。仅与 --cron 搭配有意义。Ctrl-C 退出。",
	"in":               "相对延迟（Go duration 语法，如 \"2h30m\"）。与 --at、--cron 互斥。",
	"schedule-dry-run": "打印下一次计划触发时间后立即退出，不等待。用于验证 --at / --in / --cron / --tz 而不必真等。",
	"tz":               "cron 求值用的 IANA 时区（如 \"America/New_York\"）。空 = 系统本地时区。跨时区扫描是本 flag 的主要用例。",

	// Behavior / 行为
	"no-batch":     "禁用 bbolt 批量写；回退为逐条写即 fsync",
	"no-icmp":      "跳过 ICMP 探测，仅用 TCP-ping 兜底",
	"no-prescreen": "禁用 /24 网段预筛（存活发现前跳过网关静默的网段；仅对大规模多 /24 输入生效，单网段输入永不过滤）",
	"no-tui":       "强制纯文本模式，即使 stdout 是 TTY",
	"plugins":      "要启用的插件名（逗号分隔，默认全部）",
	"silent":       "不在控制台输出 info 日志；文件输出不受影响",
	"verbose":      "详细 debug 日志",

	// Safety / 安全
	"insecure-ssh": "禁用 SSH 主机密钥校验（接受任意密钥）。默认是与 v0.2 兼容的不校验（带 stderr 警告）；要真校验请配 --known-hosts。",
	"insecure-tls": "HTTPS 探测禁用 TLS 证书校验（链 + 主机名）。默认开启校验——仅对已知可信的自签名测试环境启用。",
	"known-hosts":  "SSH known_hosts 文件路径，用于主机密钥校验（设 transport.KnownHostsFile；设置后优先于 --insecure-ssh）",
	"show-creds":   "发现的凭据在 TUI、stderr、result.txt、result.json、result.csv 中明文显示（默认只显示长度的脱敏指纹 —— 见 types.RedactUser / types.RedactPassword）。注意：无论此 flag 如何，creds.txt 始终明文——那是操作员的工作文件。",
}

// ValidateFlagDescZh returns an error listing every registered flag that
// has no Chinese translation. gendocs calls it before writing artifacts
// (a missing entry can never ship), and the guard test calls it so a
// translation regression is visible without running gendocs.
//
// / ValidateFlagDescZh 返回列出全部缺失中文翻译的已注册 flag 的错误。
// gendocs 在写产物前调用（缺条目的产物不可能被写出），守卫测试调用它
// 让翻译回归不用跑 gendocs 就可见。
func ValidateFlagDescZh() error {
	fs := cmd.PersistentFlagSet()
	var missing []string
	fs.VisitAll(func(f *pflag.Flag) {
		if _, ok := flagDescZh[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	})
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("flagDescZh missing %d translation(s): %v — add them in internal/docgen/zh.go", len(missing), missing)
}
