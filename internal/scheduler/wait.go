// wait.go — wait logic + schedule resolution for cmd/scan.
//
// v0.5: support for cross-timezone scheduled scans. Three input
// modes are mutually exclusive; the resolver picks one:
//
//   - --at  RFC3339  absolute time, time zone embedded in the
//     timestamp. The wait terminates at that exact instant in the
//     stamp's own time zone (which is just `t` after parsing).
//   - --in  DURATION  relative delay; --tz sets the local time
//     zone the user is reasoning about, but the actual wait
//     duration is just `time.Duration`. Useful for "wait 2h30m".
//   - --cron EXPR     recurring; requires --daemon. Next fire is
//     computed via CronSpec.NextAfter in the --tz zone. After each
//     run completes we re-compute and loop.
//
// All three modes also support --dry-run which prints the next
// fire time and exits without waiting.
//
// v0.5：跨时区调度扫描支持。三种输入互斥；resolver 选一种：
//   - --at  RFC3339  绝对时间，时区嵌入时间戳。wait 终止于时间
//     戳自带时区的精确瞬时（解析后就是 `t` 本身）。
//   - --in  DURATION  相对延迟；--tz 设用户参考的本地时区，但
//     实际等待时长就是 `time.Duration`。适合 "等 2h30m"。
//   - --cron EXPR     循环；需 --daemon。下次 fire 走 CronSpec
//     .NextAfter 在 --tz 时区。每次跑完重算循环。
//
// 三种都支持 --dry-run 打印下次执行时间后退出，不实际等。

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Input is the parsed schedule request. / Input 是解析后的调度
// 请求。
type Input struct {
	// At is the resolved absolute time, in any time zone. / At
	// 是任意时区下的解析后绝对时间。
	At time.Time
	// In is the relative delay. Mutually exclusive with At and
	// Cron. / In 是相对延迟。与 At / Cron 互斥。
	In time.Duration
	// Cron is the parsed cron spec. / Cron 是解析后的 cron
	// 表达式。
	Cron Cron
	// Mode tags which of the three fields above is set.
	// / Mode 标记上面三字段中哪个被设置。
	Mode Mode
	// TZ is the IANA time zone the user is reasoning in.
	// Affects the display + the cron-evaluation zone. / TZ 是
	// 用户参考的 IANA 时区。影响显示 + cron 求值时区。
	TZ *time.Location
	// Base is the reference time used to compute the next
	// fire. Captured at Resolve time so NextFire() is
	// deterministic for tests + dry-run; the Wait() loop
	// refreshes this with the live clock between iterations.
	// / Base 是算下次 fire 的参考时间。Resolve 时捕获，让
	// NextFire() 对测试 + 干跑是确定的；Wait() 循环在两轮之
	// 间用实时时钟刷新。
	Base time.Time
	// Daemon, if true with Cron, loops the scan indefinitely.
	// / Daemon 与 Cron 一起用时，循环跑 scan 不退出。
	Daemon bool
	// DryRun, if true, prints the next fire time and exits
	// without waiting. / DryRun 为 true 时打印下次执行时间后
	// 退出，不实际等。
	DryRun bool
	// Output is the sink for status / countdown lines. Defaults
	// to os.Stderr if nil. / Output 是状态 / 倒计时的写入点。
	// nil 时默认 os.Stderr。
	Output io.Writer
}

// Mode identifies which of the three input forms the user picked.
// / Mode 标识用户选的三种输入形式。
type Mode int

const (
	// ModeNone means no schedule was specified. / ModeNone 表
	// 无调度。
	ModeNone Mode = iota
	// ModeAt is a one-shot RFC3339 time. / ModeAt 是一次性
	// RFC3339 时间。
	ModeAt
	// ModeIn is a relative delay. / ModeIn 是相对延迟。
	ModeIn
	// ModeCron is a cron expression. / ModeCron 是 cron 表达式。
	ModeCron
)

// String returns the human-readable name. / String 返可读名。
func (m Mode) String() string {
	switch m {
	case ModeAt:
		return "at"
	case ModeIn:
		return "in"
	case ModeCron:
		return "cron"
	default:
		return "none"
	}
}

// Options collects the raw inputs from CLI flags. / Options
// 收 CLI flag 的原始输入。
type Options struct {
	At     string
	In     string
	Cron   string
	TZ     string
	Daemon bool
	DryRun bool
	Now    time.Time // injectable for tests
}

// ErrInvalidCombination is returned when conflicting inputs are
// provided (e.g. both --at and --in). / ErrInvalidCombination
// 是输入冲突时返的错误（如同时给 --at 和 --in）。
var ErrInvalidCombination = errors.New("scheduler: invalid flag combination")

