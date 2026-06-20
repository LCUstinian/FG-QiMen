// crypto_test.go — round-trip tests for the AES-256-GCM value encryption
// used by Store.PutResult / Store.PutCred.
//
// crypto_test.go — Store.PutResult / Store.PutCred 用的 AES-256-GCM
// 值加密的往返测试。
//
// Covers the four supported on-disk formats (0x00 plaintext, 0x01
// legacy no-AAD, 0x02 v0.3.1 SHA-256+AAD, 0x03 v0.4 Argon2id+AAD) and
// the per-magic KDF dispatch in Open().
//
// 覆盖四种磁盘格式（0x00 明文、0x01 遗留无 AAD、0x02 v0.3.1 SHA-256+AAD、
// 0x03 v0.4 Argon2id+AAD）及 Open() 按 magic 分发 KDF 的逻辑。
package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"testing"
)

// legacyCipher returns an AES-GCM AEAD bound to the legacy SHA-256 key.
// Used by tests to hand-craft v0.3.0 / v0.3.1 format values without
// going through Seal (which now always emits 0x03).
// legacyCipher 返回绑定到遗留 SHA-256 key 的 AES-GCM AEAD。测试用来
// 手工构造 v0.3.0 / v0.3.1 格式值，不走 Seal（Seal 现在总输出 0x03）。
func legacyCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func TestDeriveKeyArgon2id_DeterministicAndDistinct(t *testing.T) {
	// Same passphrase + same salt → same key. / 相同 passphrase + salt → 相同 key。
	salt := []byte("0123456789abcdef")
	k1 := DeriveKeyArgon2id("hunter2", salt)
	k2 := DeriveKeyArgon2id("hunter2", salt)
	if !bytes.Equal(k1, k2) {
		t.Fatalf("DeriveKeyArgon2id not deterministic: %x vs %x", k1, k2)
	}
	if len(k1) != keySize {
		t.Fatalf("key length = %d, want %d", len(k1), keySize)
	}

	// Different passphrase → different key. / 不同 passphrase → 不同 key。
	k3 := DeriveKeyArgon2id("hunter3", salt)
	if bytes.Equal(k1, k3) {
		t.Fatalf("DeriveKeyArgon2id produced identical keys for distinct passphrases")
	}

	// Different salt → different key. / 不同 salt → 不同 key。
	salt2 := []byte("fedcba9876543210")
	k4 := DeriveKeyArgon2id("hunter2", salt2)
	if bytes.Equal(k1, k4) {
		t.Fatalf("DeriveKeyArgon2id produced identical keys for distinct salts")
	}
}

func TestDeriveKeySHA256_Deterministic(t *testing.T) {
	k1 := DeriveKeySHA256("hunter2")
	k2 := DeriveKeySHA256("hunter2")
	if !bytes.Equal(k1, k2) {
		t.Fatalf("DeriveKeySHA256 not deterministic")
	}
	if len(k1) != keySize {
		t.Fatalf("key length = %d, want %d", len(k1), keySize)
	}
	k3 := DeriveKeySHA256("hunter3")
	if bytes.Equal(k1, k3) {
		t.Fatalf("DeriveKeySHA256 produced identical keys for distinct passphrases")
	}
}

func TestDeriveKeyArgon2id_DiffersFromSHA256(t *testing.T) {
	// Sanity: the two KDFs must NOT produce the same key for the same
	// passphrase, otherwise we'd be lying about the upgrade. (SHA-256
	// has no salt; Argon2id uses a fixed test salt here.)
	// 健全性：两个 KDF 对同一 passphrase 不能产出相同 key,否则"升级"就是
	// 假的。（SHA-256 无 salt；Argon2id 这里用固定 test salt。）
	argon := DeriveKeyArgon2id("hunter2", []byte("0123456789abcdef"))
	sha := DeriveKeySHA256("hunter2")
	if bytes.Equal(argon, sha) {
		t.Fatalf("Argon2id and SHA-256 produced identical keys — KDF upgrade is a no-op")
	}
}

