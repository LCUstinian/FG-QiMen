// multishort.go — pre-parse hook that rewrites 2-letter short flags
// (-ot / -oj / -oc / -uf / -pf) to their long form before cobra sees
// them. Necessary because pflag v1.0.9 panics at registration time
// for any shorthand > 1 ASCII char:
//
//	panic: "uf" shorthand is more than one ASCII character
//
// We want mnemonic 2-letter shorts (nmap-style -oN / -oX / -oG / -oA)
// for the output namespace and the user/pass-file wordlist pair, so
// we implement the rewrite here as a thin pre-cobra hook. The map
// is the single source of truth — adding a new 2-letter short means
// one entry here plus a matching StringVarP (or similar) in flags.go
// with shorthand="<long letter>" (a *different* 1-char shorthand
// that won't actually be reachable; we use the long-name letter
// suffix to keep them paired).
//
// multishort.go — 在 cobra 解析前重写 2 字母短参到长形式。pflag v1.0.9
// 在注册时拒绝 >1 字母的 shorthand 会 panic。我们想要 nmap 风格的 2
// 字母短参，所以在这里做薄重写。
//
// Usage: the rewrite is invoked from root.go's Execute() via
// rootCmd.SetArgs(expandMultiCharShorts(os.Args[1:])). Both -X
// value and -X=value forms are handled; long-form --output-txt
// and --output-txt=value pass through unchanged.
package cmd

import "strings"

// multiCharShorts maps our 2-letter short aliases to their long
// flag names. The rewrite is bidirectional in the sense that
// -uf users.txt → --user-file users.txt AND --user-file users.txt
// passes through unchanged; we don't have to special-case downstream.
// / multiCharShorts 把我们的 2 字母短别名映射到长 flag 名。重写是
// 单向的——-uf → --user-file；反过来 --user-file 不动。
var multiCharShorts = map[string]string{
	"ot": "output-txt",
	"oj": "output-json",
	"oc": "output-csv",
	"uf": "user-file",
	"pf": "pass-file",
}

// expandMultiCharShorts rewrites -XY → --long-name and -XY=value →
// --long-name=value for any -XY registered in multiCharShorts.
// Pass-through for anything else (-- flags, single -x flags, bare
// args). -X (single char, len 2) is not a multi-short and is
// passed through so cobra/pflag handle it as before.
//
// Flag-value heuristic: pflag doesn't peek at the syntax of the
// next arg to decide if it's a value — it just consumes the next
// arg as the value of a value-taking flag. So `-p -ot` would
// consume `-ot` as `-p`'s value, NOT treat `-ot` as a separate
// flag. To respect that, we skip the rewrite for any arg whose
// predecessor is flag-shaped (`-x`, `--long`, `--long=val`, `-`,
// `--`). This means a password literally equal to `-ot` (or
// `-uf` / `-pf` / `-oj` / `-oc`) round-trips correctly via
// `--pass=-ot` or any long form.
//
// / expandMultiCharShorts 把 -XY 改写为 --long-name，把 -XY=value
// 改写为 --long-name=value；其它（-- 长形式、单 -x、位置参数）原
// 样透传。
//
// flag-value 启发式：pflag 不看下一个 arg 语法就消费它作为
// value-taking flag 的值。所以 `-p -ot` 会把 `-ot` 当 `-p` 的
// 值，不会当作单独的 flag。为尊重这点，我们对"前一个是 flag
// 形态"的 arg 跳过重写。这意味着字面等于 `-ot`（或 `-uf` /
// `-pf` / `-oj` / `-oc`）的密码通过 `--pass=-ot` 或任何长
// 形式能正确往返。
func expandMultiCharShorts(args []string) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		// Flag-value heuristic: previous arg is flag-shaped →
		// current arg is its value, not a separate short.
		// / flag-value 启发式：上一个是 flag 形态 → 当前是它的
		// 值，不是单独的短参。
		if i > 0 && strings.HasPrefix(args[i-1], "-") {
			out = append(out, a)
			continue
		}
		// Only touch single-dash, multi-char args (--long-name
		// is not a candidate; single -x is pflag's domain).
		// 仅处理单短横 + 多字母的 arg（-- 长形式不是候选；单 -x
		// 留给 pflag）。
		if len(a) < 3 || strings.HasPrefix(a, "--") || a[0] != '-' {
			out = append(out, a)
			continue
		}
		key, val, hasEq := strings.Cut(strings.TrimPrefix(a, "-"), "=")
		if long, ok := multiCharShorts[key]; ok {
			if hasEq {
				out = append(out, "--"+long+"="+val)
			} else {
				out = append(out, "--"+long)
			}
			continue
		}
		out = append(out, a)
	}
	return out
}
