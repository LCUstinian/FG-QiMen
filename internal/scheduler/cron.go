// Package scheduler — scheduled scan support for cross-timezone work.
//
// v0.5: fg-qimen is increasingly used for cross-timezone scans
// (an operator in NYC scheduling a scan to run at 9am Shanghai
// time, for example). The scheduler package centralizes:
//
//   - RFC3339 absolute timestamps with embedded timezone
//   - Relative durations (Go syntax: "2h30m")
//   - 5-field cron expressions evaluated in an IANA timezone
//     (via github.com/robfig/cron/v3 — the de-facto Go cron lib
//     with proper location + DST handling)
//   - Persistence via the project's bbolt (the `schedules` bucket)
//   - Dry-run mode that prints the next fire time without waiting
//
// We previously rolled our own ~150-line cron parser to avoid
// pulling in an external dep. After design review (and on
// explicit user preference) we switched to robfig/cron/v3 — the
// external cost is small (~50 KB binary) and the upside (proven
// edge cases: DST transitions, seconds field, descriptor
// syntax like @daily, @hourly) is worth it. / v0.5：fg-qimen
// 越来越多用于跨时区扫描（如 NYC 操作员调度 9am 上海时间的扫
// 描）。scheduler 包集中：
//   - 带时区的 RFC3339 绝对时间戳
//   - 相对时长（Go 语法："2h30m"）
//   - 在 IANA 时区下求值的 5 字段 cron 表达式（用
//     github.com/robfig/cron/v3——事实标准 Go cron 库，时区
//     与 DST 处理都正确）
//   - 通过项目 bbolt 持久化（`schedules` bucket）
//   - 干跑模式：打印下次执行时间后退出，不实际等
//
// 之前为避免外部依赖手写 ~150 行 cron 解析器。设计复核
// 后（且用户明确偏好）切到 robfig/cron/v3——外部成本小
// （~50KB 二进制）但收益（经过验证的 edge case：DST 转换、
// 秒字段、@daily / @hourly 这种 descriptor 语法）值得。
package scheduler

import (
	"fmt"
	"os"
	"time"

	"github.com/robfig/cron/v3"
)

// Cron wraps a parsed cron expression. We hold the parser (with
// the IANA location applied) so Next() can be called cheaply
// any number of times without re-parsing. / Cron 包了已解析
// cron 表达式。我们持有 parser（已应用 IANA location）让
// Next() 可以被多次廉价调用，无需重 parse。
type Cron struct {
	raw   string
	loc   *time.Location
	sched cron.Schedule
}

// String returns the original expression. / String 返原表达式。
func (c Cron) String() string { return c.raw }

// Location returns the IANA zone the cron is evaluated in. /
// Location 返 cron 求值用的 IANA 时区。
func (c Cron) Location() *time.Location { return c.loc }

// Next returns the next fire time strictly after `from`. /
// Next 返 `from` 之后的下次 fire 时间。
func (c Cron) Next(from time.Time) time.Time {
	// robfig's Next evaluates in the parser's location. Force
	// `from` into our location so the operator's "at 9am Shanghai
	// time" intent is honored even when the system clock is UTC.
	// / robfig 的 Next 在 parser 的 location 下求值。强制
	// `from` 到我们的 location，让操作员"上海 9am"的意图在
	// 系统时钟是 UTC 时也得到遵守。
	return c.sched.Next(from.In(c.loc))
}

// ParseCron parses a 5-field cron expression in the given IANA
// time zone. Returns an error for malformed input. Supports
// the standard cron syntax (5 fields: minute hour dom month
// dow, plus robfig's extensions: @every, @daily, @hourly,
// etc., and the seconds field for 6-field expressions). /
// ParseCron 在给定 IANA 时区下解析 5 字段 cron 表达式。对格式
// 错误返错。支持标准 cron 语法（5 字段：分 时 日 月 周）
// 加 robfig 扩展（@every、@daily、@hourly 等，及 6 字段
// 表达式的秒字段）。
//
// robfig/cron/v3's package-level `standardParser` doesn't
// accept a location option (only the `*Cron` instance does, and
// its `Parse` method returns an Entry, not a Schedule we can
// drive directly). The portable workaround is the `TZ`
// environment variable — the default `time.Local` the parser
// falls back to respects `TZ` via Go's `time.LoadLocation`. We
// set it for the duration of the parse, then restore. /
// robfig/cron/v3 的包级 `standardParser` 不接 location 选
// 项（只有 `*Cron` 实例能接，且它的 `Parse` 方法返 Entry
// 不是 Schedule，不能直接驱动）。可移植的解决办法是 `TZ`
// 环境变量——parser 回退用的默认 `time.Local` 走 Go 的
// `time.LoadLocation` 遵守 `TZ`。我们为 parse 期间设
// TZ，然后恢复。
func ParseCron(raw string, loc *time.Location) (Cron, error) {
	if loc == nil {
		loc = time.Local
	}
	originalTZ, hadTZ := os.LookupEnv("TZ")
	if loc != time.Local && loc != time.UTC {
		_ = os.Setenv("TZ", loc.String())
	} else if loc == time.UTC {
		_ = os.Unsetenv("TZ")
	}
	defer func() {
		if hadTZ {
			_ = os.Setenv("TZ", originalTZ)
		} else {
			_ = os.Unsetenv("TZ")
		}
	}()
	// ParseStandard is the 5-field parser. We use a custom
	// parser with SecondOptional so operators can write
	// `* * * * * *` (every second) for fast tests, while
	// 5-field expressions (the documented form) still work.
	// The seconds field, when present, makes cron tick at
	// sub-minute granularity; for real use, the 5-field form
	// (`0 9 * * *` etc.) is the intended one.
	//
	// / ParseStandard 是 5 字段解析器。我们用带 SecondOptional
	// 的自定义解析器，操作员可写 `* * * * * *`（每秒）跑快
	// 速测试，5 字段（文档形式）仍工作。有 seconds 字段时
	// cron 以 sub-minute 粒度触发；实际使用 5 字段（`0 9
	// * * *` 等）是预期形式。
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour |
			cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	sched, err := parser.Parse(raw)
	if err != nil {
		return Cron{}, fmt.Errorf("cron %q: %w", raw, err)
	}
	return Cron{raw: raw, loc: loc, sched: sched}, nil
}
