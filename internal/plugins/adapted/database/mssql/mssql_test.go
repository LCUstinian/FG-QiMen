// mssql_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
//
// mssql_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (mssql.go:52-92, go-mssqldb driver):
//
//	[1] client sends PRELOGIN (packet type 0x12) with version,
//	    encryption, instance-name, thread-id and MARS options.
//	[2] server replies with PRELOGIN (packet type 0x12) carrying
//	    at minimum VERSION + ENCRYPTION + TERMINATOR. We pick
//	    ENCRYPTION=encryptNotSup (2) so the driver skips TLS
//	    negotiation.
//	[3] client sends LOGIN7 (packet type 0x10) with creds.
//	[4] server replies with a packReply (packet type 0x04) whose
//	    payload is a token stream. We send a single tokenError
//	    (0xAA) whose UsVarChar message contains "Login failed for
//	    user 'invalid'" so go-mssqldb's login loop surfaces
//	    "login error: mssql: Login failed for user 'invalid'".
//	    The plugin's strings.Contains check at mssql.go:82
//	    matches "login" and returns the banner "MSSQL".
//
// The TDS packet header is 8 bytes big-endian:
//   - PacketType (1)
//   - Status (1, bit 0 = final)
//   - Length (2 BE, includes header)
//   - SPID (2 BE)
//   - PacketNo (1)
//   - Window (1)
//
// PRELOGIN response payload format (MS-TDS §2.2.3):
//
//	[id 1B][offset 2B BE][length 2B BE] × N fields
//	[0xFF terminator]
//	[values...]              ← offsets are relative to the start
//	                            of the payload (i.e. byte 0 right
//	                            after the 8-byte packet header).
//
// For N=2 fields, value-area starts at byte offset 5*N+1 = 11
// (1 terminator byte + 5 bytes per field header).
//
// / 仿真的线协议格式 (mssql.go:52-92, go-mssqldb 驱动):
// [1] 客户端发 PRELOGIN（包类型 0x12）带 version/encryption/...
// [2] 服务端回 PRELOGIN（包类型 0x12），至少含 VERSION +
// ENCRYPTION + TERMINATOR。这里 ENCRYPTION=encryptNotSup (2)
// 让驱动跳过 TLS 协商。
// [3] 客户端发 LOGIN7（包类型 0x10）。
// [4] 服务端回 packReply（包类型 0x04），payload 为 token 流。
// 我们发单个 tokenError (0xAA)，其 UsVarChar 消息含 "Login
// failed for user 'invalid'"，go-mssqldb 登录循环会上抛
// "login error: mssql: Login failed for user 'invalid'"，
// 触发 mssql.go:82 的 strings.Contains("login") 匹配，插件
// 返 banner "MSSQL"。
//
// TDS 包头 8 字节大端：PacketType(1)+Status(1)+Length(2 BE)+
// SPID(2 BE)+PacketNo(1)+Window(1)。
//
// Coverage note: TDS is a deep, stateful, binary protocol. The
// happy path below drives the login-error branch (mssql.go:82-90)
// which exercises most of Identify, including the four-strings-
// Contains check that anchors the "is this MSSQL?" decision. The
// successful-query branch (mssql.go:69-76 — banner "MSSQL: <ver>"
// via firstLine) requires a full LOGINACK + SQL Batch round trip
// (LOGIN7ACK token + COLMETADATA + ROW + DONE); per the v0.6.0
// fake-server plan §11.2 this is acceptable for complex protocols
// where exhaustive negative coverage requires a multi-packet
// state machine. / TDS 是深度的、有状态的二进制协议。下面的
// happy path 驱动登录失败分支 (mssql.go:82-90)，覆盖 Identify
// 的大部分逻辑（含 "是 MSSQL 吗" 的 4 路 strings.Contains
// 判断）。成功的查询分支 (mssql.go:69-76 — banner "MSSQL:
// <ver>" via firstLine) 需要完整的 LOGINACK + SQL Batch 往返；
// 按 v0.6.0 fake-server 计划 §11.2，对复杂协议而言这是可接受
// 的，因为穷尽负向覆盖需要多包状态机。
package mssql

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// tds packet type constants (server-to-client side, since we are
// the fake server). / TDS 包类型常量（服务端→客户端，我们做假
// server 所以用的是这一组）。
const (
	packReplyTDS  = 0x04
	packPreloginT = 0x12

	// PL option IDs (MS-TDS §2.2.3). / PRELOGIN option ID。
	plVersion    = 0x00
	plEncryption = 0x01
	plTerminator = 0xFF

	// Encryption value 2 = not supported (skip TLS). / 加密值 2 =
	// 不支持（跳过 TLS）。
	encryptNotSup = 0x02

	// Token types (MS-TDS §2.2.4). / Token 类型。
	tokenError = 0xAA
)

