// Package fingerprint is the webtitle fingerprint matching engine.
// Package fingerprint 是 webtitle 指纹匹配引擎。
//
// Three matcher layers, run in order:
//   1. Hardcoded regex rules (rules.go) — fast path for known names
//   2. FingerprintHub JSON (enhanced.go) — 3139 community rules
//   3. (Per-rule favicon hash) — most precise; weighted higher
//
// 三层匹配器，按顺序跑：
//   1. 硬编码正则规则（rules.go）——已知名字的快速路径
//   2. FingerprintHub JSON（enhanced.go）——3139 条社区规则
//   3. （每条规则的 favicon 哈希）——最精确；权重更高
package fingerprint

// CheckData is the per-source input to the matcher. We support
// multiple CheckData per Identify (one for the initial response,
// one for the redirect target) but the v0.1 webtitle flattens them
// before passing in.
//
// CheckData 是匹配器每次的输入源。我们支持每次 Identify 多个
// CheckData（一个给初始响应，一个给跳转目标），但 v0.1 webtitle
// 在传入前会合并。
type CheckData struct {
	Body    []byte
	Headers string
	Favicon []int32 // nil = skip favicon match
}
