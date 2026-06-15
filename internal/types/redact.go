// redact.go — credential-redaction helpers for the UI / output layer.
//
// Why: scanning operators routinely run fg-qimen on shared hosts, in
// CI, over screen sharing, or under journald / syslog capture. Writing
// cleartext passwords to stderr, the TUI, or result.txt means every
// copy of those sinks (CI logs, screen recordings, log aggregators)
// leaks the credentials. The audit (P0#2, P0#3) flagged this as a
// critical confidentiality gap.
//
// Policy: cleartext display is opt-in via Config.ShowCleartext. The
// default render shows a short, unambiguous fingerprint of the
// password (length + first/last char, plus length-only for the user)
// so an operator watching a live dashboard still sees "this is a hit
// on user X with a 12-char password" — enough to confirm the
// authenticator is working — without exposing the secret itself.
//
// redact.go — UI / output 层的凭据 redaction helpers。
//
// 原因：扫描操作员常在共享主机 / CI / 屏幕共享 / journald / syslog 下
// 跑 fg-qimen。明文口令写到 stderr / TUI / result.txt 等于把这些 sink
// 的所有副本（CI 日志、屏幕录制、日志聚合）都泄露出去。审计（P0#2、
// P0#3）把这条标为关键保密性缺陷。
//
// 策略：明文显示通过 Config.ShowCleartext 显式 opt-in。默认渲染给出
// 简短、可识别的密码指纹（长度 + 收尾字符，user 仅显示长度）—— 操作
// 员看 dashboard 仍能看到"这是 user X 的 12 字符密码命中"，足以确认
// authenticator 在工作，但 secret 本身不外泄。
package types

// RedactUser returns a user-safe fingerprint of a username. We never
// reveal the full user; in practice usernames are short and frequently
// the secret (e.g. "service-account-prod-01"), so showing only the
// length and a single character is the right balance.
//
// RedactUser 返回用户名的脱敏指纹。用户名通常也是秘密（"service-
// account-prod-01"），只显长度+单字符最稳妥。
func RedactUser(u string) string {
	if u == "" {
		return "<empty>"
	}
	if len(u) <= 2 {
		return repeat('*', len(u))
	}
	// "a****z" form: first char, len-2 stars, last char.
	// "a****z" 形态：首字符 + len-2 个星号 + 尾字符。
	return string(u[0]) + repeat('*', len(u)-2) + string(u[len(u)-1])
}

// RedactPassword returns a length-only fingerprint. Showing the first
// or last character of a password is itself a small leak (lets an
// observer constrain dictionary attacks); length is safer and still
// gives "this is a 12-char password" signal.
//
// RedactPassword 返回仅含长度的指纹。显首尾字符也算小泄露（让观察者
// 缩小字典攻击范围）；仅显长度更安全，且仍能传达"这是 12 字符口令"。
func RedactPassword(p string) string {
	if p == "" {
		return "<empty>"
	}
	return "**" + itoaLen(len(p)) + "**"
}

// ShowUserPassword returns either the cleartext "user / pass" pair
// or a redacted variant, depending on cfg.ShowCleartext. The format
// mirrors the historical "user / pass" rendering so downstream
// parsers and operator habits continue to work — only the contents
// change when redaction is on.
//
// ShowUserPassword 根据 cfg.ShowCleartext 返回明文或脱敏 "user / pass"
// 对。格式沿用历史 "user / pass" 渲染，下游解析器和操作员习惯不需调
// 整；仅在开启 redact 时内容变化。
func ShowUserPassword(cfg *Config, user, pass string) string {
	if cfg != nil && cfg.ShowCleartext {
		return user + " / " + pass
	}
	return RedactUser(user) + " / " + RedactPassword(pass)
}

// repeat returns s repeated n times. Avoids pulling in strings.Repeat
// from a hot path that must work for short inputs only.
//
// repeat 返回 s 重复 n 次。避免在仅处理短输入的热路径上引 strings.Repeat。
func repeat(r byte, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = r
	}
	return string(b)
}

// itoaLen formats a small non-negative int as a string. Used only for
// password length display (so values are bounded by max password
// length; we cap at 999 to keep the redacted form compact).
//
// itoaLen 把小非负整数格式化为字符串。仅用于密码长度展示（数值由最大
// 口令长度界定；这里 cap 在 999 让脱敏形态更紧凑）。
func itoaLen(n int) string {
	if n > 999 {
		return "999+"
	}
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