// readTdsPacket reads one TDS packet (header + payload) from c.
// Returns the packet type and payload (header stripped). / 从 c
// 读一个 TDS 包（头+负载）。返回包类型和去掉头的负载。
func readTdsPacket(c net.Conn) (packetType byte, payload []byte, err error) {
	var hdr [8]byte
	if _, err = readFull(c, hdr[:]); err != nil {
		return 0, nil, err
	}
	packetType = hdr[0]
	length := binary.BigEndian.Uint16(hdr[2:4])
	if length < 8 {
		return packetType, nil, nil
	}
	payload = make([]byte, length-8)
	if _, err = readFull(c, payload); err != nil {
		return packetType, nil, err
	}
	return packetType, payload, nil
}

// readFull is io.ReadFull without importing io. / readFull 是
// 不引入 io 的 io.ReadFull。
func readFull(c net.Conn, buf []byte) (int, error) {
	off := 0
	for off < len(buf) {
		n, err := c.Read(buf[off:])
		if err != nil {
			return off, err
		}
		off += n
	}
	return off, nil
}

// buildPreloginResponse emits a PRELOGIN packet (type 0x12) that
// advertises VERSION=15.0.0.0 and ENCRYPTION=encryptNotSup, plus
// the 0xFF terminator. The driver uses VERSION only for logging
// (mssql.go:81 — it inspects "SQL Server" / "mssql" / "denied" /
// "login" substrings from the eventual login error), so the
// numeric value is cosmetic here. / buildPreloginResponse 生成
// 一个 PRELOGIN 包（类型 0x12），声明 VERSION=15.0.0.0 和
// ENCRYPTION=encryptNotSup，外加 0xFF 终止符。驱动只把 VERSION
// 用于日志，mssql.go:81 看的是登录错误里的关键字，所以这里
// 数值不重要。
func buildPreloginResponse() []byte {
	// 2 fields × 5 bytes header + 1 terminator byte = 11 bytes
	// of header-area. Value-area starts at payload offset 11.
	// / 2 字段 × 5 字节头 + 1 字节终止符 = 11 字节头部区。值
	// 区从 payload 偏移 11 开始。
	const valueOffset = 11
	// VERSION value: 6 bytes (4 version + 2 minor zero pad per
	// MS-TDS). / VERSION 值：6 字节（4 字节版本 + 2 字节次版本
	// 零填充，按 MS-TDS）。
	versionLen := uint16(6)
	encLen := uint16(1)
	// VERSION header: id=0x00, offset=11, length=6.
	// / VERSION 头：id=0x00, offset=11, length=6。
	versionOff := uint16(valueOffset)
	// ENCRYPTION header: id=0x01, offset=17, length=1.
	// / ENCRYPTION 头：id=0x01, offset=17, length=1。
	encOff := uint16(valueOffset) + versionLen

	payload := make([]byte, 0, 32)
	// VERSION field header / VERSION 字段头
	payload = append(payload, plVersion)
	var be2 [2]byte
	binary.BigEndian.PutUint16(be2[:], versionOff)
	payload = append(payload, be2[:]...)
	binary.BigEndian.PutUint16(be2[:], versionLen)
	payload = append(payload, be2[:]...)
	// ENCRYPTION field header / ENCRYPTION 字段头
	payload = append(payload, plEncryption)
	binary.BigEndian.PutUint16(be2[:], encOff)
	payload = append(payload, be2[:]...)
	binary.BigEndian.PutUint16(be2[:], encLen)
	payload = append(payload, be2[:]...)
	// Terminator / 终止符
	payload = append(payload, plTerminator)
	// VERSION value: 15.0.0.0 + 2 zero pad bytes
	// / VERSION 值：15.0.0.0 + 2 字节零填充
	payload = append(payload, 0x0F, 0x00, 0x00, 0x00, 0x00, 0x00)
	// ENCRYPTION value / ENCRYPTION 值
	payload = append(payload, encryptNotSup)

	// 8-byte packet header / 8 字节包头
	//
	// Note: the PRELOGIN RESPONSE uses packet type 0x04 (reply),
	// not 0x12. go-mssqldb's parsePrelogin treats the server's
	// PRELOGIN reply as a regular reply packet (status.type =
	// packReplyTDS = 0x04); sending 0x12 here triggers
	// "invalid respones, expected packet type 4, PRELOGIN
	// RESPONSE" before any token parsing.
	// / PRELOGIN RESPONSE 用包类型 0x04（reply）而不是 0x12。
	// go-mssqldb 的 parsePrelogin 把服务器的 PRELOGIN 回复
	// 当作普通 reply 包（status.type = packReplyTDS = 0x04）；
	// 这里发 0x12 会触发 "invalid respones, expected packet
	// type 4, PRELOGIN RESPONSE"，在 token 解析前就报错。
	hdr := [8]byte{
		packReplyTDS,
		0x01,       // Status = final
		0x00, 0x00, // Length (filled below)
		0x00, 0x00, // SPID
		0x01, // PacketNo
		0x00, // Window
	}
	total := 8 + len(payload)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(total))
	out := make([]byte, 0, total)
	out = append(out, hdr[:]...)
	out = append(out, payload...)
	return out
}

