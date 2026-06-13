// Package rdp — RDP deep fingerprint Identify plugin.
//
// Orchestrates a 4-step handshake (X.224 CR → X.224 CC → MCS Connect-
// Initial → MCS Connect-Response), parses the serverCore data from
// the GCC Conference Create Response, and returns a common.Result
// with Extra = *common.RDPFingerprint so the pipeline can dual-write
// to rdp.json / rdp.txt via runResultSink (see core/pipeline.go).
//
// HARD RULE: this plugin performs IDENTIFICATION ONLY. It never
// runs Attach / Login / Session Setup. The connection is closed as
// soon as the serverCore is parsed. This is the v0.1 RDP deep
// fingerprint deliverable; RDP NLA credential testing is v0.3+.
//
// 包 rdp — RDP 深指纹 Identify 插件。
// 编排 4 步握手（X.224 CR → X.224 CC → MCS Connect-Initial → MCS
// Connect-Response），从 GCC Conference Create Response 抽 serverCore
// 数据，返带 Extra = *common.RDPFingerprint 的 common.Result，让管线
// 通过 runResultSink 双写到 rdp.json / rdp.txt（见 core/pipeline.go）。
//
// 硬性原则：本插件只做识别。绝不跑 Attach / Login / Session Setup。
// serverCore 解析完即关闭连接。这是 v0.1 RDP 深指纹交付物；RDP NLA
// 凭据测试是 v0.3+。
package rdp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin is the RDP deep fingerprint Identify plugin. / Plugin 是 RDP
// 深指纹 Identify 插件。
type Plugin struct{}

// New returns a new rdp plugin. / New 返回一个新的 rdp 插件。
func New() *Plugin { return &Plugin{} }

// init registers the plugin with the plugins package registry.
// init 把插件注册到 plugins 包注册表。
func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "rdp" }

// Ports returns default RDP port. / Ports 返回默认 RDP 端口。
func (p *Plugin) Ports() []int { return []int{3389} }

// Modes returns Identify only. / Modes 仅返回 Identify。
//
// RDP credential testing (NLA / CredSSP) is explicitly v0.3+; this
// plugin only fingerprints. / RDP 凭据测试（NLA / CredSSP）明确是 v0.3+；
// 本插件只做识别。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify implements plugins.Plugin. Performs the 4-step handshake,
// extracts serverCore, and returns a Result with Extra holding the
// structured RDPFingerprint.
//
// Identify 实现 plugins.Plugin。跑 4 步握手，抽 serverCore，返带
// Extra（持结构化 RDPFingerprint）的 Result。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	conn, err := dialRDP(ctx, host, port, 3*time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	fp := common.RDPFingerprint{
		Host:     host,
		Port:     port,
		ScanTime: time.Now(),
	}

	// Step 1+2: X.224 CR with PROTOCOL_HYBRID, read CC.
	// / Step 1+2：X.224 CR + PROTOCOL_HYBRID，读 CC。
	cr := EncodeX224CR("mstshash=fgqimen", ProtocolHYBRID)
	if _, err := conn.Write(cr); err != nil {
		return nil
	}
	cc, err := DecodeX224CC(conn)
	if err != nil {
		return nil
	}
	fp.ProtocolVersion = cc.SelectedProtocol
	fp.NLASupported = cc.SelectedProtocol == ProtocolHYBRID ||
		cc.SelectedProtocol == ProtocolHYBRID_EX
	// PROTOCOL_SSL means the server wants a TLS upgrade before
	// anything else. v0.1 returns nil — TLS path is v0.2+.
	// / PROTOCOL_SSL 意味着服务器要先 TLS 升级。v0.1 返 nil——TLS
	// 路径是 v0.2+。
	if cc.SelectedProtocol == ProtocolSSL {
		return nil
	}

	// Step 3: MCS Connect-Initial. / Step 3：MCS Connect-Initial。
	ci := EncodeMCSConnectInitial()
	if _, err := conn.Write(ci); err != nil {
		return nil
	}

	// Step 4: read MCS Connect-Response, extract serverCore.
	// / Step 4：读 MCS Connect-Response，抽 serverCore。
	// Read a TPKT-framed PDU. The first 4 bytes are TPKT header,
	// everything after is the MCS body. / 读一个 TPKT 包裹的 PDU。
	// 前 4 字节是 TPKT 头，后面是 MCS body。
	hdr := make([]byte, 4)
	if _, err := readFull(conn, hdr); err != nil {
		return nil
	}
	tpktLen := int(uint16(hdr[2])<<8 | uint16(hdr[3]))
	if tpktLen < 4 {
		return nil
	}
	mcsBody := make([]byte, tpktLen-4)
	if _, err := readFull(conn, mcsBody); err != nil {
		return nil
	}
	sc, err := parseServerCoreFromMCS(mcsBody)
	if err != nil {
		return nil
	}
	fp.ServerName = strings.TrimRight(string(sc.ClientName[:]), "\x00")
	fp.OSBuild = strconv.FormatUint(uint64(sc.ClientBuild), 10)
	fp.OSVersion = sc.VersionToOSName()
	fp.Domain = "" // serverCore doesn't include the AD domain; v0.2+

	return &common.Result{
		Host:    host,
		Port:    port,
		Service: "rdp",
		Plugin:  "rdp",
		Banner: fmt.Sprintf("RDP %s build=%s nla=%v os=%s",
			fp.ServerName, fp.OSBuild, fp.NLASupported, fp.OSVersion),
		Extra: &fp,
		Time:  time.Now(),
	}
}

// dialRDP opens a TCP connection to host:port with a deadline.
// dialRDP 打开到 host:port 的 TCP 连接并设 deadline。
func dialRDP(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// readFull reads exactly len(buf) bytes from c. / readFull 从 c 读
// 正好 len(buf) 字节。
func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// parseServerCoreFromMCS walks the MCS Connect-Response BER envelope
// to find the GCC Conference Create Response, then the serverCore
// block, and parses it.
//
// parseServerCoreFromMCS 走 MCS Connect-Response BER 信封找到 GCC
// Conference Create Response，再找 serverCore block 并解析。
//
// The reliable anchor is the 8-byte prefix that precedes serverCore in
// real MS-RDPBCGR wire output: 0x30 0xC0 0x00 0x00 0x00 0x00 0x00 0x00
// (BER SEQUENCE OF tag + length encoding). Searching for this 8-byte
// pattern is more robust than searching for the version field (which
// can match garbage in BER domain references).
//
// 可靠锚点是真 MS-RDPBCGR 线输出里 serverCore 之前的 8 字节前缀：
// 0x30 0xC0 0x00 0x00 0x00 0x00 0x00 0x00（BER SEQUENCE OF tag +
// 长度编码）。比搜索 version 字段更稳（version 在 BER domain 引用中
// 可能撞到垃圾）。
func parseServerCoreFromMCS(mcs []byte) (*ServerCore, error) {
	anchor := []byte{0x30, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	idx := indexOf(mcs, anchor)
	if idx < 0 {
		return nil, fmt.Errorf("rdp: serverCore anchor not found in MCS body")
	}
	start := idx + len(anchor)
	end := start + 128
	if end > len(mcs) {
		end = len(mcs)
	}
	if start >= len(mcs) {
		return nil, fmt.Errorf("rdp: serverCore extends past MCS body")
	}
	return parseServerCore(mcs[start:end])
}

// indexOf returns the index of the first occurrence of needle in haystack,
// or -1. / indexOf 返 needle 在 haystack 中首次出现的下标，没有返 -1。
func indexOf(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
