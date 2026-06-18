// Package types — error code definitions (v0.3.0).
//
// error codes are 4-character codes grouped by category:
//
//	E0xx — validation / config errors
//	E1xx — persistence / bbolt / project errors
//	E2xx — network / proxy errors
//	E3xx — scan pipeline / plugin errors
//	E9xx — internal / unknown
//
// Codes are stable across releases (we will not renumber), so downstream
// scripts can `grep "^E102"` against log output reliably.
//
// 错误码在版本间稳定（不会重编号），所以下游脚本可以对日志输出安全地
// `grep "^E102"`。
package types

import "fmt"

// Code is a 4-character error code (e.g. "E001").
// Code 是 4 字符错误码（如 "E001"）。
type Code string

// Stable error codes. Append only; never renumber.
// 稳定错误码。仅追加；不要重编号。
const (
	// E0xx — validation / config / 校验与配置
	CodeInvalidTarget   Code = "E001" // invalid host/CIDR/range syntax
	CodeInvalidPort     Code = "E002" // invalid port spec
	CodeInvalidCred     Code = "E003" // invalid credential spec
	CodeMissingTarget   Code = "E004" // no targets specified
	CodeUnknownMode     Code = "E005" // unknown --mode value
	CodeInvalidTimeout  Code = "E006" // invalid duration
	CodeConflictingFlag Code = "E007" // e.g. --resume + --no-state

	// E1xx — persistence / bbolt / project / 持久化与项目
	CodeProjectNameInvalid Code = "E101" // --project name failed validation
	CodeBboltOpenFailed    Code = "E102" // bolt.Open failed
	CodeBboltDecryptFailed Code = "E103" // AES-GCM auth tag mismatch (wrong key)
	CodeBboltCorrupted     Code = "E104" // bbolt file structurally corrupt
	CodeProjectPathEscape  Code = "E105" // path traversal attempt
	CodeOutputPathEscape   Code = "E106" // --output-txt/json escapes cwd and opt-out env not set

	// E2xx — network / proxy / 网络
	CodeProxyInvalid    Code = "E201" // proxy URL parse failed
	CodeProxyDialFailed Code = "E202" // proxy handshake/connect failed
	CodeIfaceInvalid    Code = "E203" // --iface is not a local IP
	CodeResolveFailed   Code = "E204" // DNS resolution failed

	// E3xx — scan pipeline / plugins / 扫描管线
	CodeAllPluginsFailed Code = "E301" // every plugin worker errored
	CodePluginDisabled   Code = "E302" // --plugins excluded everything
	CodeNoOpenPorts      Code = "E303" // port scan found no open ports
	CodeCredentialNone   Code = "E304" // no credentials supplied in crack mode
	CodeTimeoutGlobal    Code = "E305" // --max-runtime triggered

	// E9xx — internal / unknown
	CodeInternal Code = "E999" // unexpected / unrecoverable
)

// CodedError pairs a stable Code with a human message and a Hint.
// CodedError 把稳定的 Code 与人类可读消息和 Hint 配对。
type CodedError struct {
	Code    Code   // E001..E999
	Message string // human description
	Hint    string // suggested fix (empty if none)
}

// Error implements the error interface.
// Error 实现 error 接口。
func (e *CodedError) Error() string {
	if e.Hint == "" {
		return fmt.Sprintf("[%s] %s", e.Code, e.Message)
	}
	return fmt.Sprintf("[%s] %s\n       Hint: %s", e.Code, e.Message, e.Hint)
}

// New constructs a CodedError with the given code, message, and optional hint.
// New 用给定 code、message 和可选 hint 构造 CodedError。
func (c Code) New(message, hint string) *CodedError {
	return &CodedError{Code: c, Message: message, Hint: hint}
}

// Newf is New with printf-style formatting on the message.
// Newf 是 New 的 printf 风格消息格式化版本。
func (c Code) Newf(hint, format string, args ...any) *CodedError {
	return &CodedError{
		Code:    c,
		Message: fmt.Sprintf(format, args...),
		Hint:    hint,
	}
}

// MarshalJSON returns a structured form for --json-errors output (v0.3.0+).
// MarshalJSON 返回 --json-errors 输出的结构化形式（v0.3.0+）。
func (e *CodedError) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"code":%q,"message":%q,"hint":%q}`,
		string(e.Code), e.Message, e.Hint)), nil
}