// buildLoginErrorReply emits a single packReply packet whose
// payload is one tokenError (0xAA). The tokenError's UsVarChar
// message is the UTF-16LE encoding of msg. The driver will parse
// it via parseError72 (mssqldb token.go:940) and surface it as
// "login error: mssql: <msg>" — which matches the "login"
// substring check at mssql.go:82. / buildLoginErrorReply 生成
// 单个 packReply 包，payload 是一个 tokenError (0xAA)。
// tokenError 的 UsVarChar 消息是 msg 的 UTF-16LE 编码。驱动会
// 经 parseError72 解析后上抛 "login error: mssql: <msg>"，
// 触发 mssql.go:82 的 "login" 子串检查。
func buildLoginErrorReply(msg string) []byte {
	// Encode msg as UTF-16LE (MS-TDS UsVarChar). UCS-2 == UTF-16LE
	// for BMP chars which "Login failed..." happens to be.
	// / 把 msg 编码为 UTF-16LE（MS-TDS UsVarChar）。BMP 字符的
	// UCS-2 与 UTF-16LE 一致，"Login failed..." 全是 BMP 字符。
	utf16 := utf16LE(msg)
	// Inner data after the 2-byte length prefix / 2 字节长度前缀之后
	// 的内部数据
	inner := make([]byte, 0, 64)
	// Number (int32 LE) / 编号
	var i32 [4]byte
	binary.LittleEndian.PutUint32(i32[:], 18456)
	inner = append(inner, i32[:]...)
	// State (1B) / 状态
	inner = append(inner, 0x01)
	// Class (1B) / 严重级
	inner = append(inner, 0x0E)
	// Message: UsVarChar = uint16 LE numchars + UTF-16LE bytes
	// / Message: UsVarChar = uint16 LE 字符数 + UTF-16LE 字节
	numChars := uint16(len(msg))
	var u16 [2]byte
	binary.LittleEndian.PutUint16(u16[:], numChars)
	inner = append(inner, u16[:]...)
	inner = append(inner, utf16...)
	// ServerName: BVarChar (uint8 char-count + UTF-16LE). Empty
	// → single 0x00 byte. / ServerName: BVarChar（uint8 字符数
	// + UTF-16LE）。空 → 1 字节 0x00。
	inner = append(inner, 0x00)
	// ProcName: BVarChar. Empty → 0x00. / ProcName: BVarChar，
	// 空 → 0x00。
	inner = append(inner, 0x00)
	// LineNumber (int32 LE) / 行号
	binary.LittleEndian.PutUint32(i32[:], 1)
	inner = append(inner, i32[:]...)

	// tokenError payload: token type + length + inner
	// / tokenError payload：token 类型 + 长度 + 内部数据
	token := make([]byte, 0, 4+len(inner))
	token = append(token, tokenError)
	binary.LittleEndian.PutUint16(u16[:], uint16(len(inner)))
	token = append(token, u16[:]...)
	token = append(token, inner...)

	// tokenDone: the driver REQUIRES this to terminate the LOGIN7
	// reply. Without it, the parser hits EOF after the error token
	// and surfaces "Invalid TDS stream: EOF" (which doesn't match
	// "login" / "denied" / "SQL Server" / "mssql" at mssql.go:82, so
	// the plugin returns nil). Format (MS-TDS §2.2.4):
	//   - TokenType (1B) = 0xFD
	//   - Status (2B LE) = 0 (final)
	//   - CurCmd (2B LE) = 0x000 (login)
	//   - RowCount (8B LE) = 0
	// / tokenDone：驱动必须有这个 token 来收尾 LOGIN7 响应。少了
	// 它，parser 在 error token 后撞 EOF，报 "Invalid TDS stream:
	// EOF"（不含 "login"/"denied"/"SQL Server"/"mssql"，所以插件
	// 返 nil）。
	tokenDone := []byte{
		tokenError + 0x53, // 0xFD (DONE token type)
		0x00, 0x00,        // status = final
		0x00, 0x00, // curCmd = login
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // rowCount = 0
	}
	payload := append(token, tokenDone...)

	// 8-byte packet header / 8 字节包头
	hdr := [8]byte{
		packReplyTDS,
		0x01, // Status = final
		0x00, 0x00,
		0x00, 0x00,
		0x01,
		0x00,
	}
	total := 8 + len(payload)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(total))
	out := make([]byte, 0, total)
	out = append(out, hdr[:]...)
	out = append(out, payload...)
	return out
}

