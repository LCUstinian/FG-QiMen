// Package protocols: IPMI authenticator.
//
// Strategy: IPMI v2.0 RAKP (Random Authentication Key Protocol)
// over RMCP+ (UDP 623). Flow:
//   1. Send RMCP+ session open (cmd 0x10, tag 0x81).
//   2. Receive RAKP Message 1 (cmd 0x12, tag 0x91) with random +
//    session ID + privilege level.
//   3. Send RAKP Message 2 (cmd 0x12, tag 0x91) with HMAC-SHA1 of
//    (random || user || password) per the BMC's chosen algorithm.
//   4. Receive RAKP Message 3 (cmd 0x12, tag 0x91) with completion
//    code 0 = success, non-zero = auth failure.
//
// HARD RULE: on a hit we return. We do NOT issue any IPMI command
// (no Get Channel Auth, no Get SEL, no Set User Password, no
// Activate Payload).
//
// 包 protocols：IPMI 认证器。
// 策略：IPMI v2.0 RAKP（Random Authentication Key Protocol）over
// RMCP+（UDP 623）。流程：1) 发 RMCP+ session open；2) 收 RAKP
// Message 1（带 random + session ID + privilege level）；3) 发 RAKP
// Message 2（HMAC-SHA1(random || user || password)）；4) 收 RAKP
// Message 3（completion code 0 = 成功，非 0 = 认证失败）。
//
// 硬性原则：命中即返回。不跑任何 IPMI 命令（不 Get Channel Auth、
// 不 Get SEL、不 Set User Password、不 Activate Payload）。
package remote

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// IPMIAuthenticator authenticates against IPMI v2.0 BMCs over
// RMCP+ (UDP 623). / IPMIAuthenticator 通过 RMCP+（UDP 623）对
// IPMI v2.0 BMC 认证。
//
// DefaultPort returns 623 (standard IPMI). / DefaultPort 返 623（标准
// IPMI）。
type IPMIAuthenticator struct{}

// NewIPMIAuthenticator returns a default IPMI authenticator.
// NewIPMIAuthenticator 返回默认配置的 IPMI 认证器。
func NewIPMIAuthenticator() *IPMIAuthenticator { return &IPMIAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *IPMIAuthenticator) Name() string { return "ipmi" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *IPMIAuthenticator) DefaultPorts() []int {
	return []int{623}
}

// IPMI v2.0 RAKP constants. / IPMI v2.0 RAKP 常量。
const (
	ipmiRMCPPlusVersion = 0x04 // RMCP+ v2.0
	ipmiCmdSessionOpen  = 0x10
	ipmiCmdRAKP         = 0x12
	ipmiPrivilegeAdmin  = 0x04 // ADMIN
	ipmiAuthAlgSHA1     = 0x04 // HMAC-SHA1
	ipmiIntegrityAlgNone = 0x00
	ipmiConfidAlgNone   = 0x00
)

