// crypto.go — value-level AES-256-GCM encryption for Store Put operations.
//
// crypto.go — Store Put 操作的值级 AES-256-GCM 加密。
//
// bbolt v1.4.3 has no built-in page-level encryption, so we encrypt the JSON
// payload before Put and decrypt on read. The on-disk format for an encrypted
// value is:
//
//	bbolt v1.4.3 没有内建页级加密，所以在 Put 前加密 JSON payload，读时解密。
//
// 加密值的磁盘格式是：
//
//	+--------+------------------+--------------------+
//	| magic  |      nonce       | ciphertext + tag   |
//	| 1 byte |  12 bytes (GCM)  |  N bytes           |
//	+--------+------------------+--------------------+
//
// The leading byte 0x01 marks "encrypted". A leading 0x00 (or absence, treated
// as 0x00) marks "plaintext" — this lets us coexist with un-encrypted DBs
// from v0.2.x for forward compatibility until the user runs with
// FG_QIMEN_PROJECT_KEY set, after which new writes are encrypted.
//
// 起始字节 0x01 表示"已加密"。0x00（或缺失，按 0x00 处理）表示"明文"——
// 这样 v0.2.x 时代未加密的 DB 仍可向后兼容读取；一旦用户设了
// FG_QIMEN_PROJECT_KEY，后续写入即加密。
//
// Key derivation: SHA-256 of the passphrase → 32-byte AES-256 key. This is
// intentionally simple — operators pick a strong passphrase; we don't try
// to be a password manager. Argon2id would be heavier without materially
// improving security against a stolen-disk-thumbnail attack.
//
// 密钥派生：passphrase 的 SHA-256 → 32 字节 AES-256 密钥。刻意保持简单——
// 操作员选强密码即可；我们不试图做密码管理器。Argon2id 会更重但不能实质
// 提升对"偷硬盘镜像"攻击的防御。
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// Encryption format constants. / 加密格式常量。
const (
	// magicPlain marks a plaintext value (v0.2.x on-disk format).
	// magicPlain 标记明文值（v0.2.x 磁盘格式）。
	magicPlain byte = 0x00

	// magicEncrypted marks an AES-256-GCM encrypted value.
	// magicEncrypted 标记 AES-256-GCM 加密值。
	magicEncrypted byte = 0x01

	// nonceSize is the GCM standard nonce size (12 bytes).
	// nonceSize 是 GCM 标准 nonce 长度（12 字节）。
	nonceSize = 12

	// keySize is the AES-256 key size (32 bytes).
	// keySize 是 AES-256 密钥长度（32 字节）。
	keySize = 32
)

// ErrDecryptFailed is returned when decryption fails — typically because the
// key is wrong or the value was tampered with.
//
// ErrDecryptFailed 在解密失败时返回——通常是密钥错误或值被篡改。
var ErrDecryptFailed = errors.New("store: decrypt failed (wrong key or tampered value)")

// DeriveKey hashes a passphrase to a 32-byte AES-256 key using SHA-256.
// The returned slice is safe to use directly with cipher.NewGCM.
//
// DeriveKey 用 SHA-256 把 passphrase 哈希成 32 字节 AES-256 密钥。
// 返回的 slice 可直接用于 cipher.NewGCM。
func DeriveKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// EncryptedValue bundles a cipher.GCM with a derived key so callers can
// encrypt/decrypt without re-deriving on every call. Safe for concurrent
// use — cipher.GCM is documented as safe for concurrent use by multiple
// goroutines.
//
// EncryptedValue 把 cipher.GCM 与派生密钥打包，调用方无需每次都重新派生。
// 并发安全——cipher.GCM 文档明确支持多 goroutine 并发。
type EncryptedValue struct {
	gcm cipher.AEAD
}

// NewEncryptedValue constructs an EncryptedValue from a 32-byte key.
// Use DeriveKey() to convert a passphrase to a key.
//
// NewEncryptedValue 从 32 字节密钥构造 EncryptedValue。
// 用 DeriveKey() 把 passphrase 转为密钥。
func NewEncryptedValue(key []byte) (*EncryptedValue, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("store: key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("store: aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("store: cipher.NewGCM: %w", err)
	}
	return &EncryptedValue{gcm: gcm}, nil
}

// Seal encrypts plaintext and returns the on-disk value layout:
//
//	0x01 | 12-byte nonce | ciphertext + 16-byte tag
//
// Seal 加密明文，返回磁盘值布局：0x01 | 12 字节 nonce | 密文 + 16 字节 tag。
func (e *EncryptedValue) Seal(plaintext []byte) ([]byte, error) {
	if e == nil || e.gcm == nil {
		return nil, errors.New("store: EncryptedValue is nil")
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("store: read nonce: %w", err)
	}
	// Seal appends ciphertext+tag to nonce and returns the combined slice.
	// Seal 把密文+tag 追加到 nonce 后并返回合并后的 slice。
	sealed := e.gcm.Seal(nonce, nonce, plaintext, nil)

	out := make([]byte, 0, 1+len(sealed))
	out = append(out, magicEncrypted)
	out = append(out, sealed...)
	return out, nil
}

// Open inspects a stored value and either returns the plaintext (when the
// stored value is unencrypted or e is nil) or decrypts it (when magic byte
// is 0x01). Returns ErrDecryptFailed for a 0x01 value when decryption
// fails (wrong key, tampered data, or truncated).
//
// Open 检查存储值，未加密或 e 为 nil 时返回明文；magic 字节为 0x01 时
// 解密。0x01 值解密失败返回 ErrDecryptFailed（密钥错、值被篡改或截断）。
func (e *EncryptedValue) Open(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return stored, nil
	}
	magic := stored[0]
	if magic == magicPlain {
		// Legacy plaintext value: return as-is, strip the leading 0x00 if
		// present. / 遗留明文值：原样返回；如有前导 0x00 则去掉。
		return stored[1:], nil
	}
	if magic != magicEncrypted {
		// Unknown magic — treat as opaque plaintext to preserve forward
		// compatibility with future format additions.
		// 未知 magic——按不透明明文处理，保持对将来格式扩展的向前兼容。
		return stored, nil
	}
	if e == nil || e.gcm == nil {
		return nil, errors.New("store: value is encrypted but no key is configured")
	}
	if len(stored) < 1+nonceSize+e.gcm.Overhead() {
		return nil, ErrDecryptFailed
	}
	nonce := stored[1 : 1+nonceSize]
	ciphertext := stored[1+nonceSize:]
	plain, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plain, nil
}

// SealPlain wraps plaintext with the legacy magic byte 0x00 so it round-
// trips through Open() correctly. This is used when no key is configured.
//
// SealPlain 给明文加 0x00 magic 以便经 Open() 正确往返。无密钥配置时使用。
func SealPlain(plaintext []byte) []byte {
	out := make([]byte, 0, 1+len(plaintext))
	out = append(out, magicPlain)
	out = append(out, plaintext...)
	return out
}
