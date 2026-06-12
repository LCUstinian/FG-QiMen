// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Nmap-style service fingerprinting types. See README's attribution
// section for upstream lineage.

// Package portfinger implements Nmap-style service fingerprinting.
// Package portfinger 实现 Nmap 风格服务指纹识别。
//
// We embed nmap-service-probes.txt (Nmap Public Source License) and
// parse the Probes / match / softmatch grammar at init time. The
// matching engine then takes a captured banner (or any response
// bytes) and returns the matched service name + version info.
//
// 我们 //go:embed nmap-service-probes.txt（Nmap Public Source License）
// 并在 init 时解析 Probes / match / softmatch 语法。匹配引擎接收捕获
// 到的 banner（或任何响应字节）并返回匹配的服务名 + 版本信息。
package portfinger

import "fmt"

// Match is a single match (or softmatch) rule within a Probe.
// Match 是 Probe 内的一条 match（或 softmatch）规则。
type Match struct {
	Service         string // product name (e.g. "OpenSSH")
	Pattern         string // original pattern (after hex/octal decode)
	PatternCompiled interface{ MatchString(string) bool }
	VersionInfo     string // p/vendor/ v/version/ o/... (raw, not parsed in v0.1)
	IsSoft          bool
	FoundItems      []string
}

// Directive is a parsed `name flag delimiter rest` line in a Probe.
// Directive 是 Probe 内一行 `name flag delimiter rest` 解析结果。
type Directive struct {
	DirectiveName string
	Flag         string
	Delimiter    string
	DirectiveStr string
}

// Probe is one Nmap-style probe (e.g. "Probe TCP GetRequest q|GET / HTTP/1.0\r\n\r\n|").
// Probe 是一条 Nmap 风格探测（如 "Probe TCP GetRequest q|GET / HTTP/1.0\r\n\r\n|"）。
type Probe struct {
	Name         string  // e.g. "GetRequest"
	Protocol     string  // "tcp" or "udp"
	Data         string  // probe payload (raw string, may contain escapes)
	Ports        string  // default port list
	SSLPorts     string  // default SSL port list
	TotalWaitMS  int     // total wait
	TCPWrappedMS int     // TCP wrap wait
	Rarity       int     // 1..9
	Fallback     string  // fallback probe name
	Matchs       *[]Match // compiled match rules
}

// VScan is the matcher + database. One VScan holds all parsed probes.
// VScan 是匹配器 + 数据库。一个 VScan 持有所有解析好的探针。
type VScan struct {
	AllProbes     []Probe
	Probes        []Probe // TCP probes
	UDPProbes     []Probe
	ProbesMapKName map[string]Probe
	Exclude       string
}

// NewVScan loads and parses the embedded probe database.
// NewVScan 加载并解析 embedded 探针数据库。
func NewVScan() *VScan {
	v := &VScan{
		ProbesMapKName: map[string]Probe{},
	}
	if err := v.parseProbesFromContent(embeddedProbes); err != nil {
		// Logged via the simple stderr writer; v0.1 doesn't fail
		// startup on parse errors. / v0.1 解析失败不阻止启动。
		fmt.Fprintf(probeLogWriter(), "portfinger: parse error: %v\n", err)
	}
	v.parseProbesToMapKName()
	v.SetUsedProbes()
	return v
}

// MatchBanner finds the best service match for a banner (or any
// response bytes). Iterates all probes' match rules, returns the
// first hit (probe order follows the upstream "rarity" order).
// / MatchBanner 为给定 banner（或响应字节）找最佳服务匹配。遍历所有
// probe 的 match 规则，返回首个命中（probe 顺序按上游 rarity 排）。
func (v *VScan) MatchBanner(banner []byte) (service, versionInfo string, found bool) {
	if len(banner) == 0 {
		return "", "", false
	}
	for _, p := range v.Probes {
		if p.Matchs == nil {
			continue
		}
		for _, m := range *p.Matchs {
			if m.MatchPattern(banner) {
				return m.Service, m.VersionInfo, true
			}
		}
	}
	return "", "", false
}