// Authenticate implements credential.Authenticator. Tries each cred in
// order. / Authenticate 实现 credential.Authenticator。按顺序尝试每个
// cred。
func (a *IPMIAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one IPMI v2.0 RAKP auth round. / attempt 跑一次 IPMI
// v2.0 RAKP 认证。
func (a *IPMIAuthenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Set a per-call deadline: without it, Read on a UDP socket that
	// never receives a reply would block forever (closed UDP ports on
	// Windows do not always return ICMP-unreachable promptly, unlike
	// Linux). / 设置单次 deadline：未设时，对永远不响应的 UDP socket
	// 调 Read 会无限阻塞（Windows 上关闭的 UDP 端口不一定及时返
	// ICMP-unreachable，与 Linux 不同）。
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// 1. Send RMCP+ session open. / 1) 发 RMCP+ session open。
	sessionOpen := buildIPMISessionOpen()
	if _, err := conn.Write(sessionOpen); err != nil {
		return false, err
	}
	// 2. Read RAKP Message 1. / 2) 读 RAKP Message 1。
	msg1, err := readIPMIPacket(conn)
	if err != nil {
		return false, nil
	}
	// Extract random + session ID from msg1. / 从 msg1 抽 random + session ID。
	random, sessionID, err := parseRAKPMessage1(msg1)
	if err != nil {
		return false, nil
	}
	// 3. Send RAKP Message 2 with HMAC-SHA1. / 3) 发 RAKP Message 2。
	msg2 := buildRAKPMessage2(sessionID, random, user, pass)
	if _, err := conn.Write(msg2); err != nil {
		return false, err
	}
	// 4. Read RAKP Message 3. / 4) 读 RAKP Message 3。
	msg3, err := readIPMIPacket(conn)
	if err != nil {
		return false, nil
	}
	completionCode, err := parseRAKPMessage3(msg3)
	if err != nil {
		return false, nil
	}
	// Completion code 0 = success. / Completion code 0 = 成功。
	return completionCode == 0, nil
}

// buildIPMISessionOpen builds an RMCP+ Session Open packet.
// / buildIPMISessionOpen 构造 RMCP+ Session Open 包。
func buildIPMISessionOpen() []byte {
	// RMCP+ header (4 bytes): version(1) + reserved(1) + seq(1) + class(1).
	// / RMCP+ 头（4 字节）：version(1) + reserved(1) + seq(1) + class(1)。
	// Class 0x06 = ASF/RMCP (1.5). Class 0x07 = IPMI.
	// Actually: RMCP+ uses class 0x06 for ASF or 0x07 for IPMI. We
	// use IPMI. / Class 0x06 = ASF/RMCP（1.5）。Class 0x07 = IPMI。
	// 实际：RMCP+ 用 class 0x06（ASF）或 0x07（IPMI）。我们用 IPMI。
	pkt := []byte{
		ipmiRMCPPlusVersion, 0x00, 0x00, 0x07, // RMCP+ header (class = IPMI)
	}
	// IPMI session open: message tag (1) + reserved (1) + command (1) +
	// completion code (1) + ... / IPMI session open：message tag (1) +
	// reserved (1) + command (1) + completion code (1) + ...
	pkt = append(pkt, 0x81, 0x00, ipmiCmdSessionOpen, 0x00)
	// Remote console session ID (4) + auth type (1) = NONE (0).
	// / Remote console session ID (4) + auth type (1) = NONE (0)。
	pkt = append(pkt, 0x00, 0x00, 0x00, 0x00, 0x00)
	// Integrity algorithm (1) = NONE. Confidentiality algorithm (1)
	// = NONE. / Integrity algorithm (1) = NONE。Confidentiality
	// algorithm (1) = NONE。
	pkt = append(pkt, ipmiIntegrityAlgNone, ipmiConfidAlgNone)
	return pkt
}

// readIPMIPacket reads one IPMI packet (RMCP+ + IPMI message).
// / readIPMIPacket 读一个 IPMI 包（RMCP+ + IPMI 消息）。
func readIPMIPacket(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// parseRAKPMessage1 extracts the random bytes and session ID from
// RAKP Message 1. / parseRAKPMessage1 从 RAKP Message 1 抽 random
// 字节和 session ID。
func parseRAKPMessage1(pkt []byte) (random []byte, sessionID uint32, err error) {
	// RMCP+ header (4) + IPMI message tag (1) + reserved (1) +
	// command (1) + completion code (1) = 8 bytes before RAKP body.
	// / RMCP+ 头（4）+ IPMI message tag (1) + reserved (1) +
	// command (1) + completion code (1) = RAKP body 前 8 字节。
	if len(pkt) < 8 {
		return nil, 0, fmt.Errorf("ipmi: packet too short %d", len(pkt))
	}
	if pkt[7] != ipmiCmdRAKP {
		return nil, 0, fmt.Errorf("ipmi: not a RAKP message, tag=0x%02x", pkt[7])
	}
	body := pkt[8:]
	// RAKP Message 1 body: tag(1) + reserved(1) + sessionID(4) +
	// sequence(4) + privilege(1) + reserved(1) + userLen(1) + user(N) +
	// randomLen(1) + random(16) ...
	// / RAKP Message 1 body：tag(1) + reserved(1) + sessionID(4) +
	// sequence(4) + privilege(1) + reserved(1) + userLen(1) + user(N) +
	// randomLen(1) + random(16) ...
	if len(body) < 13 {
		return nil, 0, fmt.Errorf("ipmi: RAKP body too short %d", len(body))
	}
	sessionID = binary.LittleEndian.Uint32(body[2:6])
	// Skip sequence + privilege + reserved + userLen + user.
	// / 跳 sequence + privilege + reserved + userLen + user。
	userLen := int(body[11])
	off := 12 + userLen
	if len(body) < off+1 {
		return nil, 0, fmt.Errorf("ipmi: RAKP body too short for user")
	}
	randLen := int(body[off])
	off++
	if len(body) < off+randLen {
		return nil, 0, fmt.Errorf("ipmi: RAKP body too short for random")
	}
	return body[off : off+randLen], sessionID, nil
}

// buildRAKPMessage2 builds RAKP Message 2 with HMAC-SHA1 of
// (random || user || password). / buildRAKPMessage2 构造 RAKP
// Message 2 含 (random || user || password) 的 HMAC-SHA1。
func buildRAKPMessage2(sessionID uint32, random []byte, user, pass string) []byte {
	// Compute HMAC-SHA1(auth_key, random || user || pass) where
	// auth_key is the user password (per RAKP v2.0 simplified auth).
	// / 算 HMAC-SHA1(auth_key, random || user || pass)，auth_key
	// 是用户密码（按 RAKP v2.0 简化认证）。
	mac := hmac.New(sha1.New, []byte(pass))
	mac.Write(random)
	mac.Write([]byte(user))
	hmacValue := mac.Sum(nil)
	// RMCP+ header. / RMCP+ 头。
	pkt := []byte{
		ipmiRMCPPlusVersion, 0x00, 0x00, 0x07, // class = IPMI
	}
	// IPMI message tag (1) + reserved (1) + command (1) + completion
	// code (1). / IPMI message tag (1) + reserved (1) + command (1) +
	// completion code (1)。
	pkt = append(pkt, 0x91, 0x00, ipmiCmdRAKP, 0x00)
	// RAKP Message 2 body: tag(1) + reserved(1) + sessionID(4) +
	// sequence(4) + privilege(1) + reserved(1) + userLen(1) + user(N) +
	// authAlg(1) + authLen(1) + hmac(20).
	// / RAKP Message 2 body：tag(1) + reserved(1) + sessionID(4) +
	// sequence(4) + privilege(1) + reserved(1) + userLen(1) + user(N) +
	// authAlg(1) + authLen(1) + hmac(20)。
	pkt = append(pkt, 0x00, 0x00) // tag + reserved
	var sidBuf [4]byte
	binary.LittleEndian.PutUint32(sidBuf[:], sessionID)
	pkt = append(pkt, sidBuf[:]...)
	// Sequence number (4) = 1. / Sequence number (4) = 1。
	var seqBuf [4]byte
	binary.LittleEndian.PutUint32(seqBuf[:], 1)
	pkt = append(pkt, seqBuf[:]...)
	pkt = append(pkt, ipmiPrivilegeAdmin)    // privilege
	pkt = append(pkt, 0x00)                 // reserved
	pkt = append(pkt, byte(len(user)))      // userLen
	pkt = append(pkt, []byte(user)...)       // user
	pkt = append(pkt, ipmiAuthAlgSHA1)      // authAlg
	pkt = append(pkt, byte(len(hmacValue))) // authLen
	pkt = append(pkt, hmacValue...)          // hmac
	return pkt
}

// parseRAKPMessage3 extracts the completion code from RAKP
// Message 3. / parseRAKPMessage3 从 RAKP Message 3 抽 completion
// code。
func parseRAKPMessage3(pkt []byte) (completionCode uint8, err error) {
	if len(pkt) < 8 {
		return 0, fmt.Errorf("ipmi: RAKP Message 3 too short %d", len(pkt))
	}
	if pkt[7] != ipmiCmdRAKP {
		return 0, fmt.Errorf("ipmi: not a RAKP message, tag=0x%02x", pkt[7])
	}
	// Body starts at offset 8. Completion code is the first byte of
	// RAKP body. / Body 从 offset 8 开始。Completion code 是 RAKP
	// body 第一字节。
	return pkt[8], nil
}

// init registers the IPMI authenticator. / init 注册 IPMI 认证器。
func init() {
	credential.Register(NewIPMIAuthenticator())
}
