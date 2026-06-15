// wire_gcc.go — GCC (Generic Conference Control, T.124) Conference
// Create Request builders. GCC is the T.124 layer that wraps the
// per-protocol blocks (clientCore / clientSecurity /
// clientNetwork) in a magic-tagged envelope that fscan's grdp
// and real Windows RDP servers look for.
//
// Split out of wire.go as part of the v0.2.1 god-file refactor.
//
// wire_gcc.go — GCC（Generic Conference Control, T.124）Conference
// Create Request 构造器。GCC 是 T.124 层，把 per-protocol 块
// （clientCore / clientSecurity / clientNetwork）包进带 magic 标签
// 的信封，fscan 的 grdp 和真 Windows RDP 服务器都识别。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分。
package rdp

import "encoding/binary"

// buildGCCConferenceCreateRequest builds the GCC Conference Create
// Request body (without outer TPKT).
//
// buildGCCConferenceCreateRequest 构造 GCC Conference Create
// Request body（不含外层 TPKT）。
//
// The h221NonStandard key 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53
// is a constant from MS-RDPBCGR §2.2.1.3 that signals
// "Microsoft RDP GCC Conference Create Request". Real RDP
// servers (and fscan's grdp) look for this magic in the body.
//
// h221NonStandard key 0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53
// 是 MS-RDPBCGR §2.2.1.3 的常量，表示"Microsoft RDP GCC
// Conference Create Request"。真 RDP 服务器（和 fscan 的 grdp）
// 都在 body 里找这个 magic。
func buildGCCConferenceCreateRequest() []byte {
	magic := []byte{0x14, 0x76, 0x62, 0x36, 0x88, 0x4E, 0xCE, 0x53}

	// clientCore: 32 bytes hostname + minimal version/width/height
	// fields. / clientCore：32 字节 hostname + 最小 version/width/
	// height 字段。
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
//
// buildClientCore 构造最小 clientCore 数据块。
func buildClientCore(hostname []byte) []byte {
	// clientCore: version(4) + desktopWidth(2) + desktopHeight(2) +
	// colorDepth(2) + SASSequence(2) + keyboardLayout(4) +
	// clientBuild(4) + clientName(32) + keyboardType(4) + ...
	// / clientCore：version(4) + ...
	b := make([]byte, 0, 128)
	// Tag 0xC0 0x0D 0x00 0x00 (GCC Conference Create Request)
	b = append(b, 0xC0, 0x0D, 0x00, 0x00)
	// Length placeholder — we don't strictly need it correct
	// for a probe, but include it for sanity. / 长度占位符——
	// 探针不严格要求正确，但写上更稳。
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
//
// buildClientSecurity 构造最小 clientSecurity 数据块。
//
// We declare no supported security protocols — server picks
// PROTOCOL_RDP or whatever the negotiation yields. / 我们声明
// 不支持任何安全协议——服务器选 PROTOCOL_RDP 或协商结果。
func buildClientSecurity() []byte {
	// Tag 0xC0 0x0E 0x00 0x00 for clientSecurity (per MS-RDPBCGR).
	return []byte{0xC0, 0x0E, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

// buildClientNetwork builds a minimal clientNetwork data block.
//
// buildClientNetwork 构造最小 clientNetwork 数据块。
func buildClientNetwork() []byte {
	// Tag 0xC0 0x0F 0x00 0x00 for clientNetwork (per MS-RDPBCGR).
	// / 0xC0 0x0F 0x00 0x00 是 clientNetwork 的 tag。
	return []byte{0xC0, 0x0F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}
