// redact_test.go — pin the redaction contract so future tweaks can't
// silently widen the leak surface.
//
// Each test asserts BOTH the redacted form (default) and the cleartext
// form (opt-in). If either side drifts unexpectedly, the test fails.
package types

import "testing"

// TestRedactUser — username fingerprint is "first*…*last" with
// (len-2) stars (or len stars for length≤2). Never reveals the full
// user. Empty user becomes "<empty>".
func TestRedactUser(t *testing.T) {
	// stars(n) keeps the expected values readable — a 30-char run of
	// '*' is hard to count by eye, and bugs in star counts were the
	// regression vector this test was written to catch.
	//
	// stars(n) 让期望值在测试里可读——30 个 '*' 的字面量人眼难数，而
	// 星号数错正是本测试要捕获的回归。
	stars := func(n int) string { return repeat('*', n) }

	cases := []struct {
		in, want string
	}{
		{"", "<empty>"},
		{"a", "*"},                                                         // len 1 → 1 star
		{"ab", "**"},                                                       // len 2 → 2 stars
		{"alice", "a" + stars(3) + "e"},                                    // len 5 → 3 stars
		{"service-account-prod-01", "s" + stars(21) + "1"},                 // len 23 → 21 stars
		{"verylongusernamethatkeepsgoingforever", "v" + stars(35) + "r"},   // len 37 → 35 stars
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := RedactUser(c.in); got != c.want {
				t.Errorf("RedactUser(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestRedactPassword — only length is exposed, never any character
// of the actual password. Critical: leaking first/last char of a
// password is a small but real dictionary-attack enabler.
func TestRedactPassword(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "<empty>"},
		{"x", "**1**"},
		{"hunter2", "**7**"},
		// 999 is the cap; longer values render as 999+ to keep the
		// redacted form compact (P0#2: redaction must not accidentally
		// leak password length precision in high-entropy cases).
		{repeat('a', 1000), "**999+**"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := RedactPassword(c.in); got != c.want {
				t.Errorf("RedactPassword(len=%d) = %q, want %q", len(c.in), got, c.want)
			}
		})
	}
}

// TestShowUserPassword — the gate. nil cfg → redact (defensive).
// cfg.ShowCleartext=false → redact. cfg.ShowCleartext=true →
// cleartext. The format is "user / pass" either way so downstream
// parsers and operator habits don't have to branch.
func TestShowUserPassword(t *testing.T) {
	const user, pass = "root", "hunter2"
	wantRedacted := "r**t / **7**"
	wantCleartext := "root / hunter2"

	if got := ShowUserPassword(nil, user, pass); got != wantRedacted {
		t.Errorf("nil cfg: got %q, want %q", got, wantRedacted)
	}
	if got := ShowUserPassword(&Config{ShowCleartext: false}, user, pass); got != wantRedacted {
		t.Errorf("ShowCleartext=false: got %q, want %q", got, wantRedacted)
	}
	if got := ShowUserPassword(&Config{ShowCleartext: true}, user, pass); got != wantCleartext {
		t.Errorf("ShowCleartext=true: got %q, want %q", got, wantCleartext)
	}
}

// TestRedactDoesNotLeakCleartext — regression guard for the most
// catastrophic failure mode: the helper accidentally regressing to
// always emitting cleartext. We hash the redacted output of a known
// secret and assert it does not contain any prefix/suffix of the
// original. If a future refactor reintroduces a leak, the substring
// check will fail.
func TestRedactDoesNotLeakCleartext(t *testing.T) {
	const secret = "SuperSecret-Password-2024"
	redacted := RedactPassword(secret)
	if redacted == secret {
		t.Fatal("redacted form equals cleartext — redaction is broken")
	}
	// None of the secret's substrings of length ≥ 2 should appear.
	// / 长度 ≥ 2 的 secret 子串都不应出现。
	for i := 0; i+2 <= len(secret); i++ {
		sub := secret[i : i+2]
		if contains(redacted, sub) {
			t.Errorf("redacted output %q contains cleartext substring %q", redacted, sub)
		}
	}
}

// contains is a tiny local helper to avoid importing strings in a
// test that wants to assert "no substring". strings.Contains would
// pull in unicode handling we don't need for ASCII secrets.
//
// contains 是本地小 helper，避免在不需要 unicode 的 ASCII 测试里导
// 入 strings。
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
