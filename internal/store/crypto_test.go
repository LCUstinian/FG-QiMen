// crypto_test.go — round-trip tests for the AES-256-GCM value encryption
// used by Store.PutResult / Store.PutCred.
//
// crypto_test.go — Store.PutResult / Store.PutCred 用的 AES-256-GCM
// 值加密的往返测试。
package store

import (
	"bytes"
	"errors"
	"testing"
)

func TestDeriveKey_DeterministicAnd32Bytes(t *testing.T) {
	// Same passphrase → same key. / 相同 passphrase → 相同 key。
	k1 := DeriveKey("hunter2")
	k2 := DeriveKey("hunter2")
	if !bytes.Equal(k1, k2) {
		t.Fatalf("DeriveKey not deterministic: %x vs %x", k1, k2)
	}
	if len(k1) != 32 {
		t.Fatalf("key length = %d, want 32", len(k1))
	}

	// Different passphrase → different key. / 不同 passphrase → 不同 key。
	k3 := DeriveKey("hunter3")
	if bytes.Equal(k1, k3) {
		t.Fatalf("DeriveKey produced identical keys for distinct passphrases")
	}
}

func TestNewEncryptedValue_BadKeyLength(t *testing.T) {
	cases := []int{0, 16, 31, 33, 64}
	for _, n := range cases {
		_, err := NewEncryptedValue(make([]byte, n))
		if err == nil {
			t.Errorf("NewEncryptedValue(len=%d) accepted; want error", n)
		}
	}
}

func TestSealOpen_RoundTrip(t *testing.T) {
	key := DeriveKey("test-passphrase")
	enc, err := NewEncryptedValue(key)
	if err != nil {
		t.Fatalf("NewEncryptedValue: %v", err)
	}

	plaintext := []byte(`{"user":"admin","password":"hunter2","host":"10.0.0.1"}`)

	sealed, err := enc.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Sealed output must start with the encrypted magic byte.
	// 密文必须以加密 magic 字节开头。
	if len(sealed) == 0 || sealed[0] != magicEncrypted {
		t.Fatalf("Seal output magic = 0x%02x, want 0x%02x", sealed[0], magicEncrypted)
	}

	// Ciphertext must NOT contain plaintext (sanity check on a small payload).
	// 密文不应包含明文（小负载的健全性检查）。
	if bytes.Contains(sealed, plaintext) {
		t.Fatalf("sealed output contains plaintext bytes — encryption not applied")
	}

	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open roundtrip mismatch:\n got: %s\nwant: %s", opened, plaintext)
	}
}

func TestSeal_NonceUniqueness(t *testing.T) {
	// GCM nonce reuse is catastrophic. Verify Seal generates a fresh
	// nonce each call (12 random bytes — collision odds ≈ 2^-48).
	//
	// GCM nonce 复用是灾难性的。验证 Seal 每次生成新的 nonce
	// (12 随机字节 — 碰撞概率 ≈ 2^-48)。
	key := DeriveKey("nonce-test")
	enc, _ := NewEncryptedValue(key)
	a, _ := enc.Seal([]byte("hello"))
	b, _ := enc.Seal([]byte("hello"))
	if bytes.Equal(a, b) {
		t.Fatalf("two Seals produced identical output — nonce reuse")
	}
	// Compare nonces: bytes [1:13] differ. / 比较 nonce：bytes [1:13] 不同。
	if bytes.Equal(a[1:1+nonceSize], b[1:1+nonceSize]) {
		t.Fatalf("two Seals produced identical nonce — collision")
	}
}

func TestOpen_WrongKey(t *testing.T) {
	keyA := DeriveKey("alpha")
	keyB := DeriveKey("beta")

	encA, _ := NewEncryptedValue(keyA)
	encB, _ := NewEncryptedValue(keyB)

	sealed, err := encA.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	_, err = encB.Open(sealed)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Open with wrong key returned err=%v, want ErrDecryptFailed", err)
	}
}

func TestOpen_TamperedCiphertext(t *testing.T) {
	key := DeriveKey("tamper-test")
	enc, _ := NewEncryptedValue(key)

	sealed, _ := enc.Seal([]byte("important"))

	// Flip a bit in the ciphertext region. / 在密文区翻一位。
	sealed[len(sealed)-1] ^= 0x01

	_, err := enc.Open(sealed)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Open tampered returned err=%v, want ErrDecryptFailed", err)
	}
}

func TestSealOpen_PlaintextBackcompat(t *testing.T) {
	// A v0.2.x plaintext value (no magic byte → leading 0x00) must
	// still round-trip through Open when no key is configured.
	//
	// v0.2.x 明文值（无 magic 字节 → 前导 0x00）在无 key 配置时
	// 仍应能经 Open 正确往返。
	plaintext := []byte(`{"v":1}`)
	sealed := SealPlain(plaintext)

	if sealed[0] != magicPlain {
		t.Fatalf("SealPlain magic = 0x%02x, want 0x%02x", sealed[0], magicPlain)
	}

	// With nil enc, Open returns the plaintext bytes (stripping the magic).
	// 用 nil enc 时，Open 返回明文（去掉 magic）。
	enc := (*EncryptedValue)(nil)
	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open(plain, nil enc): %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open(plain) roundtrip mismatch: got %s, want %s", opened, plaintext)
	}
}

func TestOpen_EmptyInput(t *testing.T) {
	// Zero-length input is a no-op; this is defensive against bucket
	// values that were never written (very rare but seen during tests).
	//
	// 零长输入是 no-op；这是对从未写入的 bucket 值（罕见但测试中
	// 见过）的防御。
	enc, _ := NewEncryptedValue(DeriveKey("any"))
	got, err := enc.Open(nil)
	if err != nil || got != nil {
		t.Fatalf("Open(nil) = (%v, %v), want (nil, nil)", got, err)
	}
	got, err = enc.Open([]byte{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Open([]) = (%v, %v), want ([], nil)", got, err)
	}
}

func TestOpen_EncryptedValueButNoKey(t *testing.T) {
	// If a value is encrypted but no key is configured, Open must
	// return an error rather than silently returning ciphertext.
	//
	// 如果值已加密但未配置 key，Open 必须返回错误而非默默返回密文。
	key := DeriveKey("k")
	encWithKey, _ := NewEncryptedValue(key)
	sealed, _ := encWithKey.Seal([]byte("secret"))

	encNoKey := (*EncryptedValue)(nil)
	_, err := encNoKey.Open(sealed)
	if err == nil {
		t.Fatalf("Open(encrypted, nil enc) returned nil error; want error")
	}
}
