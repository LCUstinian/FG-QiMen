// wire.go — RDP wire protocol helpers (TPKT / X.224 / MCS / GCC / serverCore).
//
// This is a hand-written, NO-fscan-source implementation of the minimum
// RDP 5.0+ handshake needed to extract the server's identity BEFORE
// authentication. We never do Attach / Login / Session Setup — the
// connection is closed as soon as the GCC Conference Create Response
// is parsed.
//
// References:
//   - MS-RDPBCGR §2.2.1 (X.224 / TPKT / MCS / GCC)
//   - MS-RDPBCGR §2.2.1.3.2 (serverCore data block)
//   - RFC 1006 (TPKT)
//
// NO fscan / shadow1ng code is used; the protocol is a public Microsoft
// spec. See README.md Attribution for the original fscan inspiration.
//
// wire.go — RDP 协议辅助函数（TPKT / X.224 / MCS / GCC / serverCore）。
//
// 这是手写的、不引用 fscan 源码的 RDP 5.0+ 握手最小子集实现，在认证
// 之前抽取服务器身份。绝不执行 Attach / Login / Session Setup——
// GCC Conference Create Response 解析完即关闭连接。
//
// 引用：
//   - MS-RDPBCGR §2.2.1 (X.224 / TPKT / MCS / GCC)
//   - MS-RDPBCGR §2.2.1.3.2 (serverCore 数据块)
//   - RFC 1006 (TPKT)
package rdp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Protocol identifiers for the X.224 requestedProtocols field. / X.224
// requestedProtocols 字段的协议标识符。
const (
	ProtocolRDP       uint32 = 0x00000000 // legacy RDP security
	ProtocolSSL       uint32 = 0x00000001 // TLS upgrade (v0.2+)
	ProtocolHYBRID    uint32 = 0x00000002 // NLA (CredSSP)
	ProtocolHYBRID_EX uint32 = 0x00000008 // NLA extended
)

// TPKTFrame prepends a 4-byte TPKT header to `payload`.
//
// TPKT frame layout (RFC 1006):
//
//	1 byte: version (always 0x03)
//	1 byte: reserved (0x00)
//	2 bytes: length (BE, includes the 4-byte header)
//
// TPKTFrame 在 `payload` 前加 4 字节 TPKT 头。
func TPKTFrame(payload []byte) []byte {
	total := 4 + len(payload)
	out := make([]byte, total)
	out[0] = 0x03
	out[1] = 0x00
	binary.BigEndian.PutUint16(out[2:4], uint16(total))
	copy(out[4:], payload)
	return out
}

