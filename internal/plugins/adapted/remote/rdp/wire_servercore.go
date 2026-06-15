// wire_servercore.go — serverCore parser. This is the only
// *response-side* code in the rdp/wire package: the client-side
// builders (tpkt / x224 / mcs / gcc) all live in their own
// files. The serverCore is what fscan's grdp and Windows RDP
// servers echo back inside their GCC Conference Create Response;
// parsing it gives us version + hostname + keyboard layout
// (enough to fingerprint the OS via VersionToOSName).
//
// Split out of wire.go as part of the v0.2.1 god-file refactor.
//
// wire_servercore.go — serverCore 解析器。这是 rdp/wire 包里
// 唯一的*响应侧*代码：客户端 builder（tpkt / x224 / mcs / gcc）
// 各在各自文件。serverCore 是 fscan 的 grdp 和 Windows RDP 服务
// 器在它们的 GCC Conference Create Response 里回显的；解析它
// 给我们 version + hostname + keyboard layout（足以通过
// VersionToOSName 指纹 OS）。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分。
package rdp

import (
	"encoding/binary"
	"fmt"
)

// ServerCore is the parsed serverCore data block from a GCC
// Conference Create Response. / ServerCore 是从 GCC Conference
// Create Response 抽出的 serverCore 数据块。
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

// VersionToOSName returns a human-readable OS name for known RDP
// version values. Unknown versions return the raw hex string.
//
// VersionToOSName 对已知 RDP version 返可读 OS 名。未知返原始 hex
// 串。
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

// parseServerCore decodes a serverCore data block (the raw
// bytes after the GCC envelope). / parseServerCore 解码
// serverCore 数据块（GCC 信封后的原始字节）。
func parseServerCore(b []byte) (*ServerCore, error) {
	// Minimum size for the fields we care about:
	// 16 + 4 + 32 = 52 bytes. / 我们关心的字段最小大小：16 + 4
	// + 32 = 52 字节。
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