// utf16LE encodes s as UTF-16LE bytes (BMP-only, no surrogate
// pair handling — sufficient for ASCII/Latin-1 server error
// messages). / utf16LE 把 s 编码为 UTF-16LE 字节（仅 BMP，
// 不处理代理对——ASCII/Latin-1 的服务端错误消息够用）。
func utf16LE(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r > 0xFFFF {
			r = '?' // BMP clamp; never hit for our messages.
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

// authErrorHandler drives the minimum TDS handshake needed to
// surface an auth-error from go-mssqldb's login loop:
//
//  1. read client's PRELOGIN (header + payload) — drain so the
//     kernel doesn't RST us when we write the response. / 读
//     客户端 PRELOGIN（头+负载）——排空以防我们写响应时内核
//     发 RST。
//  2. write PRELOGIN response (VERSION + ENCRYPTION=NotSup +
//     TERMINATOR). / 写 PRELOGIN 响应（VERSION + ENCRYPTION=
//     NotSup + TERMINATOR）。
//  3. read client's LOGIN7 — drain. / 读客户端 LOGIN7 并排空。
//  4. write a single packReply containing one tokenError with
//     message "Login failed for user 'invalid'". / 写单个
//     packReply，含一个 tokenError，消息为 "Login failed for
//     user 'invalid'"。
//
// The driver will close the connection after parsing the error,
// so the handler returns; fakeserver.ListenLoop owns connection
// lifetime. / 驱动解析错误后会关连接，所以 handler 直接返回；
// 连接的生灭由 fakeserver.ListenLoop 拥有。
func authErrorHandler(c net.Conn) {
	// 1. PRELOGIN from client.
	pt, _, err := readTdsPacket(c)
	if err != nil {
		return
	}
	if pt != packPreloginT {
		return // not a TDS prelogin → driver won't proceed; close.
	}
	// 2. PRELOGIN response.
	if _, err = c.Write(buildPreloginResponse()); err != nil {
		return
	}
	// 3. LOGIN7 from client — just drain.
	if _, _, err = readTdsPacket(c); err != nil {
		return
	}
	// 4. Login-error reply.
	_, _ = c.Write(buildLoginErrorReply("Login failed for user 'invalid'"))
}

// notMssqlHandler sends a non-PRELOGIN first byte so the driver's
// BeginRead sees packNormal (0x0F) instead of packPrelogin (0x12)
// and panics out of the connection setup, surfacing a non-MSSQL
// error to the plugin. The plugin must then return nil at
// mssql.go:91. / notMssqlHandler 发一个非 PRELOGIN 首字节，让驱
// 动的 BeginRead 看到 packNormal (0x0F) 而非 packPrelogin (0x12)，
// 在连接建立阶段 panic，给插件一个非 MSSQL 错误，触发
// mssql.go:91 的 nil 返回。
func notMssqlHandler(c net.Conn) {
	// Single TDS-ish packet with packNormal (0x0F) — driver will
	// reject this in readPrelogin and panic with "invalid
	// respones, expected packet type 4, PRELOGIN RESPONSE". / 单
	// 个 TDS-ish 包用 packNormal (0x0F) — 驱动在 readPrelogin
	// 中会拒绝并 panic "invalid respones, expected packet type
	// 4, PRELOGIN RESPONSE"。
	hdr := [8]byte{0x0F, 0x01, 0x00, 0x08, 0x00, 0x00, 0x01, 0x00}
	_, _ = c.Write(hdr[:])
}

// TestMssql_IdentifyHit drives a minimal TDS handshake and ends
// with a tokenError containing "Login failed for user 'invalid'".
// The driver's login loop surfaces "login error: mssql: Login
// failed for user 'invalid'", which the plugin's strings.Contains
// check at mssql.go:82 matches against "login", returning a
// non-nil *types.Result with Service "mssql" and banner "MSSQL".
// / TestMssql_IdentifyHit 驱动最小 TDS 握手并以 tokenError
// "Login failed for user 'invalid'" 收尾。驱动登录循环会抛出
// "login error: mssql: Login failed for user 'invalid'"，
// 插件在 mssql.go:82 用 strings.Contains 匹配 "login"，返
// 非 nil *types.Result，Service="mssql"，banner="MSSQL"。
// SKIPPED: previously the production plugin's DSN at mssql.go:57 was
//
//	server=127.0.0.1:<port>;<port>;<creds>
//
// which go-mssqldb's tcpParser.ParseServer (msdsn/conn_str.go:1094)
// assigned verbatim to Config.Host — the colon-port suffix was NOT
// stripped. The driver then called net.ParseIP(p.Host) → nil →
// net.LookupIP("127.0.0.1:<port>") → "no such host"
// (protocol.go:83), so the connection never reached the TDS handshake.
//
// Fixed 2026-09-11: mssql.go:57 now passes `host` (not `addr`) to
// the `server=%s` format verb, so the DSN is `server=127.0.0.1;port=N;…`.
// The fake-server framework below drives a full PRELOGIN → LOGIN7
// → tokenError("Login failed for user 'invalid'") round trip; the
// driver's login loop surfaces
// "login error: mssql: Login failed for user 'invalid'" which the
// plugin's strings.Contains check at mssql.go:82 matches against
// "login", returning a non-nil *types.Result with Service="mssql"
// and banner="MSSQL". / 此前因 mssql.go:57 DSN 把端口塞进
// server= 字段，go-mssqldb LookupIP 直接失败，TDS 握手走不到。
// 2026-09-11 修复：mssql.go:57 改用 `host`（不是 `addr`）作为
// server=%s 参数，DSN 变为 `server=127.0.0.1;port=N;…`。下面
// 的假 server 框架驱动完整 PRELOGIN → LOGIN7 → tokenError
// 握手；驱动登录循环会抛出
// "login error: mssql: Login failed for user 'invalid'"，
// 插件在 mssql.go:82 用 strings.Contains 匹配 "login"，
// 返 Service="mssql"、banner="MSSQL"。
func TestMssql_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, authErrorHandler)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid PRELOGIN + Login-failed reply")
	}
	if r.Service != "mssql" {
		t.Errorf("Service = %q, want %q", r.Service, "mssql")
	}
	if r.Banner != "MSSQL" {
		t.Errorf("Banner = %q, want %q", r.Banner, "MSSQL")
	}
}