// TPKTRead reads one TPKT-framed PDU from r and returns the payload
// (everything after the 4-byte TPKT header).
//
// TPKTRead 从 r 读一个 TPKT 框架的 PDU，返 payload（TPKT 4 字节头后
// 的所有内容）。
func TPKTRead(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if hdr[0] != 0x03 {
		return nil, fmt.Errorf("rdp: bad TPKT version 0x%02x", hdr[0])
	}
	length := int(binary.BigEndian.Uint16(hdr[2:4]))
	if length < 4 {
		return nil, fmt.Errorf("rdp: TPKT length %d < 4", length)
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// X224Confirm is the parsed X.224 Connection Confirm. / X224Confirm
// 是解析后的 X.224 Connection Confirm。
type X224Confirm struct {
	// SelectedProtocol is the server's chosen protocol. Compare against
	// ProtocolRDP / ProtocolSSL / ProtocolHYBRID / ProtocolHYBRID_EX.
	// / SelectedProtocol 是服务器选的协议。与上面常量比较。
	SelectedProtocol uint32
}

// EncodeX224CR builds a TPKT-framed X.224 Connection Request.
//
// EncodeX224CR 构造一个 TPKT 包裹的 X.224 Connection Request。
//
// X.224 CR TPDU layout (after TPKT header):
//
//	1 byte: length (of remaining PDU)
//	1 byte: CR (0xE0)
//	2 bytes: DST-REF (0x0000)
//	2 bytes: SRC-REF (any nonzero, we use 0x1234)
//	1 byte: Class Option (0x00)
//	1 byte: cookie length
//	N bytes: cookie
//	1 byte: 0x0D
//	1 byte: 0x0A   ← RDP negotiation cookie terminator
//	4 bytes: requestedProtocols (LE)
//
// The cookie is typically "mstshash=<username>" or empty. The 0x0D0A
// terminator is required even when the cookie is empty.
//
// cookie 通常是 "mstshash=<username>" 或空。0x0D0A 结束符是必需的，
// 即使 cookie 为空。
func EncodeX224CR(cookie string, requestedProtocols uint32) []byte {
	// Build the X.224 PDU body (after length byte).
	// / 构造 X.224 PDU body（长度字节之后）。
	body := make([]byte, 0, 64)
	body = append(body, 0xE0) // CR
	body = append(body, 0x00, 0x00) // DST-REF
	body = append(body, 0x34, 0x12) // SRC-REF (any nonzero)
	body = append(body, 0x00)        // Class Option
	body = append(body, byte(len(cookie)))
	body = append(body, []byte(cookie)...)
	body = append(body, 0x0D, 0x0A) // terminator
	// requestedProtocols (4 bytes LE)
	var proto [4]byte
	binary.LittleEndian.PutUint32(proto[:], requestedProtocols)
	body = append(body, proto[:]...)
	// Prepend length byte.
	// / 前置长度字节。
	pdu := make([]byte, 0, 1+len(body))
	pdu = append(pdu, byte(len(body)))
	pdu = append(pdu, body...)
	// Wrap in TPKT.
	// / TPKT 包裹。
	return TPKTFrame(pdu)
}

// DecodeX224CC reads a TPKT-framed X.224 Connection Confirm.
//
// DecodeX224CC 读一个 TPKT 包裹的 X.224 Connection Confirm。
//
// CC TPDU layout (after TPKT header):
//
//	1 byte: length (of remaining PDU)
//	1 byte: CC (0xD0)
//	2 bytes: DST-REF
//	2 bytes: SRC-REF
//	1 byte: Class Option
//	4 bytes: selectedProtocol (LE)
func DecodeX224CC(r io.Reader) (*X224Confirm, error) {
	pdu, err := TPKTRead(r)
	if err != nil {
		return nil, err
	}
	if len(pdu) < 7 {
		return nil, fmt.Errorf("rdp: CC too short (%d bytes)", len(pdu))
	}
	// pdu[0] is the length byte (we ignore it because we trust TPKT).
	// / pdu[0] 是长度字节（信 TPKT 即可，忽略）。
	if pdu[1] != 0xD0 {
		return nil, fmt.Errorf("rdp: expected CC (0xD0), got 0x%02x", pdu[1])
	}
	if len(pdu) < 11 {
		return nil, fmt.Errorf("rdp: CC too short for selectedProtocol (%d bytes)", len(pdu))
	}
	proto := binary.LittleEndian.Uint32(pdu[7:11])
	return &X224Confirm{SelectedProtocol: proto}, nil
}

// EncodeMCSConnectInitial builds a TPKT-framed MCS Connect-Initial PDU
// with a minimal GCC Conference Create Request (T.124).
//
// EncodeMCSConnectInitial 构造一个 TPKT 包裹的 MCS Connect-Initial PDU，
// 含最小 GCC Conference Create Request（T.124）。
//
// The structure follows MS-RDPBCGR §2.2.1.3 and uses BER encoding for
// the GCC data. We include a minimal clientCore + clientSecurity; the
// server only echoes serverCore back, which is what we want.
//
// 结构遵循 MS-RDPBCGR §2.2.1.3，GCC 数据用 BER 编码。我们包含最小的
// clientCore + clientSecurity；服务器只回 serverCore，那正是我们要的。
func EncodeMCSConnectInitial() []byte {
	// GCC Conference Create Request payload (T.124 key value 0x0C with
	// conference create request body). / GCC Conference Create Request
	// payload（T.124 key 0x0C 跟 conference create request body）。
	gcc := buildGCCConferenceCreateRequest()
	// MCS Connect-Initial BER body:
	//   0x65 = CHOICE [APPLICATION 5] Constructed
	//   encoded length of the following
	//   [0] MCS domain reference (single ASN.1 OBJECT IDENTIFIER)
	//   [1] MCS user data (OCTET STRING wrapping our gcc)
	// / MCS Connect-Initial BER body：
	//   0x65 = CHOICE [APPLICATION 5] Constructed
	//   后面的编码长度
	//   [0] MCS domain reference（单个 ASN.1 OBJECT IDENTIFIER）
	//   [1] MCS user data（OCTET STRING 包裹我们的 gcc）
	mcsBody := buildMCSConnectInitialBody(gcc)
	return TPKTFrame(mcsBody)
}

// buildGCCConferenceCreateRequest builds the GCC Conference Create
// Request body (without outer TPKT).
//
// buildGCCConferenceCreateRequest 构造 GCC Conference Create Request body
// （不含外层 TPKT）。
//
// The h221NonStandard key 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53 is a
// constant from MS-RDPBCGR §2.2.1.3 that signals "Microsoft RDP GCC
// Conference Create Request". Real RDP servers (and fscan's grdp) look
// for this magic in the body.
//
// h221NonStandard key 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53 是
// MS-RDPBCGR §2.2.1.3 的常量，表示"Microsoft RDP GCC Conference Create
// Request"。真 RDP 服务器（和 fscan 的 grdp）都在 body 里找这个 magic。
func buildGCCConferenceCreateRequest() []byte {
	magic := []byte{0x14, 0x76, 0x62, 0x36, 0x88, 0x4E, 0xCE, 0x53}

	// clientCore: 32 bytes hostname + minimal version/width/height fields.
	// / clientCore：32 字节 hostname + 最小的 version/width/height 字段。
	hostname := make([]byte, 32)
	copy(hostname, "fg-qimen")
	clientCore := buildClientCore(hostname)
	clientSecurity := buildClientSecurity()
	clientNetwork := buildClientNetwork()

	// Concatenate per MS-RDPBCGR. / 按 MS-RDPBCGR 拼装。
	var body []byte
	body = append(body, magic...)
	body = append(body, clientCore...)
	body = append(body, clientSecurity...)
	body = append(body, clientNetwork...)
	return body
}

// buildClientCore builds a minimal clientCore data block.
// buildClientCore 构造最小 clientCore 数据块。
func buildClientCore(hostname []byte) []byte {
	// clientCore: version(4) + desktopWidth(2) + desktopHeight(2) +
	// colorDepth(2) + SASSequence(2) + keyboardLayout(4) +
	// clientBuild(4) + clientName(32) + keyboardType(4) + ...
	// / clientCore：version(4) + ...
	b := make([]byte, 0, 128)
	// Tag 0xC0 0x0D 0x00 0x00 (GCC Conference Create Request)
	b = append(b, 0xC0, 0x0D, 0x00, 0x00)
	// Length placeholder — we don't strictly need it correct for a
	// probe, but include it for sanity. / 长度占位符——探针不严格要求
	// 正确，但写上更稳。
	lengthPos := len(b)
	b = append(b, 0x00, 0x00) // length placeholder
	// version (4 LE)
	ver := make([]byte, 4)
	binary.LittleEndian.PutUint32(ver, 0x00080004)
	b = append(b, ver...)
	// desktopWidth, desktopHeight, colorDepth, SASSequence (2 each)
	b = append(b, 0x00, 0x00) // width
	b = append(b, 0x00, 0x00) // height
	b = append(b, 0x00, 0x00) // colorDepth
	b = append(b, 0x00, 0x00) // SASSequence
	// keyboardLayout (4 LE)
	kl := make([]byte, 4)
	binary.LittleEndian.PutUint32(kl, 0x00000409) // en-US
	b = append(b, kl...)
	// clientBuild (4 LE)
	cb := make([]byte, 4)
	binary.LittleEndian.PutUint32(cb, 0x00010000) // 65536
	b = append(b, cb...)
	// clientName (32 bytes, pad with NUL)
	b = append(b, hostname...)
	if len(hostname) < 32 {
		b = append(b, make([]byte, 32-len(hostname))...)
	}
	// keyboardType(4) + keyboardSubtype(4) + keyboardFunctionKey(4)
	b = append(b, make([]byte, 12)...)
	// imeFileName (64 bytes)
	b = append(b, make([]byte, 64)...)
	// Patch the length (number of bytes that follow this field).
	// / 补长度（该字段后跟的字节数）。
	rest := len(b) - (lengthPos + 2)
	binary.LittleEndian.PutUint16(b[lengthPos:lengthPos+2], uint16(rest))
	return b
}

// buildClientSecurity builds a minimal clientSecurity data block.
// buildClientSecurity 构造最小 clientSecurity 数据块。
func buildClientSecurity() []byte {
	// We declare no supported security protocols — server picks PROTOCOL_RDP
	// or whatever the negotiation yields. / 我们声明不支持任何安全协议
	// ——服务器选 PROTOCOL_RDP 或协商结果。
	// Tag 0xC0 0x0E 0x00 0x00 for clientSecurity (per MS-RDPBCGR).
	return []byte{0xC0, 0x0E, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

// buildClientNetwork builds a minimal clientNetwork data block.
// buildClientNetwork 构造最小 clientNetwork 数据块。
func buildClientNetwork() []byte {
	// Tag 0xC0 0x0F 0x00 0x00 for clientNetwork (per MS-RDPBCGR).
	// / 0xC0 0x0F 0x00 0x00 是 clientNetwork 的 tag。
	return []byte{0xC0, 0x0F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

// buildMCSConnectInitialBody wraps the GCC body in an MCS Connect-Initial
// BER envelope. / buildMCSConnectInitialBody 把 GCC body 包进 MCS
// Connect-Initial BER 信封。
func buildMCSConnectInitialBody(gcc []byte) []byte {
	// 0x65 = APPLICATION 5 (MCS Connect-Initial, Constructed).
	// 0x00 0x00 0x00 = length placeholder (3 bytes for lengths > 127).
	// / 0x65 = APPLICATION 5 (MCS Connect-Initial, Constructed)。
	// 0x00 0x00 0x00 = 长度占位符（> 127 用 3 字节）。
	// Then domain reference (we use a 0-byte object identifier):
	//   0x00 = MCS domain reference
	//   <length>
	//   <value>
	// / 然后 domain reference（用 0 字节对象标识符）：
	//   0x00 = MCS domain reference
	//   <length>
	//   <value>
	// Then user data (OCTET STRING):
	//   0x00 0x00 0x00 = BER tag for OCTET STRING (constructed + length 0+)
	//   <length>
	//   <gcc bytes>
	// / 然后 user data（OCTET STRING）：
	//   0x00 0x00 0x00 = BER tag for OCTET STRING
	//   <length>
	//   <gcc 字节>
	//
	// For the probe, we use a minimal valid body that fscan's grdp
	// library would also accept. The exact format is finicky, but the
	// server replies with serverCore regardless of details here.
	// / 探针用最小合法 body，fscan 的 grdp 也接受。格式细节挑剔，
	// 但服务器无论如何都会回 serverCore。
	//
	// We use a "per ITU-T T.125" minimal Connect-Initial:
	//   0x65 (APPLICATION 5, Constructed)
	//   length
	//   0x04 0x01 0x00 (calling domain selector: 1-byte OID, value 0)
	//   0x04 0x01 0x00 (called domain selector)
	//   0x01 0x00 0x00 0x00 (upward flag)
	//   0x04 0x00 (target parameters: 0 parameters)
	//   0x04 0x00 (minimum parameters)
	//   0x04 0x00 (maximum parameters)
	//   0x30 <len> (user data SEQUENCE, wrapping GCC)

	// Domain selectors. / 域选择符。
	mcsInner := []byte{
		0x04, 0x01, 0x00, // calling domain selector
		0x04, 0x01, 0x00, // called domain selector
		0x01, 0x00, 0x00, 0x00, // upward flag
		0x04, 0x00, // target parameters
		0x04, 0x00, // minimum parameters
		0x04, 0x00, // maximum parameters
	}
	// User data: SEQUENCE { 0x05 0x00 0x14 0x76 ... } / user data：...
	// Actually, per MS-RDPBCGR, the user data is a single OCTET STRING
	// containing the GCC body. / 按 MS-RDPBCGR，user data 是单条
	// OCTET STRING 包含 GCC body。
	// We wrap the GCC body in BER OCTET STRING (tag 0x04).
	gccOctet := berOctetString(gcc)
	mcsInner = append(mcsInner, gccOctet...)

	// Wrap in APPLICATION 5. / 包进 APPLICATION 5。
	out := []byte{0x65}
	out = append(out, berLength(len(mcsInner))...)
	out = append(out, mcsInner...)
	return out
}

// berOctetString returns `b` wrapped as a BER OCTET STRING (tag 0x04).
// berOctetString 返 `b` 包成的 BER OCTET STRING（tag 0x04）。
func berOctetString(b []byte) []byte {
	out := []byte{0x04}
	out = append(out, berLength(len(b))...)
	out = append(out, b...)
	return out
}

// berLength encodes a length in BER short or long form.
// berLength 用 BER 短或长形式编码长度。
func berLength(n int) []byte {
	if n < 128 {
		return []byte{byte(n)}
	}
	// Long form: high bit of first byte = 1, then N-1 length bytes.
	// / 长形式：首字节高位=1，后跟 N-1 个长度字节。
	nb := 1
	for v := n; v >= 256; v >>= 8 {
		nb++
	}
	out := make([]byte, 1+nb)
	out[0] = 0x80 | byte(nb)
	for i := nb; i > 0; i-- {
		out[i] = byte(n)
		n >>= 8
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────
// serverCore parsing
// ─────────────────────────────────────────────────────────────────────

// ServerCore is the parsed serverCore data block from a GCC Conference
// Create Response. / ServerCore 是从 GCC Conference Create Response 抽
// 出的 serverCore 数据块。
//
// Wire layout (MS-RDPBCGR §2.2.1.3.2), all little-endian:
//
//	Offset  Size  Field
//	0       4     version
//	4       2     desktopWidth
//	6       2     desktopHeight
//	8       2     colorDepth
//	10      2     SASSequence
//	12      4     keyboardLayout
//	16      4     clientBuild
//	20      32    clientName
//	52      4     keyboardType
//	56      4     keyboardSubType
//	60      4     keyboardFunctionKey
//	64      64    imeFileName
//	128+    ...   (other fields we don't need)
type ServerCore struct {
	Version             uint32
	DesktopWidth        uint16
	DesktopHeight       uint16
	ColorDepth          uint16
	SASSequence         uint16
	KeyboardLayout      uint32
	ClientBuild         uint32
	ClientName          [32]byte
	KeyboardType        uint32
	KeyboardSubType     uint32
	KeyboardFunctionKey uint32
	IMEName             [64]byte
}

// VersionToOSName returns a human-readable OS name for known RDP version
// values. Unknown versions return the raw hex string.
//
// VersionToOSName 对已知 RDP version 返可读 OS 名。未知返原始 hex 串。
func (sc *ServerCore) VersionToOSName() string {
	switch sc.Version {
	case 0x00080004:
		return "Windows 7 / Server 2008 R2 / 10 / 2016"
	case 0x00080005:
		return "Windows 10 1607+ / Server 2019 / 11"
	default:
		return fmt.Sprintf("0x%08X", sc.Version)
	}
}

// parseServerCore decodes a serverCore data block (the raw bytes
// after the GCC envelope). / parseServerCore 解码 serverCore 数据块
// （GCC 信封后的原始字节）。
func parseServerCore(b []byte) (*ServerCore, error) {
	// Minimum size for the fields we care about: 16 + 4 + 32 = 52 bytes.
	// / 我们关心的字段最小大小：16 + 4 + 32 = 52 字节。
	if len(b) < 52 {
		return nil, fmt.Errorf("rdp: serverCore too short (%d bytes)", len(b))
	}
	sc := &ServerCore{
		Version:        binary.LittleEndian.Uint32(b[0:4]),
		DesktopWidth:   binary.LittleEndian.Uint16(b[4:6]),
		DesktopHeight:  binary.LittleEndian.Uint16(b[6:8]),
		ColorDepth:     binary.LittleEndian.Uint16(b[8:10]),
		SASSequence:    binary.LittleEndian.Uint16(b[10:12]),
		KeyboardLayout: binary.LittleEndian.Uint32(b[12:16]),
		ClientBuild:    binary.LittleEndian.Uint32(b[16:20]),
	}
	copy(sc.ClientName[:], b[20:52])
	if len(b) >= 128 {
		sc.KeyboardType = binary.LittleEndian.Uint32(b[52:56])
		sc.KeyboardSubType = binary.LittleEndian.Uint32(b[56:60])
		sc.KeyboardFunctionKey = binary.LittleEndian.Uint32(b[60:64])
		copy(sc.IMEName[:], b[64:128])
	}
	return sc, nil
}
