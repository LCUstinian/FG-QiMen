// helpers.go — webtitle package entry point. The implementation
// is split by concern:
//
//   http.go    — HTTP client setup, redirect following, scheme detect
//   parse.go   — HTML / header / favicon parsing
//   display.go — banner string assembly for the result
//
// Each file is small (~50-80 LOC) and self-contained, so the
// next person who wants to extend any one of them can find the
// relevant code in seconds.
//
// helpers.go — webtitle 包入口。实现按关注点拆分：
//
//   http.go    — HTTP 客户端搭建、重定向跟随、协议探测
//   parse.go   — HTML / 头 / favicon 解析
//   display.go — 结果的 banner 串构造
//
// 各文件都小（~50-80 LOC）且独立，下一个想扩展任一块的人能在几秒
// 内找到相关代码。
package webtitle

import "regexp"

// (All functions moved to http.go / parse.go / display.go as part
// of the v0.2.1 god-file refactor.)

// Regexes used by extractTitle in parse.go. They live at the
// package scope (not inside extractTitle) so the compiler can
// pre-compile them once at package init. Same for whitespaceRegex.
//
// extractTitle 用的正则，迁到 parse.go 了。留在包级（而非函数
// 局部）让编译器在包 init 时一次性预编译。whitespaceRegex 同理。
var (
	titleRegex      = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	whitespaceRegex = regexp.MustCompile(`\s+`)
)
