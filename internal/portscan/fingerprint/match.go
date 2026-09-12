// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// nmap-service-probes match-engine (regex / softmatch / version
// extraction). See README's attribution section for upstream lineage.
package fingerprint

import (
	"fmt"
	"regexp"
	"strings"
)

// bytesToLatin1String maps each byte 1:1 to the corresponding Latin-1
// Unicode code point so that \x{NN} patterns match the expected byte.
// / bytesToLatin1String 把每个字节 1:1 映射到对应 Latin-1 Unicode 码点，
// 这样 \x{NN} 正则能匹配预期字节。
func bytesToLatin1String(b []byte) string {
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}

// patternToGoRegex translates an nmap-service-probes match pattern
// into a Go-regexp source string, preserving upstream PCRE semantics.
//
// The critical rule: byte-level escapes (\xNN, octal \NNN) must reach
// the regex engine AS ESCAPES, never as decoded raw bytes. The
// pre-fix pipeline decoded \x7c into a raw '|' byte, which turned
// every protocol-field separator inside a pattern into a live regex
// ALTERNATION operator. That split e.g. jrpgt's `^<<jrpgt!>>\x7c$`
// into `^<<jrpgt!>>` | `$` — the bare `$` branch matches ANY banner —
// and is the root cause of the field-observed "arbitrary garbage
// banner matches dps-shell / jrpgt" false positives (garbage-lab
// experiment: jrpgt fired on 2972/3000 pseudo-random banners).
// Go's regexp understands \x{NN} natively, so this translator re-emits
// byte escapes textually and leaves every operator syntax
// ((\d), {26}, ?, *, |, [...]) untouched.
//
// patternToGoRegex 把 nmap-service-probes 的 match pattern 翻译成
// Go regexp 源串，保持上游 PCRE 语义。
//
// 关键规则：字节级转义（\xNN、八进制 \NNN）必须以转义形式到达正则
// 引擎，绝不能解码成裸字节。修复前的管线把 \x7c 解码成裸 '|' 字节，
// 让 pattern 里每个协议字段分隔符都变成正则的"或"分支：jrpgt 的
// `^<<jrpgt!>>\x7c$` 被切成 `^<<jrpgt!>>` | `$`——裸 `$` 分支匹配
// 任意 banner。这就是现场"任意垃圾 banner 误报 dps-shell / jrpgt"
// 的根因（垃圾字节实验：jrpgt 在 3000 个伪随机 banner 上命中
// 2972 次）。Go regexp 原生支持 \x{NN}，本翻译器把字节转义按文本
// 重新输出，所有操作符语法（(\d)、{26}、?、*、|、[...]）原样保留。
func patternToGoRegex(src string) string {
	var b strings.Builder
	b.Grow(len(src) + 8)
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			n := src[i+1]
			switch {
			case n == 'x' && i+3 < len(src) && isValidHex(src[i+2:i+4]):
				// \xNN → \x{NN}: stays a literal byte for the engine.
				// / \xNN → \x{NN}：对引擎保持字面字节。
				b.WriteString(`\x{`)
				b.WriteString(src[i+2 : i+4])
				b.WriteByte('}')
				i += 4
			case n >= '0' && n <= '7':
				// Octal \0..\377 → \x{NN}, same byte semantics as
				// DecodePattern's octal branch. / 八进制 \0..\377 →
				// \x{NN}，与 DecodePattern 八进制分支同字节语义。
				val, j := 0, i+1
				for j < len(src) && j < i+4 && src[j] >= '0' && src[j] <= '7' {
					val = val*8 + int(src[j]-'0')
					j++
				}
				if val <= 255 {
					fmt.Fprintf(&b, `\x{%02x}`, val)
					i = j
				} else {
					// >255: same fallback as DecodePattern — literal
					// backslash, digits become plain chars. / >255：与
					// DecodePattern 相同回退——反斜杠按字面量，数字变
					// 普通字符。
					b.WriteByte('\\')
					i++
				}
			default:
				// \d \w \. \+ \\ \a \f \t \n \r \v ... all pass through
				// verbatim — Go regexp supports every shorthand the
				// probes file uses. / \d \w \. \+ \\ \a \f \t \n \r \v
				// 等全部原样通过——probes 文件用到的简写 Go regexp 全部
				// 支持。
				b.WriteByte('\\')
				b.WriteByte(n)
				i += 2
			}
			continue
		}
		if c < 32 || c >= 127 {
			// Raw control/high bytes (rare in source, e.g. UTF-8
			// literals) map to \x{NN} — consistent with
			// bytesToLatin1String on the response side. / 裸控制/高位
			// 字节（源中罕见，如 UTF-8 字面量）映射为 \x{NN}，与响应侧
			// bytesToLatin1String 一致。
			fmt.Fprintf(&b, `\x{%02x}`, c)
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// versionSubRe matches the $1..$9 submatch references used by
// nmap-service-probes version-info templates. / versionSubRe 匹配
// nmap-service-probes 版本信息模板使用的 $1..$9 子匹配引用。
var versionSubRe = regexp.MustCompile(`\$([1-9])`)

// substituteVersionInfo expands $N references against the pattern's
// captured submatches (Match.FoundItems). Out-of-range references
// expand to the empty string, mirroring nmap's tolerant behaviour.
// / substituteVersionInfo 用 pattern 捕获的子匹配（Match.FoundItems）
// 展开 $N 引用。越界引用展开为空串，与 nmap 的宽容行为一致。
func substituteVersionInfo(s string, subs []string) string {
	if s == "" || !strings.Contains(s, "$") {
		return s
	}
	return versionSubRe.ReplaceAllStringFunc(s, func(ref string) string {
		n := int(ref[1] - '0')
		if n >= 1 && n <= len(subs) {
			return subs[n-1]
		}
		return ""
	})
}

// parseVersionInfo splits the raw version-info template
// (` p/product/ v/version/ i/.../ o/.../ cpe:/.../`) into the product
// and version fields. Each segment is a directive letter followed by a
// delimiter character and the value up to the next unescaped
// delimiter; values may contain spaces (e.g. Debian package versions)
// and backslash-escaped delimiters. Only `p` and `v` are extracted —
// i/o/d/cpe are intentionally ignored (YAGNI).
// / parseVersionInfo 把原始版本信息模板（` p/product/ v/version/
// i/.../ o/.../ cpe:/.../`）拆成 product 和 version 字段。每段 = 指令
// 字母 + 分隔符 + 值（到下一个未转义的分隔符为止）；值可含空格（如
// Debian 包版本）和反斜杠转义的分隔符。只提取 `p` 和 `v`——i/o/d/cpe
// 有意忽略（YAGNI）。
func parseVersionInfo(vi string, subs []string) (product, version string) {
	var productRaw, versionRaw string
	for i := 0; i < len(vi); {
		// Skip the whitespace between segments. / 跳过段间空白。
		for i < len(vi) && vi[i] == ' ' {
			i++
		}
		if i >= len(vi) {
			break
		}
		// Directive name: a single letter, except the multi-char
		// "cpe:". / 指令名：单字母，多字符的 "cpe:" 除外。
		var name string
		if strings.HasPrefix(vi[i:], "cpe:") {
			name, i = "cpe:", i+4
		} else {
			name, i = vi[i:i+1], i+1
		}
		if i >= len(vi) {
			break
		}
		delim := vi[i]
		i++
		var val strings.Builder
		for i < len(vi) {
			c := vi[i]
			if c == '\\' && i+1 < len(vi) {
				// Backslash escapes the next character (including the
				// delimiter). / 反斜杠转义下一字符（含分隔符）。
				val.WriteByte(vi[i+1])
				i += 2
				continue
			}
			if c == delim {
				i++
				break
			}
			val.WriteByte(c)
			i++
		}
		switch name {
		case "p":
			productRaw = val.String()
		case "v":
			versionRaw = val.String()
		}
	}
	product = strings.TrimSpace(substituteVersionInfo(productRaw, subs))
	version = strings.TrimSpace(substituteVersionInfo(versionRaw, subs))
	return product, version
}

// parseMatchDirective is the common implementation for both `match`
// and `softmatch`. / parseMatchDirective 是 `match` 和 `softmatch` 的
// 公共实现。
func (p *Probe) parseMatchDirective(data, prefix string, isSoft bool) (Match, error) {
	m := Match{IsSoft: isSoft}
	matchText := data[len(prefix)+1:]
	d := p.getDirectiveSyntax(matchText)
	parts := strings.Split(d.DirectiveStr, d.Delimiter)
	if len(parts) == 0 {
		return m, fmt.Errorf("fingerprint: invalid %s directive", prefix)
	}
	pattern := parts[0]
	versionInfo := strings.Join(parts[1:], "")

	// versionInfo 格式是 "flags p/product/ v/version/ ..." 跳过 flags
	// / versionInfo 格式是 "flags p/product/ v/version/ ..."；跳过 flags。
	if idx := strings.Index(versionInfo, " "); idx >= 0 {
		versionInfo = versionInfo[idx:]
	}

	// Compile the pattern with escape fidelity intact — see
	// patternToGoRegex for why DecodePattern must NOT be used here
	// (decoding \x7c to a raw '|' byte splits the regex into
	// alternations and lets e.g. jrpgt match any banner).
	// / 用转义保真的方式编译 pattern——见 patternToGoRegex：这里
	// 不能用 DecodePattern（把 \x7c 解码成裸 '|' 会把正则切成多分支，
	// jrpgt 因此能匹配任意 banner）。
	compiled, err := regexp.Compile(patternToGoRegex(pattern))
	if err != nil {
		return m, err
	}

	m.Service = d.DirectiveName
	m.Pattern = pattern
	m.PatternCompiled = compiled
	m.VersionInfo = versionInfo
	return m, nil
}

// getMatch parses a `match` line. / getMatch 解析 `match` 行。
func (p *Probe) getMatch(data string) (Match, error) {
	return p.parseMatchDirective(data, "match", false)
}

// getSoftMatch parses a `softmatch` line. / getSoftMatch 解析 `softmatch` 行。
func (p *Probe) getSoftMatch(data string) (Match, error) {
	return p.parseMatchDirective(data, "softmatch", true)
}

// MatchPattern tests response bytes against the compiled pattern.
// Stores any submatches in m.FoundItems for callers that want to
// extract version info (we don't parse p/v/ in v0.1).
//
// MatchPattern 用编译后的 pattern 测响应字节。子匹配存在
// m.FoundItems 给调用方（v0.1 不解析 p/v/）。
func (m *Match) MatchPattern(response []byte) bool {
	type matcher interface{ MatchString(string) bool }
	re, ok := m.PatternCompiled.(matcher)
	if !ok || re == nil {
		return false
	}
	target := bytesToLatin1String(response)
	if !re.MatchString(target) {
		return false
	}
	// Capture submatches via reflection-free type assertion to the
	// concrete *regexp.Regexp.
	// / 通过非反射类型断言到 *regexp.Regexp 拿子匹配。
	if re2, ok2 := m.PatternCompiled.(*regexp.Regexp); ok2 {
		if subs := re2.FindStringSubmatch(target); len(subs) > 1 {
			m.FoundItems = subs[1:]
		}
	}
	return true
}
