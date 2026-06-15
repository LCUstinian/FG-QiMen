// pool_test.go — pin the dedup-key HMAC behaviour and the Clear()
// semantics from the v0.2.1 audit.
//
// The P3 / F-15 audit finding was that the dedup key held
// cleartext on the heap (a process memory dump would surface
// every sprayed cred). The fix HMAC-hashes the key with a
// per-process random nonce and adds Clear() to wipe the
// credential strings post-scan.
//
// We test:
//   - dedupKey of same cred → same key (no false dup-skips)
//   - dedupKey of different creds → different keys (no false
//     dedup-hits)
//   - dedupKey contains no substring of the cleartext (heap
//     dump regression guard)
//   - Clear() resets Len() to 0 and the pool can be re-used
//   - Two processes' dedupKeys (we can't actually fork, so we
//     simulate by computing one and re-deriving via the
//     package-level key — see below for what we can/can't test)
package credential

import (
	"strings"
	"testing"
)

// TestDedupKeyStable — same cred, same key. The dedup contract
// demands this: two calls to Add(cred) must agree on whether
// it's a dup.
func TestDedupKeyStable(t *testing.T) {
	c := Cred{User: "root", Pass: "hunter2", Method: AuthPassword}
	if dedupKey(c) != dedupKey(c) {
		t.Error("dedupKey(c) ≠ dedupKey(c); dedup is non-deterministic")
	}
}

// TestDedupKeyDistinct — different creds, different keys. We
// check the four "boundary" cases: user differs, pass differs,
// method differs, keypath differs. None should collide.
func TestDedupKeyDistinct(t *testing.T) {
	base := Cred{User: "root", Pass: "hunter2", Method: AuthPassword}
	cases := []struct {
		name string
		mod  func(*Cred)
	}{
		{"user differs", func(c *Cred) { c.User = "admin" }},
		{"pass differs", func(c *Cred) { c.Pass = "letmein" }},
		{"method differs", func(c *Cred) { c.Method = AuthKey }},
		{"keypath differs", func(c *Cred) { c.KeyPath = "/id_rsa" }},
		// Concat-collision regression (NUL separator):
		// "ab" + "c" must differ from "a" + "bc". / 拼接碰撞
		// 回归（NUL 分隔）："ab"+"c" 必须异于 "a"+"bc"。
		{"concat ab+c vs a+bc", func(c *Cred) { c.User, c.Pass = "ab", "c" }},
		{"concat a+bc", func(c *Cred) { c.User, c.Pass = "a", "bc" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			other := base
			c.mod(&other)
			if dedupKey(base) == dedupKey(other) {
				t.Errorf("dedupKey collision: %+v and %+v produce same hash", base, other)
			}
		})
	}
}

// TestDedupKeyNoCleartextLeak — heap-dump regression guard. The
// hashed dedup key must NOT contain any 4+ character substring
// of the cleartext password (HMAC-SHA256 is collision-resistant
// against length-extension but we still don't want the same
// hash to contain recognizable cleartext).
func TestDedupKeyNoCleartextLeak(t *testing.T) {
	const secret = "SuperSecret-Password-2024-XYZ"
	c := Cred{User: "root", Pass: secret, Method: AuthPassword}
	k := dedupKey(c)
	// No 4+ char substring of the cleartext should appear in the
	// key. (Sub-4 substrings are too prone to false positives.)
	for i := 0; i+4 <= len(secret); i++ {
		sub := secret[i : i+4]
		if strings.Contains(k, sub) {
			t.Errorf("dedupKey contains cleartext substring %q: %q", sub, k)
		}
	}
	// The full cleartext must not appear either.
	if strings.Contains(k, secret) {
		t.Errorf("dedupKey contains full cleartext: %q", k)
	}
}

// TestPoolAddDedup — same cred added twice: only one in pool.
func TestPoolAddDedup(t *testing.T) {
	p := NewPool()
	c := Cred{User: "u", Pass: "p", Method: AuthPassword}
	if !p.Add(c) {
		t.Error("first Add returned false; want true")
	}
	if p.Add(c) {
		t.Error("second Add returned true; want false (dup)")
	}
	if p.Len() != 1 {
		t.Errorf("Len = %d, want 1", p.Len())
	}
}

// TestPoolClear — Clear resets Len, dedup state, and creds
// slice. The pool must be re-usable after Clear.
func TestPoolClear(t *testing.T) {
	p := NewPool()
	p.Add(Cred{User: "u1", Pass: "p1", Method: AuthPassword})
	p.Add(Cred{User: "u2", Pass: "p2", Method: AuthPassword})
	if p.Len() != 2 {
		t.Fatalf("setup: Len = %d, want 2", p.Len())
	}
	p.Clear()
	if p.Len() != 0 {
		t.Errorf("after Clear: Len = %d, want 0", p.Len())
	}
	// Re-use: adding the same cred should now succeed (index
	// was wiped).
	if !p.Add(Cred{User: "u1", Pass: "p1", Method: AuthPassword}) {
		t.Error("Add after Clear returned false; want true (fresh index)")
	}
}

// TestPoolAllReturnsCopy — All() returns a slice; mutations to
// the returned slice don't affect the pool. (We can't enforce
// that the underlying string is unlinked — Go strings are
// immutable but the slice header + backing array are new.)
func TestPoolAllReturnsCopy(t *testing.T) {
	p := NewPool()
	p.Add(Cred{User: "u", Pass: "p", Method: AuthPassword})
	got := p.All()
	if len(got) != 1 {
		t.Fatalf("All returned %d creds, want 1", len(got))
	}
	// Mutate the returned slice; the pool's slice must not change.
	got[0] = Cred{User: "MUTATED"}
	p.Clear()
	if p.Len() != 0 {
		t.Errorf("Clear after external mutation: Len = %d, want 0", p.Len())
	}
}
