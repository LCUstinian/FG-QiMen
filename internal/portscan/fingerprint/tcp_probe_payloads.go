// tcp_probe_payloads.go — TCP active-probe payload selection.
//
// The nmap-service-probes TCP rule set is probe-RESPONSE driven: most
// services (HTTP, memcached, Redis, RPC portmappers, ...) say nothing
// until spoken to, so passive banner grabbing (the FirstBanner read
// right after connect) can never see them. These selectors pick which
// probe payloads to send to a silent open port before giving up on
// identification.
//
// / tcp_probe_payloads.go — TCP 主动探针的 payload 选择。
//
// nmap-service-probes 的 TCP 规则集是"探针-响应"驱动的：多数服务
// （HTTP、memcached、Redis、RPC portmapper……）不被搭话就一言不发，
// 被动 banner 抓取（连接后的 FirstBanner 读）永远看不见它们。这里的
// 选择器决定对沉默的开放端口发哪些探针 payload，之后才放弃识别。
package fingerprint

import "sort"

// MaxTCPProbesPerPort caps how many probe payloads may be sent to ONE
// silent TCP port. The nmap hint set for a busy port (80, 443) can be
// large; a scanner that fires all of them is noisy and slow. Three is
// the sweet spot observed across the golden corpus: hint-specific
// probes hit almost immediately, the tail adds noise, not recall.
// / MaxTCPProbesPerPort 限制单个沉默 TCP 端口最多发多少个探针
// payload。繁忙端口（80、443）的 nmap 提示集可以很大，全发既吵又
// 慢。golden 语料观察下来 3 是甜点：hint 专属探针几乎立即命中，
// 尾部只加噪声不加召回。
const MaxTCPProbesPerPort = 3

// TCPPayloadMap builds the port → probe payloads map for TCP ports
// with explicit hints in the probe database (e.g. memcached's
// "version" probe hints 11211). Per port: rarity-ascending, ties in
// database order, capped at MaxTCPProbesPerPort. The NULL probe (empty
// payload — the passive banner grab already covers it) and the TLS
// handshake probes (SSLSessionReq / TLSSessionReq — the webtitle
// plugin owns TLS) are excluded, as are probes with empty or
// undecodable payloads.
// / TCPPayloadMap 为探针库里有显式端口提示的 TCP 端口构建
// port → payload 映射（如 memcached 的 "version" 探针提示 11211）。
// 每端口：rarity 升序，同 rarity 按库序，上限 MaxTCPProbesPerPort。
// NULL 探针（空 payload——被动 banner 抓取已覆盖）与 TLS 握手探针
// （SSLSessionReq / TLSSessionReq——TLS 归 webtitle 插件管）排除，
// 空 payload 或解码失败的探针同样跳过。
func (v *VScan) TCPPayloadMap() map[int][][]byte {
	type entry struct {
		rarity  int
		seq     int // database order tiebreak / 库序平局裁决
		payload []byte
	}
	out := make(map[int][][]byte)
	perPort := make(map[int][]entry)
	for seq, p := range v.Probes {
		if p.Data == "" {
			continue // NULL probe — passive banner grab covers it
		}
		if p.Name == "SSLSessionReq" || p.Name == "TLSSessionReq" {
			continue // TLS probes — webtitle plugin owns TLS
		}
		payload, err := DecodePattern(p.Data)
		if err != nil || len(payload) == 0 {
			continue
		}
		for _, port := range ParsePortHint(p.Ports) {
			perPort[port] = append(perPort[port], entry{rarity: p.Rarity, seq: seq, payload: payload})
		}
	}
	for port, entries := range perPort {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].rarity != entries[j].rarity {
				return entries[i].rarity < entries[j].rarity
			}
			return entries[i].seq < entries[j].seq
		})
		if len(entries) > MaxTCPProbesPerPort {
			entries = entries[:MaxTCPProbesPerPort]
		}
		payloads := make([][]byte, 0, len(entries))
		for _, e := range entries {
			payloads = append(payloads, e.payload)
		}
		out[port] = payloads
	}
	return out
}

// TCPGenericPayloads returns the fallback probes for TCP ports that no
// database probe hints at: the rarity-1 trio nmap itself walks for
// unknown ports (GenericLines, GetRequest, Help). A trailing \r\n\r\n
// or a bare "help" is harmless to essentially every TCP service —
// they answer with an error banner, which is exactly what the
// matching engine eats.
// / TCPGenericPayloads 返回探针库无端口提示的 TCP 端口的兜底探针：
// nmap 自己对未知端口也会走的 rarity-1 三件套（GenericLines、
// GetRequest、Help）。尾部 \r\n\r\n 或一句 "help" 对几乎所有 TCP
// 服务无害——服务会以错误 banner 回应，而这正是匹配引擎的口粮。
func (v *VScan) TCPGenericPayloads() [][]byte {
	wanted := map[string]int{"GenericLines": 0, "GetRequest": 1, "Help": 2}
	out := make([][]byte, 0, len(wanted))
	for _, p := range v.Probes {
		if _, ok := wanted[p.Name]; !ok {
			continue
		}
		payload, err := DecodePattern(p.Data)
		if err != nil || len(payload) == 0 {
			continue
		}
		out = append(out, payload)
	}
	return out
}
