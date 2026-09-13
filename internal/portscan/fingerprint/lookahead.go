// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// RE2-safe rewrite of the nmap "skip header lines" lookaround idiom.
// Fingerprint-identification work, 方向1 (identify depth): the
// measurement harness (golden_census_internal_test.go) found 687
// upstream rules silently dead at compile time because Go's regexp
// engine (RE2 semantics) supports neither PCRE lookaround
// ((?=, (?!, (?<) nor backreferences. 645 of them share ONE exact
// construct — this file revives them with a single mechanical
// translation instead of a per-rule fork of the embedded upstream
// file (the file stays verbatim; attribution stays clean).
//
// nmap「跳过响应头行」前瞻习语的 RE2 安全改写。指纹识别深化（方向1）：
// 度量 harness（golden_census_internal_test.go）发现 687 条上游规则
// 在编译期静默死亡——Go regexp（RE2 语义）不支持 PCRE 前瞻/后顾
// （(?=、(?!、(?<）也不支持反向引用。其中 645 条共享同一个精确构造
// ——本文件用一次机械翻译救活它们，而不是逐条改 fork 嵌入的上游
// 文件（探针文件保持原样，Attribution 干净）。
package fingerprint

import "strings"

const (
	// lookaheadHeaderLoop is the PCRE idiom nmap uses to skip HTTP
	// response-header lines up to (not past) the blank line: one
	// line, then an assertion that the NEXT two bytes are not \r\n.
	// / lookaheadHeaderLoop 是 nmap 跳过 HTTP 响应头行（不越过空行）
	// 的 PCRE 习语：一行，随后断言紧随其后的两字节不是 \r\n。
	lookaheadHeaderLoop = `(?:[^\r\n]*\r\n(?!\r\n))*?`

	// re2HeaderLoop is the RE2-safe equivalent. Upstream itself
	// ships this exact form natively on rules that predate the
	// lookahead variant (e.g. nmap-service-probes.txt line 6995:
	// `^HTTP/1\.1 200 OK\r\n(?:[^\r\n]+\r\n)*?Server: Apache\r\n`),
	// and those compile on Go today — the rewrite only brings the
	// `*`+lookahead form in line with the form upstream already
	// uses. / re2HeaderLoop 是 RE2 安全等价形式。上游自己在部分
	// 规则中原生使用该精确形式（如 nmap-service-probes.txt 6995 行
	// 的 `^HTTP/1\.1 200 OK\r\n(?:[^\r\n]+\r\n)*?Server: Apache\r\n`），
	// 这些规则今天就能在 Go 上编译——本改写只是把 `*`+前瞻 形式
	// 对齐到上游已在使用的形式。
	re2HeaderLoop = `(?:[^\r\n]+\r\n)*?`
)

// rewriteLookaheadHeaderLoop rewrites the header-loop lookaround to
// its RE2-safe equivalent.
//
// Equivalence argument / 等价性论证:
//   - Original: each lazy iteration consumes one header line
//     `[^\r\n]*\r\n` and REFUSES to continue when the next two bytes
//     are \r\n (the guard). The loop therefore stops exactly at the
//     header/body blank line, and the literal following the loop only
//     ever matches inside the header block.
//   - Rewrite: `[^\r\n]+` requires at least one non-CRLF byte per
//     iteration, so a blank line (a bare \r\n) can never be consumed.
//     The lazy loop terminates at the same boundary, and the literal
//     lands in the same places.
//   - Known deviation: the original tolerates an EMPTY line in the
//     middle of a header block when it is not followed by a second
//     blank line; the rewrite does not. Empty lines inside a header
//     block are not valid HTTP — no real banner regresses. Real
//     banner 场景零回归。
//
// Safety: the rewrite triggers ONLY on the exact literal construct.
// Rules that do not contain it are byte-identical after this pass,
// and every rule it does change was previously dead (failed compile),
// so the rewrite can only revive, never alter a working rule.
//
// / rewriteLookaheadHeaderLoop 把头块跳行前瞻构造改写为 RE2 安全
// 等价形式。
//
// 安全性：本改写只对精确字面构造生效；不含该构造的规则逐字节不变，
// 含该构造的规则此前全部编译死亡——改写只可能救活规则，不可能
// 改变任何已工作的规则的行为。
func rewriteLookaheadHeaderLoop(pattern string) string {
	if !strings.Contains(pattern, lookaheadHeaderLoop) {
		return pattern
	}
	return strings.ReplaceAll(pattern, lookaheadHeaderLoop, re2HeaderLoop)
}
