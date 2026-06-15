// display.go — banner string assembly for the webtitle plugin's
// Identify result. The banner is a single line that goes into
// result.txt / the TUI events column / the JSON title field.
//
// buildBanner is the only function here; the file is small
// enough to keep as-is but large enough to deserve its own name
// so the next maintainer doesn't grep helpers.go for "buildBanner".
//
// display.go — webtitle 插件 Identify 结果的 banner 串构造。banner
// 是单行，进 result.txt / TUI events 列 / JSON title 字段。
//
// buildBanner 是这里唯一的函数；文件虽小但值得独立成块，让下一
// 个维护者不用在 helpers.go 里 grep "buildBanner"。
package webtitle

import (
	"fmt"
	"strings"
)

// buildBanner assembles the single-line banner from the structured
// fields. Layout: "<url> [<status>/<len> | title=... | server=...
// | fps=...]" — brackets and pipes chosen to be grep-friendly
// (operators can pipe the result.txt through `grep | foo=`).
//
// buildBanner 从结构化字段拼单行 banner。布局："<url> [<status>/
// <len> | title=... | server=... | fps=...]"——方括号和管道选成
// grep 友好（操作员可 `grep | foo=` 跑 result.txt）。
func buildBanner(displayURL string, status, length int, title, server string, fps []string) string {
	var b strings.Builder
	b.WriteString(displayURL)
	b.WriteString(" [")
	fmt.Fprintf(&b, "%d/%d", status, length)
	b.WriteString(" | title=")
	b.WriteString(title)
	b.WriteString(" | server=")
	b.WriteString(server)
	if len(fps) > 0 {
		b.WriteString(" | fps=")
		b.WriteString(strings.Join(fps, ","))
	}
	b.WriteString("]")
	return b.String()
}
