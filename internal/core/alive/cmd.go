// Package alive: System `ping` command probe.
// Package alive: 系统 `ping` 命令探测。
//
// Last-resort fallback when ICMP raw socket is denied (e.g. non-admin
// on Windows). Shells out to the platform's `ping` binary, parses the
// output, and reports liveness based on a successful reply line.
//
// 兜底方案：ICMP raw socket 被拒绝时使用（如 Windows 非 admin）。
// 调系统的 `ping` 二进制，解析输出，根据成功响应行判断存活。
package alive

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// cmdProbe shells out to the system `ping` command. / cmdProbe 调系统 `ping` 命令。
//
// The default timeout (5s) lives in NewSystemPingProbe and is read-
// only from this point on. Task 5 (first-batch fixes): the previous
// implementation read-and-wrote `timeout` from every Probe call,
// which raced across concurrent goroutines sharing one *cmdProbe
// (the audit's P0 data race finding). Now `timeout` is set once at
// construction and Probe only narrows it locally per call, leaving
// the struct field untouched.
//
// cmdProbe 调系统 `ping` 命令。默认 timeout（5s）在 NewSystemPingProbe
// 中设置，之后只读。第一批修复 Task 5：旧实现每次 Probe 调用都读写
// `timeout`，并发 goroutine 共享同一 *cmdProbe 时数据竞争（审计
// P0 发现）。现在 `timeout` 在构造时设一次，Probe 仅按调用本地
// 收窄，不动 struct 字段。
type cmdProbe struct {
	// timeout is the default wall-clock budget for the whole command.
	// Set once at construction (NewSystemPingProbe) and never mutated.
	// Callers may pass a smaller timeout to Probe; we honour the
	// smaller value locally without writing back to this field.
	// / timeout 是整条命令的默认墙钟超时。仅在构造（NewSystemPingProbe）
	// 时设一次，永不修改。调用方可向 Probe 传更小的 timeout；我们仅
	// 本地取较小值，不回写本字段。
	timeout time.Duration
}

// NewSystemPingProbe returns a `ping`-command probe.
// NewSystemPingProbe 返回一个 `ping` 命令探测。
func NewSystemPingProbe() Probe {
	return &cmdProbe{timeout: 5 * time.Second}
}

// Name implements Probe. / Name 实现 Probe。
func (p *cmdProbe) Name() string { return "system" }

// Method implements Probe. / Method 实现 Probe。
func (p *cmdProbe) Method() Method { return MethodSystem }

// Available reports whether the platform `ping` binary is on PATH.
// Available 报告平台 `ping` 二进制是否在 PATH 上。
func (p *cmdProbe) Available() error {
	name := "ping"
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("system-ping: %s not on PATH: %w", name, err)
	}
	return nil
}

// replyRegex matches a single "reply from ..." line in the system
// ping output. Used as a positive liveness signal.
// replyRegex 匹配系统 ping 输出中单条 "reply from ..." 行。
// 作为正面存活信号。
var (
	// Windows: "Reply from 127.0.0.1: bytes=32 time<1ms TTL=128"
	// Linux/macOS: "64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.043 ms"
	replyRegex = regexp.MustCompile(`(?i)(reply from|bytes from)`)
)

// Probe executes `ping -n 1` (Windows) or `ping -c 1` (POSIX) and
// returns a Hit if the output contains a "reply from"/"bytes from" line.
//
// Probe 执行 `ping -n 1`（Windows）或 `ping -c 1`（POSIX），
// 若输出包含 "reply from"/"bytes from" 行则返回 Hit。
func (p *cmdProbe) Probe(ctx context.Context, host string, timeout time.Duration) (Hit, error) {
	// Task 5 (first-batch fixes): compute the effective timeout in a
	// local variable. The previous version wrote the caller's
	// narrowed timeout back to p.timeout, which raced across
	// concurrent goroutines sharing one *cmdProbe (audit P0 race).
	//
	// 第一批修复 Task 5：在局部变量里算 effective timeout。旧版本把
	// 调用方收窄的 timeout 回写到 p.timeout，跨并发 goroutine 共享
	// 同一 *cmdProbe 时数据竞争（审计 P0）。
	effective := p.timeout
	if effective <= 0 {
		effective = 5 * time.Second
	}
	if timeout > 0 && timeout < effective {
		effective = timeout
	}

	var name string
	var args []string
	if runtime.GOOS == "windows" {
		// -n 1: send 1 echo / 发送 1 次
		// -w N: timeout N ms (override the default 4s wait)
		name = "ping"
		args = []string{"-n", "1", "-w", fmt.Sprintf("%d", effective.Milliseconds()), host}
	} else {
		// -c 1: send 1 echo
		// -W N: timeout N seconds for each reply
		name = "ping"
		secs := int(effective.Seconds())
		if secs < 1 {
			secs = 1
		}
		args = []string{"-c", "1", "-W", fmt.Sprintf("%d", secs), host}
	}
	// SECURITY: refuse hosts that look like command-line flags. Otherwise
	// an operator typo (`-r foo`) or a malicious target (e.g. a
	// filename that starts with `-`) would be passed to the system
	// `ping` binary as a flag. POSIX `ping` doesn't support `--` to
	// end-of-options on every platform, so we reject up-front.
	//
	// 安全：拒绝看起来像命令行 flag 的 host。否则操作员笔误
	// （`-r foo`）或恶意 target（如以 `-` 开头的文件名）会被传
	// 给系统 `ping` 当 flag。POSIX `ping` 并非所有平台都支持
	// `--` 终止选项，所以提前拒绝。
	if strings.HasPrefix(host, "-") {
		return Hit{}, fmt.Errorf("%w: host starts with '-' which would be parsed as a ping flag: %q", ErrUnreachable, host)
	}

	cctx, cancel := context.WithTimeout(ctx, effective+1*time.Second)
	defer cancel()

	start := time.Now()
	rawOut, err := exec.CommandContext(cctx, name, args...).CombinedOutput()
	rtt := time.Since(start)

	// Windows `ping` outputs in the system code page (GB18030 for
	// Chinese locales). Decode before regex matching so the debug
	// hint is human-readable and so we don't fail in non-ASCII locales.
	// Windows `ping` 用系统代码页输出（中文环境是 GB18030）。先解码再
	// 做正则匹配，让 debug 提示可读、且非 ASCII locale 下不误判。
	out := rawOut
	if runtime.GOOS == "windows" {
		decoded, decErr := simplifiedchinese.GB18030.NewDecoder().Bytes(rawOut)
		if decErr == nil && len(decoded) > 0 {
			out = decoded
		}
	}
	if err != nil {
		// exec returns error on non-zero exit; check output anyway.
		// exec 在非零退出时返回 error；无论如何检查输出。
		if !replyRegex.Match(out) {
			return Hit{}, ErrUnreachable
		}
	}
	if replyRegex.Match(out) {
		return Hit{
			Host:   host,
			Port:   0,
			Method: MethodSystem,
			RTT:    rtt,
			Time:   time.Now(),
		}, nil
	}
	// Show a hint of the output for debugging when alive check fails.
	// 失败时给出部分输出便于调试。
	short := strings.TrimSpace(string(out))
	if len(short) > 120 {
		short = short[:120] + "..."
	}
	return Hit{}, fmt.Errorf("%w (ping output: %q)", ErrUnreachable, short)
}
