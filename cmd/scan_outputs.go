// scan_outputs.go — output path resolution and multi-sink wiring for
// the scan pipeline, split out of scan.go (audit L-7). Everything
// about where results land (date bucketing, timestamp stamping, cwd
// sandboxing) lives here.
//
// scan_outputs.go — 扫描管线的输出路径解析与多 sink 装配，从
// scan.go 拆出（审计 L-7）。结果落点相关的一切（日桶、时间戳、
// cwd 沙箱）都在这里。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

// openOutputSinks opens the multi-format result sink and attaches it
// to sess. Defaults are project-relative for project mode, or current
// directory for ephemeral. `now` is the run-start time captured once
// by runScan so every per-run file (log + result sinks) lands in the
// same daily bucket even across a midnight boundary.
//
// openOutputSinks 打开多格式结果汇并挂到 sess。默认在项目目录下
// （项目模式）或当前目录（即扫即走）。`now` 是 runScan 一次性捕获
// 的 run 起始时间，让本次 run 的所有落盘文件（日志 + 结果 sink）
// 跨午夜时也进同一日桶。
func openOutputSinks(sess *session.Session, cfg *types.Config, now time.Time) error {
	// v0.6.1: validate --alive-format. Empty / unknown values
	// fall back to "txt" (the v0.5.1 default); the help string
	// already lists the three valid choices. / v0.6.1：校验
	// --alive-format。空 / 非法值回退到 "txt"（v0.5.1 的默认）；
	// 帮助字符串已列出三种合法选项。
	switch cfg.AliveFormat {
	case "", "txt", "json", "csv":
		// ok
	default:
		fmt.Fprintf(os.Stderr,
			"warning: --alive-format=%q invalid (want txt|json|csv); falling back to txt\n",
			cfg.AliveFormat)
		cfg.AliveFormat = "txt"
	}

	// `now` comes from runScan (captured once per run) — see the
	// doc comment above. / `now` 来自 runScan（每次 run 捕获一次）
	// —— 见上方 doc 注释。
	// resolveOutputPath may reject user-supplied paths that
	// escape the cwd (Stage 18 / P1#18 / F-05 fix). Fail fast
	// here so we don't half-open some sinks before discovering
	// the rest.
	//
	// resolveOutputPath 可能拒绝跳出 cwd 的用户路径（Stage 18 /
	// P1#18 / F-05 修法）。这里快速失败，避免开了部分 sink 之后
	// 才暴露别的。
	resultTXT, err := resolveOutputPath(cfg, flagOutputTXT, "fgqm_result.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	resultJSON, err := resolveOutputPath(cfg, flagOutputJSON, "fgqm_result.json", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	credsPath, err := resolveOutputPath(cfg, "", "fgqm_creds.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpJSON, err := resolveOutputPath(cfg, "", "fgqm_rdp.json", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpTXT, err := resolveOutputPath(cfg, "", "fgqm_rdp.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	// Alive-host list (one IP per line, deduped). Always on by
	// default — operators pipe it directly into nmap / masscan /
	// curl loops, and there's no harm in writing it (an empty
	// scan produces an empty file). / 存活主机列表（每行一个 IP，
	// 去重）。默认始终开启——操作员直接管道给 nmap / masscan /
	// curl 循环，写个空文件没坏处。
	alivePath, err := resolveOutputPath(cfg, "", "fgqm_alive.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	// CSV is opt-in: only resolve when --output-csv is set. An empty
	// path in OpenOutput disables the sink, so the simplest pattern
	// is to pass the empty string when the flag was not provided.
	//
	// CSV 是 opt-in：只有 --output-csv 设置时才解析。OpenOutput 中
	// 空路径即禁用 sink，所以最简模式是 flag 未提供时传空串。
	var resultCSV string
	if cfg.OutputCSV != "" {
		resultCSV, err = resolveOutputPath(cfg, flagOutputCSV, "fgqm_result.csv", now)
		if err != nil {
			return fmt.Errorf("output path: %w", err)
		}
	}
	// SARIF is opt-in (v0.4): GitHub Code Scanning ingests it natively.
	// / SARIF 是 opt-in（v0.4）：GitHub Code Scanning 原生摄取。
	var resultSARIF string
	if flagOutputSARIF != "" {
		resultSARIF, err = resolveOutputPath(cfg, flagOutputSARIF, "fgqm_result.sarif", now)
		if err != nil {
			return fmt.Errorf("output path: %w", err)
		}
	}
	out, err := output.OpenOutput(output.OutputConfig{
		ResultTXTPath:   resultTXT,
		ResultJSONPath:  resultJSON,
		ResultCSVPath:   resultCSV,
		ResultSARIFPath: resultSARIF,
		CredsPath:       credsPath,
		RotateMaxBytes:  flagOutputRotateBytes,
		RotateMaxFiles:  flagOutputRotateFiles,
		RDPJSONPath:     rdpJSON,
		RDPTXTPath:      rdpTXT,
		ResultAlivePath: alivePath,
		AliveFormat:     flagAliveFormat,
		// P0#2: result.txt gets the redaction gate; creds.txt is
		// always cleartext (operator's working file).
		// P0#2：result.txt 加 redact 门；creds.txt 始终是明文（操作员
		// 工作文件）。
		ShowCleartext: cfg.ShowCleartext,
	})
	if err != nil {
		return fmt.Errorf("output error: %w", err)
	}
	sess.Out = out
	return nil
}

// resolveOutputPath resolves a possibly-empty output path to a default
// inside the project root (project mode) or the ./fgqm_workspace/default/
// directory (ephemeral mode), bucketed by the local-date `now`
// (YYYY-MM-DD) and stamped on the filename with HH-MM-SS so
// multiple runs on the same day don't overwrite each other.
// User-supplied paths via -o / -j bypass the bucketing AND the
// stamp (operators who pass an explicit path want their exact
// path, not an auto-decorated one).
//
// resolveOutputPath 把可能为空的输出路径解析为默认值，按 `now`
// 的本地日期（YYYY-MM-DD）分桶，文件名再加 HH-MM-SS 时间戳：
//   - 项目模式：./fgqm_workspace/projects/<name>/<YYYY-MM-DD>/<file>_<HH-MM-SS>
//   - 即扫即走：./fgqm_workspace/default/<YYYY-MM-DD>/<file>_<HH-MM-SS>
//   - 显式 -o / -j：原样返回（不分桶 + 不加时间戳）
//
// Why bucket by date + stamp by time: an operator who runs
// fg-qimen every day against the same project would otherwise
// see one day's results clobber the previous day's (date bucket
// fixes that), AND two runs on the same day would clobber each
// other (timestamp suffix fixes that). Bucketing by local-date
// gives the operator a per-day audit trail in the same project
// root, the HH-MM-SS stamp gives per-run isolation within a day,
// while fgqm.db (the persistent state / dedup DB) stays at the
// project root and is shared across all runs. / 为什么按日分桶
// + 文件加时间戳：操作员每天对同一项目跑 fg-qimen 时，结果文件
// 会互相覆盖（日桶解决这个），同一天多次跑也会互相覆盖（时
// 间戳后缀解决这个）。按本地日分桶给同项目根保留每日审计轨迹，
// HH-MM-SS 给同日内每次 run 隔离，fgqm.db 保持在项目根跨所有
// run 共享。
func resolveOutputPath(cfg *types.Config, flagValue, defaultName string, now time.Time) (string, error) {
	if flagValue != "" {
		return safeOutputPath(flagValue)
	}
	day := dailyRunSubdir(now)
	stamped := stampFileName(defaultName, now)
	if cfg.Project != "" {
		return filepath.Join(workspace.Root(), "projects", cfg.Project, day, stamped), nil
	}
	return filepath.Join(workspace.Root(), "default", day, stamped), nil
}

// dailyRunSubdir formats `t` as the YYYY-MM-DD bucket name used
// under each project / ephemeral root. The format is intentionally
// fixed-width ISO so directory listings sort chronologically. The
// caller is expected to pass a local-time `t` (we don't pin a TZ
// here — operators reason in local time and the daily bucket name
// should match their calendar day).
//
// dailyRunSubdir 把 `t` 格式化为项目 / 即扫即走根下用的 YYYY-MM-DD
// 桶名。格式固定 ISO 让目录列表按时间排序。调用方传本地时间 `t`
// （不固定 TZ——操作员按本地时区想，日桶名应匹配他们日历日）。
func dailyRunSubdir(t time.Time) string {
	return t.Format("2006-01-02")
}

// stampFileName inserts the run timestamp between the base and
// the extension of `name`. The timestamp is local HH-MM-SS,
// dash-separated (Windows doesn't allow `:` in filenames, and
// the dashes match the YYYY-MM-DD style for visual consistency).
// "fgqm_result.txt" at 14:30:22 → "fgqm_result_14-30-22.txt".
// No extension → suffix appended raw. / stampFileName 在文件名
// 基与扩展名之间插入运行时间戳。本地 HH-MM-SS，连字符分隔
// （Windows 不允许文件名带冒号，连字符与 YYYY-MM-DD 风格一致）。
// 无扩展名则直接追加。
func stampFileName(name string, t time.Time) string {
	ts := t.Format("15-04-05")
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return base + "_" + ts + ext
}

// safeOutputPath sanitizes a user-supplied output path. The
// default behaviour is to confine writes to the current
// working directory; the operator can opt out via env var.
//
// safeOutputPath 安全化用户给的输出路径。默认行为把写入范围
// 限制在当前工作目录；操作员可经环境变量 opt-out。
func safeOutputPath(p string) (string, error) {
	clean := filepath.Clean(p)
	// Make the path absolute relative to cwd. / 把路径解析成相对
	// cwd 的绝对路径。
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("output path %q: %w", p, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("output path %q: getwd: %w", p, err)
	}
	// Containment check: abs must be cwd or under cwd. We use a
	// trailing separator on cwd so /foo/bar doesn't match
	// /foo/barbaz.
	//
	// 包含检查：abs 必须是 cwd 或在 cwd 之下。我们给 cwd 加尾
	// 部分隔符以防 /foo/bar 误匹配 /foo/barbaz。
	cwdWithSep := cwd
	if !strings.HasSuffix(cwdWithSep, string(os.PathSeparator)) {
		cwdWithSep += string(os.PathSeparator)
	}
	if abs != cwd && !strings.HasPrefix(abs, cwdWithSep) {
		// Opt-out: an operator who really needs to write to
		// /var/log or similar can set the env var. The
		// rationale for env-not-flag: the use case is sysadmin
		// overrides, not operator-button-clicks.
		//
		// Opt-out：操作员真要写 /var/log 等可设环境变量。选环
		// 境而非 flag 的理由：这是 sysadmin 覆写，不是操作员
		// 点按钮。
		if os.Getenv("FG_QIMEN_ALLOW_EXTERNAL_OUTPUT") == "1" {
			return abs, nil
		}
		return "", fmt.Errorf(
			"output path %q resolves to %q which is outside the current working directory %q; "+
				"set FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1 to override",
			p, abs, cwd)
	}
	return abs, nil
}

// openRunLogFile opens the per-run log sink: fgqm_log.txt under the
// same daily bucket + HH-MM-SS stamp as the result sinks, so a scan's
// log line archive sits next to its results and same-day runs don't
// clobber each other. Written unbuffered (every log line survives a
// hard os.Exit) and opened 0o600 — CredFound lines carry cleartext
// credentials, so the file gets the same treatment as fgqm_creds.txt.
//
// openRunLogFile 打开本次 run 的日志 sink：fgqm_log.txt 与结果 sink
// 同日桶 + 同 HH-MM-SS 时间戳，让一次扫描的日志归档与其结果放在
// 一起，且同日多次 run 互不覆盖。无缓冲写入（每条日志都能在硬
// os.Exit 下存活），0o600 打开——CredFound 行带明文凭据，与
// fgqm_creds.txt 同等待遇。
func openRunLogFile(cfg *types.Config, now time.Time) (*os.File, string, error) {
	path, err := resolveOutputPath(cfg, "", "fgqm_log.txt", now)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}
