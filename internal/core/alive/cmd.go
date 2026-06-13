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
type cmdProbe struct {
	// timeout is the wall-clock budget for the whole command. We pass
	// a count=1 ping so the command self-terminates after one echo.
	// timeout 是整条命令的墙钟超时。我们传 count=1 让命令在一次 echo
	// 后自行结束。
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
	var name string
	if runtime.GOOS == "windows" {
		name = "ping"
	} else {
		name = "ping"
	}
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
	if p.timeout <= 0 {
		p.timeout = 5 * time.Second
	}
	if timeout > 0 && timeout < p.timeout {
		p.timeout = timeout
	}

	var name string
	var args []string
	if runtime.GOOS == "windows" {
		// -n 1: send 1 echo / 发送 1 次
		// -w N: timeout N ms (override the default 4s wait)
		name = "ping"
		args = []string{"-n", "1", "-w", fmt.Sprintf("%d", p.timeout.Milliseconds()), host}
	} else {
		// -c 1: send 1 echo
		// -W N: timeout N seconds for each reply
		name = "ping"
		secs := int(p.timeout.Seconds())
		if secs < 1 {
			secs = 1
		}
		args = []string{"-c", "1", "-W", fmt.Sprintf("%d", secs), host}
	}

	cctx, cancel := context.WithTimeout(ctx, p.timeout+1*time.Second)
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