// TestMssql_IdentifyMiss sends a packNormal byte where the driver
// expects PRELOGIN. The driver's readPrelogin panics with a
// non-MSSQL error message (e.g. "Invalid TDS stream: ..."). The
// plugin's strings.Contains check at mssql.go:82 fails to match
// any of "SQL Server" / "mssql" / "denied" / "login", so it
// returns nil — confirming we don't false-positive on a non-MSSQL
// TCP service. / TestMssql_IdentifyMiss 发一个 packNormal 字节
// 到驱动期望 PRELOGIN 的位置。驱动的 readPrelogin 会 panic 出
// "Invalid TDS stream: ..." 之类的非 MSSQL 错误。插件在
// mssql.go:82 的 strings.Contains 4 路都不命中，返 nil ——
// 证明我们不会在非 MSSQL TCP 服务上假阳性。
func TestMssql_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, notMssqlHandler)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-MSSQL reply = %+v, want nil", got)
	}
}

// TestMssql_CredentialHit is skipped: the mssql plugin's
// Credential is a documented no-op stub (always returns nil) per
// mssql.go:43-45 — actual MSSQL credential testing lives in
// core/cred/auth/database/mssql.go via MSSQLAuthenticator
// (go-mssqldb). / TestMssql_CredentialHit 跳过：mssql 插件的
// Credential 是有文档说明的空 stub（始终返回 nil），见
// mssql.go:43-45——真正的 MSSQL 凭证测试在
// core/cred/auth/database/mssql.go（MSSQLAuthenticator，
// go-mssqldb）。
func TestMssql_CredentialHit(t *testing.T) {
	t.Skip("mssql.Credential is a no-op stub; see mssql.go Credential()")
}