func TestNewEncryptedValue_EmptyPassphraseRejected(t *testing.T) {
	// v0.4+ API: NewEncryptedValue takes a passphrase string. An empty
	// passphrase is rejected to keep callers from accidentally
	// creating an EncryptedValue that would only use the SHA-256
	// fallback (which the caller may not realise).
	// v0.4+ API：NewEncryptedValue 接受 passphrase 字符串。空 passphrase
	// 被拒绝，避免调用方意外创建仅用 SHA-256 兜底的 EncryptedValue
	// （调用方可能没意识到）。
	_, err := NewEncryptedValue("")
	if err == nil {
		t.Fatalf("NewEncryptedValue(\"\") accepted; want error")
	}
}

func TestSealOpen_RoundTrip(t *testing.T) {
	enc, err := NewEncryptedValue("test-passphrase")
	if err != nil {
		t.Fatalf("NewEncryptedValue: %v", err)
	}

	plaintext := []byte(`{"user":"admin","password":"hunter2","host":"10.0.0.1"}`)

	sealed, err := enc.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Sealed output must start with the v0.4 magic byte 0x03.
	// 密文必须以 v0.4 magic 字节 0x03 开头。
	if len(sealed) == 0 || sealed[0] != magicEncryptedV2 {
		t.Fatalf("Seal output magic = 0x%02x, want 0x%02x", sealed[0], magicEncryptedV2)
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
	enc, _ := NewEncryptedValue("nonce-test")
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

func TestOpen_WrongPassphrase(t *testing.T) {
	// v0.4+ API: passphrase-based. Two EncryptedValue built with
	// different passphrases must fail to Open each other's Seals.
	// v0.4+ API：基于 passphrase。两个 EncryptedValue 用不同 passphrase
	// 构造时,Open 对方 Seal 必须失败。
	encA, _ := NewEncryptedValue("alpha")
	encB, _ := NewEncryptedValue("beta")

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
	enc, _ := NewEncryptedValue("tamper-test")

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
	// 应仍能经 Open 正确往返。
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
	enc, _ := NewEncryptedValue("any")
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
	encWithKey, _ := NewEncryptedValue("k")
	sealed, _ := encWithKey.Seal([]byte("secret"))

	encNoKey := (*EncryptedValue)(nil)
	_, err := encNoKey.Open(sealed)
	if err == nil {
		t.Fatalf("Open(encrypted, nil enc) returned nil error; want error")
	}
}

func TestOpen_TamperedMagicByteV4(t *testing.T) {
	// SECURITY: The magic byte (0x03) is bound to the ciphertext via
	// AAD. A tamperer who flips 0x03 → 0x01 (legacy no-AAD) must NOT
	// be able to recover the plaintext via the legacy code path (the
	// GCM auth tag was computed with AAD=[]byte{0x03}, not nil).
	//
	// 安全：magic 字节（0x03）通过 AAD 绑定到密文。攻击者把 0x03
	// 翻成 0x01（遗留无 AAD），不应能通过遗留代码路径还原明文
	// （GCM auth tag 是用 AAD=[]byte{0x03} 算的，不是 nil）。
	enc, _ := NewEncryptedValue("magic-tamper")
	plaintext := []byte(`{"user":"admin","pass":"hunter2"}`)
	sealed, err := enc.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Flip 0x03 → 0x01 (legacy encrypted, AAD=nil). The legacy
	// code path tries to decrypt with AAD=nil, but the GCM auth
	// tag was computed with AAD=[]byte{0x03}, so it must fail.
	// 翻成 0x01（遗留加密格式，AAD=nil）。遗留代码路径尝试用
	// AAD=nil 解密，但 GCM auth tag 是用 AAD=[]byte{0x03} 算
	// 的，所以必须失败。
	sealed[0] = magicEncryptedLegacy
	opened, err := enc.Open(sealed)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Open with flipped magic 0x%01x returned err=%v, want ErrDecryptFailed", magicEncryptedLegacy, err)
	}
	if bytes.Equal(opened, plaintext) {
		t.Fatalf("flipped magic 0x%01x returned the original plaintext — auth bypass!", magicEncryptedLegacy)
	}

	// Flip 0x03 → 0x00 (plaintext marker). The returned bytes must
	// not equal the original plaintext. / 翻成 0x00（明文标记）。
	// 返回的字节不能等于原始明文。
	sealed[0] = magicPlain
	opened, err = enc.Open(sealed)
	if err != nil {
		// An error is fine — what matters is no plaintext leak.
		// 出错也可——重要的是没有明文泄露。
		return
	}
	if bytes.Equal(opened, plaintext) {
		t.Fatalf("flipped magic 0x%01x returned the original plaintext — auth bypass!", magicPlain)
	}
}

func TestOpen_LegacyV3EncryptedFormat_Readable(t *testing.T) {
	// Forward compat: a v0.3.0 DB written with magic=0x01 and AAD=nil
	// must still be readable on a v0.4+ build via the SHA-256 path
	// inside EncryptedValue. This is regression protection for
	// existing v0.3.0 DBs.
	//
	// 向后兼容：v0.3.0 时代 magic=0x01 + AAD=nil 写入的 DB 在 v0.4+
	// 构建中通过 EncryptedValue 内置的 SHA-256 路径仍能读出。这是对
	// 已有 v0.3.0 DB 的回归保护。
	passphrase := "legacy"
	enc, _ := NewEncryptedValue(passphrase)
	plaintext := []byte(`{"legacy":true}`)

	// Hand-craft a v0.3.0-format value: magic=0x01, AAD=nil, sealed
	// with the SHA-256 key (not the Argon2id key — that would make
	// 0x01 indistinguishable from 0x03 on the wire).
	// 手工构造 v0.3.0 格式：magic=0x01, AAD=nil，用 SHA-256 key 加密
	// （不用 Argon2id key —— 那会让 0x01 在线缆上和 0x03 不可区分）。
	nonce := make([]byte, nonceSize)
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	legacyBlock, err := legacyCipher(enc.shaKey)
	if err != nil {
		t.Fatalf("legacyCipher: %v", err)
	}
	ct := legacyBlock.Seal(nil, nonce, plaintext, nil)
	sealed := append([]byte{magicEncryptedLegacy}, append(nonce, ct...)...)

	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open(legacy 0x01): %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("legacy roundtrip mismatch: got %q want %q", opened, plaintext)
	}
}

func TestOpen_LegacyV3_1EncryptedFormat_Readable(t *testing.T) {
	// Forward compat: a v0.3.1+ DB written with magic=0x02, AAD=magic,
	// SHA-256 key must still be readable on a v0.4+ build.
	//
	// 向后兼容：v0.3.1+ 时代 magic=0x02, AAD=magic, SHA-256 key 写入的
	// DB 在 v0.4+ 构建中仍能读出。
	passphrase := "v031"
	enc, _ := NewEncryptedValue(passphrase)
	plaintext := []byte(`{"v031":true}`)

	nonce := make([]byte, nonceSize)
	for i := range nonce {
		nonce[i] = byte(i + 100)
	}
	legacyBlock, err := legacyCipher(enc.shaKey)
	if err != nil {
		t.Fatalf("legacyCipher: %v", err)
	}
	ct := legacyBlock.Seal(nil, nonce, plaintext, []byte{magicEncrypted})
	sealed := append([]byte{magicEncrypted}, append(nonce, ct...)...)

	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open(v0.3.1 0x02): %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("v0.3.1 roundtrip mismatch: got %q want %q", opened, plaintext)
	}
}
