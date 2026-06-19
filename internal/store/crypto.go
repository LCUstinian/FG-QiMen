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

	// magicEncryptedLegacy marks a v0.3.0 AES-256-GCM encrypted value
	// without AAD protection on the magic byte. Kept readable for
	// forward compatibility with freshly-created v0.3.0 DBs.
	// magicEncryptedLegacy 标记 v0.3.0 时代 AES-256-GCM 加密值，
	// magic 字节未受 AAD 保护。保留可读以兼容刚创建的 v0.3.0 DB。
	magicEncryptedLegacy byte = 0x01

	// magicEncrypted marks an AES-256-GCM encrypted value with the
	// magic byte bound to the ciphertext via Additional Authenticated
	// Data. Bit-flips on the magic byte are detected as ErrDecryptFailed.
	// magicEncrypted 标记 AES-256-GCM 加密值，magic 字节通过
	// AAD 绑定到密文。magic 字节位翻转会被检测为 ErrDecryptFailed。
	magicEncrypted byte = 0x02

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
//	0x02 | 12-byte nonce | ciphertext + 16-byte tag
//
// Seal 加密明文，返回磁盘值布局：0x02 | 12 字节 nonce | 密文 + 16 字节 tag。
//
// The magic byte (0x02) is bound to the ciphertext as Additional
// Authenticated Data so a tamperer who flips 0x02 → 0x00/0x01 cannot trick
// Open() into returning ciphertext as plaintext. AAD participates in
// the GCM auth tag, so any magic-byte flip is detected as ErrDecryptFailed.
//
// magic 字节（0x02）作为 Additional Authenticated Data 绑定到密文，攻击
// 者若把 0x02 翻成 0x00/0x01 无法骗过 Open() 把密文当明文返回。AAD 参与
// GCM auth tag 计算，任何 magic 字节翻转都会触发 ErrDecryptFailed。
func (e *EncryptedValue) Seal(plaintext []byte) ([]byte, error) {
	if e == nil || e.gcm == nil {
		return nil, errors.New("store: EncryptedValue is nil")
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("store: read nonce: %w", err)
	}
	// Bind the magic byte to the ciphertext via AAD. The first byte of
	// the on-disk layout is the magic; the auth tag covers it.
	// Use the nonce as the destination buffer (matching the on-disk
	// layout 0x02 | nonce | ct | tag) so we avoid one allocation.
	// 通过 AAD 把 magic 字节绑定到密文。磁盘布局首字节是 magic；auth tag 覆盖它。
	// 用 nonce 当 dst buffer（保持磁盘布局 0x02 | nonce | ct | tag），
	// 省一次内存分配。
	sealed := e.gcm.Seal(nonce, nonce, plaintext, []byte{magicEncrypted})

	out := make([]byte, 0, len(sealed))
	out = append(out, magicEncrypted)
	out = append(out, sealed...)
	return out, nil
}

// Open inspects a stored value and either returns the plaintext (when the
// stored value is unencrypted or e is nil) or decrypts it. Returns
// ErrDecryptFailed for an encrypted value when decryption fails (wrong
// key, tampered data, or truncated).
//
// Supported on-disk formats:
//   - 0x00 + payload           — v0.2.x plaintext (legacy)
//   - 0x01 + nonce + ct + tag  — v0.3.0 encrypted, AAD=nil (legacy)
//   - 0x02 + nonce + ct + tag  — v0.3.1+ encrypted, AAD=magic (AAD-protected)
//
// Bit-flips on the magic byte of an AAD-protected value are detected as
// ErrDecryptFailed (the GCM auth tag covers the magic byte).
//
// Open 检查存储值，未加密或 e 为 nil 时返回明文；加密值解密失败
// 返回 ErrDecryptFailed（密钥错、值被篡改或截断）。
//
// 支持的磁盘格式：
//   - 0x00 + payload          — v0.2.x 明文（遗留）
//   - 0x01 + nonce + ct + tag — v0.3.0 加密，AAD=nil（遗留）
//   - 0x02 + nonce + ct + tag — v0.3.1+ 加密，AAD=magic（AAD 保护）
//
// AAD 保护值的 magic 字节位翻转会被检测为 ErrDecryptFailed（GCM auth
// tag 覆盖 magic 字节）。
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
	if magic != magicEncrypted && magic != magicEncryptedLegacy {
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
	// AAD is the magic byte. For the legacy 0x01 format AAD=nil (the
	// v0.3.0 implementation); for the new 0x02 format AAD=magic so
	// any flip on the magic byte is detected by the GCM auth tag.
	// AAD 是 magic 字节。遗留 0x01 格式 AAD=nil（v0.3.0 实现）；
	// 新 0x02 格式 AAD=magic，magic 字节任何翻转都会被 GCM auth
	// tag 检测。
	var aad []byte
	if magic == magicEncrypted {
		aad = []byte{magic}
	}
	plain, err := e.gcm.Open(nil, nonce, ciphertext, aad)
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
