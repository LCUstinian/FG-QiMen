// wire_mcs.go — MCS (Multipoint Communication Service) Connect-
// Initial PDU. Per ITU-T T.125 / MS-RDPBCGR §2.2.1.3, the
// client sends a single Connect-Initial that wraps a GCC
// Conference Create Request. The server replies with
// Connect-Response, but for our Identify probe we close after
// writing — we only need the GCC serverCore back to fingerprint
// the OS version (see wire_servercore.go).
//
// Split out of wire.go as part of the v0.2.1 god-file refactor.
//
// wire_mcs.go — MCS（Multipoint Communication Service）Connect-
// Initial PDU。按 ITU-T T.125 / MS-RDPBCGR §2.2.1.3，客户端发一
// 条 Connect-Initial 包一个 GCC Conference Create Request。服务器
// 回 Connect-Response；我们探针在写完就关——只需 GCC serverCore 回
// 回来指纹 OS 版本（见 wire_servercore.go）。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分。
package rdp

// EncodeMCSConnectInitial builds a TPKT-framed MCS Connect-Initial
// PDU with a minimal GCC Conference Create Request (T.124).
//
// EncodeMCSConnectInitial 构造一个 TPKT 包裹的 MCS Connect-Initial
// PDU，含最小 GCC Conference Create Request（T.124）。
//
// The structure follows MS-RDPBCGR §2.2.1.3 and uses BER encoding
// for the GCC data. We include a minimal clientCore +
// clientSecurity; the server only echoes serverCore back, which
// is what we want.
//
// 结构遵循 MS-RDPBCGR §2.2.1.3，GCC 数据用 BER 编码。我们包含最
// 小的 clientCore + clientSecurity；服务器只回 serverCore，那正
// 是我们要的。
func EncodeMCSConnectInitial() []byte {
	// GCC Conference Create Request payload (T.124 key value 0x0C
	// with conference create request body). / GCC Conference Create
	// Request payload（T.124 key 0x0C 跟 conference create request
	// body）。
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

// buildMCSConnectInitialBody wraps the GCC body in an MCS
// Connect-Initial BER envelope.
//
// buildMCSConnectInitialBody 把 GCC body 包进 MCS Connect-Initial
// BER 信封。
func buildMCSConnectInitialBody(gcc []byte) []byte {
	// Domain selectors + parameter placeholders, then the
	// user-data SEQUENCE containing the GCC body (as an OCTET
	// STRING, per MS-RDPBCGR).
	//
	// 域选择符 + 参数占位符，然后是含 GCC body 的 user-data
	// SEQUENCE（按 MS-RDPBCGR 为 OCTET STRING）。
	mcsInner := []byte{
		0x04, 0x01, 0x00, // calling domain selector
		0x04, 0x01, 0x00, // called domain selector
		0x01, 0x00, 0x00, 0x00, // upward flag
		0x04, 0x00, // target parameters
		0x04, 0x00, // minimum parameters
		0x04, 0x00, // maximum parameters
	}
	gccOctet := berOctetString(gcc)
	mcsInner = append(mcsInner, gccOctet...)

	// Wrap in APPLICATION 5. / 包进 APPLICATION 5。
	out := []byte{0x65}
	out = append(out, berLength(len(mcsInner))...)
	out = append(out, mcsInner...)
	return out
}
