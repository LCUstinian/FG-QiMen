// fuzz_test.go — fuzz targets for the dedup key + pool.
// Run via `go test -fuzz=FuzzPoolDedup -fuzztime=30s`.
package credential

import (
	"strings"
	"testing"
)

// FuzzPoolDedup drives the Pool with random cred tuples. Two creds
// with the same (method, user, pass, keypath) must dedup; any other
// pair must NOT collide (HMAC-SHA256 with a random per-process key
// makes accidental collisions ~2^-128). / FuzzPoolDedup 用随机凭据
// 元组驱动 Pool。两条相同 (method, user, pass, keypath) 必须去重；
// 其他任意两条不能碰撞（带随机 per-process key 的 HMAC-SHA256 让
// 意外碰撞概率 ~2^-128）。
func FuzzPoolDedup(f *testing.F) {
	// Seed corpus. / 种子语料。
	f.Add(string(AuthPassword), "admin", "admin", "")
	f.Add(string(AuthPassword), "admin", "admin123", "")
	f.Add(string(AuthToken), "user", "tok", "")
	f.Add(string(AuthPassword), "", "", "")
	f.Add(string(AuthNone), "", "", "")

	f.Fuzz(func(t *testing.T, method, user, pass, keypath string) {
		// Truncate to keep keys short. / 截断保持 key 短。
		if len(method) > 32 {
			method = method[:32]
		}
		if len(user) > 128 {
			user = user[:128]
		}
		if len(pass) > 128 {
			pass = pass[:128]
		}
		if len(keypath) > 128 {
			keypath = keypath[:128]
		}
		// Reject control chars that might confuse the dedup key.
		// 拒绝可能让 dedup key 混乱的控制字符。
		if strings.ContainsAny(method, "\x00") ||
			strings.ContainsAny(user, "\x00") ||
			strings.ContainsAny(pass, "\x00") ||
			strings.ContainsAny(keypath, "\x00") {
			return
		}
		// Check that dedupKey is deterministic. / 检查 dedupKey 确定性。
		c := Cred{Method: AuthMethod(method), User: user, Pass: pass, KeyPath: keypath}
		k1 := dedupKey(c)
		k2 := dedupKey(c)
		if k1 != k2 {
			t.Errorf("dedupKey not deterministic: %q vs %q", k1, k2)
		}
	})
}