// Resolve parses the Options and returns a validated Input.
// Exactly one of At, In, Cron must be set (unless all are
// empty, in which case Mode is ModeNone and the caller skips
// waiting). Daemon requires Cron. / Resolve 解析 Options 返
// 已校验的 Input。At / In / Cron 必须恰好一个（除非全空——
// 那种情况 Mode=ModeNone，调用方跳过等待）。Daemon 需 Cron。
func Resolve(opts Options) (*Input, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	// TZ load. / 加载 TZ。
	var tz *time.Location
	if opts.TZ != "" {
		l, err := time.LoadLocation(opts.TZ)
		if err != nil {
			return nil, fmt.Errorf("scheduler: --tz %q: %w", opts.TZ, err)
		}
		tz = l
	} else {
		tz = time.Local
	}
	// Count set modes. / 数已设置的 mode。
	modes := 0
	if opts.At != "" {
		modes++
	}
	if opts.In != "" {
		modes++
	}
	if opts.Cron != "" {
		modes++
	}
	if modes > 1 {
		return nil, fmt.Errorf("%w: --at, --in, and --cron are mutually exclusive", ErrInvalidCombination)
	}
	if opts.Daemon && opts.Cron == "" {
		return nil, fmt.Errorf("%w: --daemon requires --cron", ErrInvalidCombination)
	}
	in := &Input{TZ: tz, Base: now, Daemon: opts.Daemon, DryRun: opts.DryRun}
	switch {
	case opts.At != "":
		t, err := time.Parse(time.RFC3339, opts.At)
		if err != nil {
			return nil, fmt.Errorf("scheduler: --at %q: %w", opts.At, err)
		}
		if !t.After(now) {
			return nil, fmt.Errorf("scheduler: --at %s is in the past (now %s)", t, now)
		}
		in.At = t
		in.Mode = ModeAt
	case opts.In != "":
		d, err := time.ParseDuration(opts.In)
		if err != nil {
			return nil, fmt.Errorf("scheduler: --in %q: %w", opts.In, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("scheduler: --in %q: must be positive", opts.In)
		}
		in.In = d
		in.Mode = ModeIn
	case opts.Cron != "":
		spec, err := ParseCron(opts.Cron, tz)
		if err != nil {
			return nil, fmt.Errorf("scheduler: --cron %q: %w", opts.Cron, err)
		}
		in.Cron = spec
		in.Mode = ModeCron
	default:
		in.Mode = ModeNone
	}
	return in, nil
}

// NextFire returns the absolute time the schedule should run
// next, in the schedule's time zone. / NextFire 返回下次执行
// 时间的绝对值，在调度时区中。
func (in *Input) NextFire() time.Time {
	base := in.Base
	if base.IsZero() {
		base = time.Now()
	}
	base = base.In(in.TZ)
	switch in.Mode {
	case ModeAt:
		return in.At.In(in.TZ)
	case ModeIn:
		return base.Add(in.In)
	case ModeCron:
		// The cron library evaluates in `base`'s zone. Force
		// `base` into the right zone so the result is in the
		// operator's expected frame. / cron 库在 `base` 的时区
		// 下求值。强制 `base` 到对的时区，让结果在操作员期望的
		// 框架内。
		return in.Cron.Next(base)
	default:
		return time.Time{}
	}
}

// Wait blocks until the next fire time (or returns immediately
// if no schedule is set), printing a one-line-per-second
// countdown on in.Output. Returns when:
//   - the target time is reached (nil error), or
//   - ctx is cancelled (ctx.Err()), or
//
// - the schedule is ModeNone (nil error, no wait).
//
// Wait 阻塞到下次执行时间（或无调度时立即返回），每秒在
// in.Output 打一行倒计时。返回条件：
//   - 到目标时间（nil error）
//   - ctx 取消（ctx.Err()）
//   - ModeNone（nil error，不等）
//
// defaultOutput returns the io.Writer used when Input.Output
// is nil. The "tee" form below mirrors the project's standard
// human channel (stderr) so countdown lines don't pollute the
// result.txt / result.json output streams. / defaultOutput
// 在 Input.Output 为 nil 时返默认写入点。下方的 "tee" 形式镜
// 像项目标准的人类通道（stderr），让倒计时行不污染 result.txt
// / result.json 输出流。
func defaultOutput() io.Writer { return os.Stderr }

func (in *Input) Wait(ctx context.Context) error {
	if in.Mode == ModeNone {
		return nil
	}
	if in.Output == nil {
		in.Output = defaultOutput()
	}
	for {
		target := in.NextFire()
		d := time.Until(target)
		if d <= 0 {
			return nil
		}
		// Tick: print a countdown, then check again. We sleep
		// in 1-second chunks so ctx cancellation is responsive
		// within ~1s. / 滴答：打一行倒计时，再查。我们在 1
		// 秒块里 sleep，让 ctx 取消在 ~1s 内响应。
		fmt.Fprintf(in.Output,
			"  schedule: next run at %s (%s); in %s\n",
			target.Format(time.RFC3339),
			in.TZ.String(),
			d.Truncate(time.Second),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(target)):
			return nil
		case <-time.After(time.Second):
			// Loop, re-print countdown. / 循环重打印倒计时。
			// Refresh base to the live clock so the next
			// iteration computes the right next-fire (matters
			// when the wait was long enough to cross into
			// a new cron period). / 刷新 base 为实时时钟，让
			// 下次迭代算对 next-fire（等得久跨过新 cron 周
			// 期时重要）。
			in.Base = time.Now()
		}
	}
}

// IsCronDaemon reports whether this is a cron+daemon schedule
// (the loop is the caller's responsibility after each run).
// / IsCronDaemon 报告是否是 cron+daemon 调度（循环是调用方
// 在每次跑完后的责任）。
func (in *Input) IsCronDaemon() bool { return in.Mode == ModeCron && in.Daemon }
