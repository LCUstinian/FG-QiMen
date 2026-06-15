// wire_ber.go — BER (Basic Encoding Rules) primitives used by the
// MCS Connect-Initial and GCC Conference Create Request builders
// in wire_mcs.go and wire_gcc.go. BER is the ITU-T X.690 subset
// of ASN.1 we use to frame the nested structures RDP carries on
// the wire.
//
// Split out of wire.go as part of the v0.2.1 god-file refactor.
//
// wire_ber.go — MCS Connect-Initial 和 GCC Conference Create
// Request 构造器（wire_mcs.go、wire_gcc.go）共用的 BER（Basic
// Encoding Rules）原语。BER 是 ITU-T X.690 ASN.1 子集，RDP 用来
// 在线上框嵌套结构。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分。
package rdp

// berOctetString returns `b` wrapped as a BER OCTET STRING (tag 0x04).
// berOctetString 返 `b` 包成的 BER OCTET STRING（tag 0x04）。
func berOctetString(b []byte) []byte {
	out := []byte{0x04}
	out = append(out, berLength(len(b))...)
	out = append(out, b...)
	return out
}

// berLength encodes a length in BER short or long form.
//
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
