// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Nmap-style service fingerprinting types. See README's attribution
// section for upstream lineage.

// Package fingerprint implements Nmap-style service fingerprinting.
// Package fingerprint 实现 Nmap 风格服务指纹识别。
//
// We embed nmap-service-probes.txt (Nmap Public Source License) and
// parse the Probes / match / softmatch grammar at init time. The
// matching engine then takes a captured banner (or any response
// bytes) and returns the matched service name + version info.
//
// 我们 //go:embed nmap-service-probes.txt（Nmap Public Source License）
// 并在 init 时解析 Probes / match / softmatch 语法。匹配引擎接收捕获
// 到的 banner（或任何响应字节）并返回匹配的服务名 + 版本信息。
package fingerprint

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
	Flag          string
	Delimiter     string
	DirectiveStr  string
}

// Probe is one Nmap-style probe (e.g. "Probe TCP GetRequest q|GET / HTTP/1.0\r\n\r\n|").
// Probe 是一条 Nmap 风格探测（如 "Probe TCP GetRequest q|GET / HTTP/1.0\r\n\r\n|"）。
type Probe struct {
	Name         string   // e.g. "GetRequest"
	Protocol     string   // "tcp" or "udp"
	Data         string   // probe payload (raw string, may contain escapes)
	Ports        string   // default port list
	SSLPorts     string   // default SSL port list
	TotalWaitMS  int      // total wait
	TCPWrappedMS int      // TCP wrap wait
	Rarity       int      // 1..9
	Fallback     string   // fallback probe name
	Matchs       *[]Match // compiled match rules
}

// VScan is the matcher + database. One VScan holds all parsed probes.
// VScan 是匹配器 + 数据库。一个 VScan 持有所有解析好的探针。
type VScan struct {
	AllProbes      []Probe
	Probes         []Probe // TCP probes
	UDPProbes      []Probe
	ProbesMapKName map[string]Probe
	Exclude        string
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
		fmt.Fprintf(probeLogWriter(), "fingerprint: parse error: %v\n", err)
	}
	v.parseProbesToMapKName()
	v.SetUsedProbes()
	return v
}

// BannerMatch is the structured outcome of one banner fingerprint hit.
// For hard matches Product/Version carry the parsed p/.../ v/.../
// segments (with $N substitution applied); for soft fallbacks only
// Service is set — with nmap's "service?" suffix — and Product/Version
// stay empty, matching the long-standing softmatch demotion contract.
// / BannerMatch 是一次 banner 指纹命中的结构化结果。硬匹配时
// Product/Version 携带解析后的 p/.../ v/.../ 分段（已做 $N 替换）；
// soft 兜底只填 Service（带 nmap "service?" 后缀），Product/Version
// 留空——与既有的 softmatch 降级契约一致。
type BannerMatch struct {
	Service string
	Product string
	Version string
	Soft    bool
}

// MatchBanner finds the best service match for a banner (or any
// response bytes) against the TCP probe rules. Iterates all probes'
// match rules; a HARD match returns immediately (authoritative). Soft
// matches are only a fallback: if no hard rule matches anywhere, the
// FIRST soft hit is reported in nmap's convention as "service?" with
// no product/version — a soft regex is a loose protocol hint, not a
// fingerprint.
// / MatchBanner 针对给定 banner（或响应字节）在 TCP probe 规则上找最
// 佳服务匹配。遍历所有 probe 的 match 规则；硬匹配立即返回（可信）。
// softmatch 只作兜底：全库无硬匹配时，首个 soft 命中按 nmap 惯例降
// 级为 "service?" 且不带产品/版本——soft 正则是宽松的协议提示，不
// 是指纹。
func (v *VScan) MatchBanner(banner []byte) (BannerMatch, bool) {
	return matchAll(v.Probes, banner)
}

// MatchUDPBanner is MatchBanner against the UDP probe rules: a UDP
// response captured by a UDP probe must only ever be matched against
// UDP rules — DNS/SNMP/NBTStat responses share no grammar with the
// TCP rule set, and letting TCP loose rules at raw binary responses
// is a false-positive factory.
// / MatchUDPBanner 是针对 UDP probe 规则的 MatchBanner：UDP probe 捕
// 获的响应只能匹配 UDP 规则——DNS/SNMP/NBTStat 响应与 TCP 规则集毫
// 无共同语法，让 TCP 宽松规则去咬原始二进制响应是误报工厂。
func (v *VScan) MatchUDPBanner(banner []byte) (BannerMatch, bool) {
	return matchAll(v.UDPProbes, banner)
}

// matchAll is the shared engine behind MatchBanner/MatchUDPBanner: a
// hard match wins immediately; the first soft hit is only a fallback
// reported as "service?".
// / matchAll 是 MatchBanner/MatchUDPBanner 的公共引擎：硬匹配立即胜
// 出；首个 soft 命中只作兜底，按 "service?" 上报。
func matchAll(probes []Probe, banner []byte) (BannerMatch, bool) {
	if len(banner) == 0 {
		return BannerMatch{}, false
	}
	var softService string
	for _, p := range probes {
		if p.Matchs == nil {
			continue
		}
		for _, m := range *p.Matchs {
			if !m.MatchPattern(banner) {
				continue
			}
			if !m.IsSoft {
				product, version := parseVersionInfo(m.VersionInfo, m.FoundItems)
				return BannerMatch{Service: m.Service, Product: product, Version: version}, true
			}
			if softService == "" {
				softService = m.Service
			}
		}
	}
	if softService != "" {
		return BannerMatch{Service: softService + "?", Soft: true}, true
	}
	return BannerMatch{}, false
}

// UDPHintPorts returns the union of all UDP probes' ports hints — the
// only ports where a UDP probe payload can plausibly elicit a
// response. This is the default --udp target set (~70 ports).
// / UDPHintPorts 返回所有 UDP probe 的 ports 提示并集——只有这些端
// 口上的 UDP probe payload 才可能引出响应。这是 --udp 的默认目标端
// 口集（约 70 个）。
func (v *VScan) UDPHintPorts() map[int]struct{} {
	out := make(map[int]struct{})
	for _, p := range v.UDPProbes {
		for _, port := range ParsePortHint(p.Ports) {
			out[port] = struct{}{}
		}
	}
	return out
}

// UDPPayloadMap builds the port → probe payloads map for the UDP
// service probe: every UDP probe whose ports hint includes the port
// contributes its escape-decoded Data. Multiple probes may target the
// same port (53 has three) — the caller sends all of them on one
// socket. Probes with empty or undecodable payloads are skipped.
// / UDPPayloadMap 为 UDP 服务探针构建 port → probe payload 映射：每
// 个 ports 提示覆盖该端口的 UDP probe 贡献其转义解码后的 Data。多个
// probe 可指向同一端口（53 有三个）——调用方在同一 socket 上全部发
// 出。空 payload 或解码失败的 probe 跳过。
func (v *VScan) UDPPayloadMap() map[int][][]byte {
	out := make(map[int][][]byte)
	for _, p := range v.UDPProbes {
		if p.Data == "" {
			continue
		}
		payload, err := DecodePattern(p.Data)
		if err != nil || len(payload) == 0 {
			continue
		}
		for _, port := range ParsePortHint(p.Ports) {
			out[port] = append(out[port], payload)
		}
	}
	return out
}
