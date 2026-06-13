// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Probe payload encoding helpers. See README's attribution section
// for upstream lineage.
package portfinger

import (
	"encoding/hex"
	"strconv"
)

// DecodePattern decodes the nmap-style escape sequences in a pattern
// string: \xNN (hex), \NNN (octal), \a \f \t \n \r \v \\.
//
// DecodePattern 解码 nmap 风格转义序列的 pattern 字符串：\xNN（十六进制）、
// \NNN（八进制）、\a \f \t \n \r \v \\。
func DecodePattern(s string) ([]byte, error) {
	b := []byte(s)
	var result []byte
	for i := 0; i < len(b); {
		if b[i] == '\\' && i+1 < len(b) {
			switch b[i+1] {
			case 'x':
				// \xNN — 2 hex digits. / \xNN — 2 个十六进制位。
				if i+3 < len(b) {
					hexStr := string(b[i+2 : i+4])
					if isValidHex(hexStr) {
						if decoded, err := hex.DecodeString(hexStr); err == nil {
							result = append(result, decoded...)
							i += 4
							continue
						}
					}
				}
			case 'a':
				result = append(result, '\a')
				i += 2
				continue
			case 'f':
				result = append(result, '\f')
				i += 2
				continue
			case 't':
				result = append(result, '\t')
				i += 2
				continue
			case 'n':
				result = append(result, '\n')
				i += 2
				continue
			case 'r':
				result = append(result, '\r')
				i += 2
				continue
			case 'v':
				result = append(result, '\v')
				i += 2
				continue
			case '\\':
				result = append(result, '\\')
				i += 2
				continue
			default:
				// \NNN — 1-3 octal digits. / \NNN — 1-3 个八进制位。
				if i+1 < len(b) && b[i+1] >= '0' && b[i+1] <= '7' {
					octalStr := ""
					j := i + 1
					for j < len(b) && j < i+4 && b[j] >= '0' && b[j] <= '7' {
						octalStr += string(b[j])
						j++
					}
					// 16-bit parse to avoid int8 overflow (\377 = 255).
					// 16 位解析以避免 int8 溢出（\377 = 255 超过 int8）。
					if octal, err := strconv.ParseInt(octalStr, 8, 16); err == nil && octal <= 255 {
						result = append(result, byte(octal))
						i = j
						continue
					}
				}
			}
		}
		// Plain character. / 普通字符。
		result = append(result, b[i])
		i++
	}
	return result, nil
}

// DecodeData is DecodePattern with the leading/trailing quote
// characters stripped. / DecodeData 是 DecodePattern 但去掉首尾
// 引号字符。
func DecodeData(s string) ([]byte, error) {
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	if len(s) > 0 && (s[len(s)-1] == '"' || s[len(s)-1] == '\'') {
		s = s[:len(s)-1]
	}
	return DecodePattern(s)
}

// isValidHex returns true if s is exactly 2 hex digits. / isValidHex
// 在 s 恰好是 2 个十六进制位时返回 true。
func isValidHex(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'F':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
