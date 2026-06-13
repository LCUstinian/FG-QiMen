// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// nmap-service-probes parser. See README's attribution section for
// upstream lineage.
package fingerprint

import (
	"fmt"
	"strconv"
	"strings"
)

// getDirectiveSyntax parses `name flag delimiter rest` (e.g. `q|` or
// `m|...|`) into a Directive. / getDirectiveSyntax 把 `name flag delimiter rest`
// （如 `q|` 或 `m|...|`）解析为 Directive。
func (p *Probe) getDirectiveSyntax(data string) Directive {
	d := Directive{}
	blank := strings.Index(data, " ")
	if blank < 0 {
		return d
	}
	d.DirectiveName = data[:blank]
	d.Flag = data[blank+1 : blank+2]
	d.Delimiter = data[blank+2 : blank+3]
	d.DirectiveStr = data[blank+3:]
	return d
}

// parseProbeInfo parses the first line of a Probe block. / parseProbeInfo
// 解析一个 Probe 块的第一行。
func (p *Probe) parseProbeInfo(probeStr string) error {
	if len(probeStr) < 4 {
		return fmt.Errorf("fingerprint: probe line too short: %q", probeStr)
	}
	proto := probeStr[:4]
	if proto != "TCP " && proto != "UDP " {
		return fmt.Errorf("fingerprint: invalid protocol %q", proto)
	}
	other := probeStr[4:]
	if other == "" {
		return fmt.Errorf("fingerprint: empty probe name")
	}
	d := p.getDirectiveSyntax(other)
	p.Name = d.DirectiveName
	p.Data = strings.Split(d.DirectiveStr, d.Delimiter)[0]
	p.Protocol = strings.ToLower(strings.TrimSpace(proto))
	return nil
}

// fromString parses one full Probe block (a "Probe" directive plus
// its match / softmatch / ports / etc. follow-ups).
//
// fromString 解析一个完整 Probe 块（一条 "Probe" 指令 + 它的 match /
// softmatch / ports / 等后续行）。
func (p *Probe) fromString(data string) error {
	data = strings.TrimSpace(data)
	lines := strings.Split(data, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("fingerprint: empty block")
	}
	if err := p.parseProbeInfo(lines[0]); err != nil {
		return err
	}
	var matches []Match
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "match "):
			m, err := p.getMatch(line)
			if err != nil {
				continue
			}
			matches = append(matches, m)
		case strings.HasPrefix(line, "softmatch "):
			m, err := p.getSoftMatch(line)
			if err != nil {
				continue
			}
			matches = append(matches, m)
		case strings.HasPrefix(line, "ports "):
			p.parsePorts(line)
		case strings.HasPrefix(line, "sslports "):
			p.parseSSLPorts(line)
		case strings.HasPrefix(line, "totalwaitms "):
			p.parseTotalWaitMS(line)
		case strings.HasPrefix(line, "tcpwrappedms "):
			p.parseTCPWrappedMS(line)
		case strings.HasPrefix(line, "rarity "):
			p.parseRarity(line)
		case strings.HasPrefix(line, "fallback "):
			p.parseFallback(line)
		}
	}
	p.Matchs = &matches
	return nil
}

func (p *Probe) parsePorts(line string)     { p.Ports = line[len("ports")+1:] }
func (p *Probe) parseSSLPorts(line string)  { p.SSLPorts = line[len("sslports")+1:] }
func (p *Probe) parseTotalWaitMS(line string) {
	v, err := strconv.Atoi(strings.TrimSpace(line[len("totalwaitms")+1:]))
	if err == nil {
		p.TotalWaitMS = v
	}
}
func (p *Probe) parseTCPWrappedMS(line string) {
	v, err := strconv.Atoi(strings.TrimSpace(line[len("tcpwrappedms")+1:]))
	if err == nil {
		p.TCPWrappedMS = v
	}
}
func (p *Probe) parseRarity(line string) {
	v, err := strconv.Atoi(strings.TrimSpace(line[len("rarity")+1:]))
	if err == nil {
		p.Rarity = v
	}
}
func (p *Probe) parseFallback(line string) { p.Fallback = line[len("fallback")+1:] }

// parseProbesFromContent parses the whole nmap-service-probes.txt
// content into Probes. / parseProbesFromContent 把整个 nmap-service-probes.txt
// 内容解析为 Probes。
func (v *VScan) parseProbesFromContent(content string) error {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return fmt.Errorf("fingerprint: empty probe file")
	}

	excludeCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "Exclude ") {
			excludeCount++
		}
		if excludeCount > 1 {
			return fmt.Errorf("fingerprint: duplicate Exclude directive")
		}
	}

	first := lines[0]
	if !strings.HasPrefix(first, "Exclude ") && !strings.HasPrefix(first, "Probe ") {
		return fmt.Errorf("fingerprint: first line must be Exclude or Probe")
	}

	if excludeCount == 1 {
		v.Exclude = first[len("Exclude")+1:]
		lines = lines[1:]
	}

	content = "\n" + strings.Join(lines, "\n")
	parts := strings.Split(content, "\nProbe")[1:]

	var probes []Probe
	for _, p := range parts {
		probe := Probe{}
		if err := probe.fromString(p); err != nil {
			continue
		}
		probes = append(probes, probe)
	}
	v.AllProbes = probes
	return nil
}

// parseProbesToMapKName builds a name → Probe lookup. / parseProbesToMapKName
// 构造 name → Probe 查询。
func (v *VScan) parseProbesToMapKName() {
	v.ProbesMapKName = make(map[string]Probe, len(v.AllProbes))
	for _, p := range v.AllProbes {
		v.ProbesMapKName[p.Name] = p
	}
}

// SetUsedProbes splits probes into TCP / UDP buckets (the upstream
// behavior). v0.1: we don't actively send UDP probes; the UDP list
// is kept for completeness but is unused by the scanner.
//
// SetUsedProbes 把探针分成 TCP / UDP 两组。v0.1 不主动发 UDP 探针；
// UDP 列表保留但扫描器不使用。
func (v *VScan) SetUsedProbes() {
	for _, p := range v.AllProbes {
		if strings.ToLower(p.Protocol) == "tcp" {
			if p.Name == "SSLSessionReq" {
				continue
			}
			v.Probes = append(v.Probes, p)
			// Special: when we add TLSSessionReq, also add
			// SSLSessionReq. / 特殊：加 TLSSessionReq 时同时加
			// SSLSessionReq。
			if p.Name == "TLSSessionReq" {
				if ssl, ok := v.ProbesMapKName["SSLSessionReq"]; ok {
					v.Probes = append(v.Probes, ssl)
				}
			}
		} else {
			v.UDPProbes = append(v.UDPProbes, p)
		}
	}
}
