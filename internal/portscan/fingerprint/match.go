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

// BytesToRegexSafeString turns a raw byte pattern (e.g. SSH banner
// bytes) into a Go regexp-safe string by escaping non-printable and
// high bytes as \x{NN}. / BytesToRegexSafeString 把原始字节 pattern
// 转为 Go regexp 安全字符串（非可打印/高位字节转 \x{NN}）。
func BytesToRegexSafeString(b []byte) string {
	var result strings.Builder
	for _, c := range b {
		if c < 32 || c >= 128 {
			fmt.Fprintf(&result, "\\x{%02x}", c)
		} else {
			result.WriteByte(c)
		}
	}
	return result.String()
}

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

	patternBytes, err := DecodePattern(pattern)
	if err != nil {
		return m, err
	}
	safe := BytesToRegexSafeString(patternBytes)
	compiled, err := regexp.Compile(safe)
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
