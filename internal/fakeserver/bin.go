package fakeserver

import "encoding/binary"

// WriteMagic is a convenience for binary protocol handlers that
// need to emit a magic-byte prefix + uint16 / uint32 length +
// payload in one shot. Returns the assembled byte slice. Big-endian.
//
// / WriteMagic 是给二进制协议 handler 的便利函数：拼 magic + 长度
// + payload 一次性 emit。
func WriteMagic(magic []byte, lengthLen int, payload []byte) []byte {
	out := make([]byte, 0, len(magic)+lengthLen+len(payload))
	out = append(out, magic...)
	switch lengthLen {
	case 2:
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(len(payload)))
		out = append(out, b[:]...)
	case 4:
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(len(payload)))
		out = append(out, b[:]...)
	}
	out = append(out, payload...)
	return out
}
