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
// The leading byte 0x00 marks "plaintext" (v0.2.x on-disk format).
// 0x01 marks v0.3.0 AES-GCM with SHA-256-derived key (AAD=nil).
// 0x02 marks v0.3.1+ AES-GCM with SHA-256-derived key + magic AAD.
// 0x03 marks v0.4+ AES-GCM with Argon2id-derived key + magic AAD.
//
// 起始字节 0x00 表示"明文"（v0.2.x 磁盘格式）。0x01 = v0.3.0 加密（SHA-256
// 派生 key，AAD=nil）。0x02 = v0.3.1+ 加密（SHA-256 派生 key + magic AAD）。
// 0x03 = v0.4+ 加密（Argon2id 派生 key + magic AAD）。
//
// Open() dispatches on magic to the right KDF, so v0.3.x projects remain
// readable on a v0.4+ build (existing 0x01/0x02 values decrypt via SHA-256).
// New writes always use 0x03 (Argon2id) once a project key is configured.
//
// Open() 按 magic 分发到对应 KDF，所以 v0.3.x 项目在 v0.4+ 构建中仍可读
// （旧的 0x01/0x02 值走 SHA-256 解密）。一旦配置了 project key，新写入一律用
// 0x03（Argon2id）。
//
// Key derivation:
//   - v0.3.x (magic 0x01, 0x02): SHA-256(passphrase) — single-pass, kept for
//     backward compat with existing encrypted DBs.
//   - v0.4+ (magic 0x03):       Argon2id(passphrase, salt) with OWASP-2024
//     parameters (time=3, memory=64 MiB, parallelism=4, salt=16 B,
//     key=32 B). Salt is per-DB (16 random bytes), cached inside
//     EncryptedValue so PutMany batches don't re-derive per row.
//
// 密钥派生：
//   - v0.3.x（magic 0x01、0x02）：SHA-256(passphrase) — 单遍哈希，保留以兼容
//     已有加密 DB。
//   - v0.4+（magic 0x03）：Argon2id(passphrase, salt)，OWASP-2024 参数
//     （time=3, memory=64 MiB, parallelism=4, salt=16 B, key=32 B）。salt
//     per-DB（16 字节随机），缓存在 EncryptedValue 内，PutMany 批量写入
//     不会逐行重派生。
//
// AAD on the magic byte (for 0x02 and 0x03) prevents bit-flip → "plaintext"
// confusion: any tamper that flips the magic byte is detected by the GCM
// auth tag as ErrDecryptFailed.
//
// magic 字节作为 AAD 绑定（0x02 和 0x03），防止位翻转 → "明文"混淆：任何篡改
// magic 字节都会被 GCM auth tag 检测为 ErrDecryptFailed。
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameter constants (OWASP 2024 minimums for at-rest use).
// Argon2id 参数常量（OWASP 2024 静态存储场景下限）。
//
// These are paid once per NewEncryptedValue call (key is then cached), so
// the per-Seal cost is one AES-GCM (negligible). Trade-off: opening a
// project takes ~100-500 ms on first Put, then is fast for the lifetime
// of the EncryptedValue.
//
// 这些成本只在 NewEncryptedValue 调用时付一次（key 之后缓存），所以每个
// Seal 的成本是一次 AES-GCM（可忽略）。权衡：项目首次 Put 时打开需
// 100-500 ms，之后 EncryptedValue 生命周期内都很快。
const (
	argonTime    uint32 = 3         // iterations / 迭代次数
	argonMemory  uint32 = 64 * 1024 // 64 MiB, in KiB
	argonThreads uint8  = 4         // parallelism
	argonSalt    int    = 16        // salt size in bytes
)

// Encryption format constants. / 加密格式常量。
const (
	// magicPlain marks a plaintext value (v0.2.x on-disk format).
	// magicPlain 标记明文值（v0.2.x 磁盘格式）。
	magicPlain byte = 0x00

	// magicEncryptedLegacy marks a v0.3.0 AES-256-GCM encrypted value
	// without AAD protection on the magic byte, SHA-256-derived key.
	// Kept readable for forward compatibility with v0.3.0 DBs.
	// magicEncryptedLegacy 标记 v0.3.0 时代 AES-256-GCM 加密值，
	// magic 字节未受 AAD 保护，SHA-256 派生 key。保留可读以兼容 v0.3.0 DB。
	magicEncryptedLegacy byte = 0x01

	// magicEncrypted marks a v0.3.1+ AES-256-GCM encrypted value with the
	// magic byte bound to the ciphertext via AAD, SHA-256-derived key.
	// magicEncrypted 标记 v0.3.1+ 时代 AES-256-GCM 加密值，magic 字节通过
	// AAD 绑定到密文，SHA-256 派生 key。
	magicEncrypted byte = 0x02

	// magicEncryptedV2 marks a v0.4+ AES-256-GCM encrypted value with
	// magic AAD and Argon2id-derived key (per-DB salt, OWASP 2024
	// parameters). All new Seals use this magic once a project key is
	// configured.
	// magicEncryptedV2 标记 v0.4+ 时代 AES-256-GCM 加密值，magic AAD，
	// Argon2id 派生 key（per-DB salt，OWASP 2024 参数）。一旦配置了
	// project key，所有新 Seal 一律使用此 magic。
	magicEncryptedV2 byte = 0x03

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

// DeriveKeyArgon2id derives a 32-byte AES-256 key from passphrase + salt
// using Argon2id with the package-level OWASP-2024 parameters. The cost
// is ~100-500 ms per call on a modern machine; cache the result in an
// EncryptedValue rather than calling per-Seal.
//
// DeriveKeyArgon2id 用包级 OWASP-2024 参数通过 Argon2id 从 passphrase
// + salt 派生 32 字节 AES-256 key。单次调用约 100-500 ms（现代机器）；
// 缓存在 EncryptedValue 中，不要每个 Seal 都重派生。
func DeriveKeyArgon2id(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keySize)
}

// DeriveKeySHA256 derives a 32-byte AES-256 key from passphrase via
// single-pass SHA-256. Used only for legacy magic bytes (0x01, 0x02)
// in Open() to keep v0.3.x DBs readable on v0.4+ builds. New Seals
// always use DeriveKeyArgon2id via EncryptedValue.
//
// DeriveKeySHA256 用单遍 SHA-256 从 passphrase 派生 32 字节 AES-256 key。
// 仅在 Open() 中用于遗留 magic 字节（0x01、0x02），以让 v0.3.x DB 在
// v0.4+ 构建中可读。新 Seal 一律通过 EncryptedValue 走 DeriveKeyArgon2id。
func DeriveKeySHA256(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// EncryptedValue bundles cipher.GCM plus both KDF keys (Argon2id + SHA-256)
// so Open() can dispatch on magic byte without re-deriving. Safe for
// concurrent use — cipher.GCM is documented as safe for concurrent use
// by multiple goroutines.
//
// EncryptedValue 把 cipher.GCM 与两套 KDF key（Argon2id + SHA-256）打包，
// Open() 按 magic 分发时无需重派生。并发安全——cipher.GCM 文档明确支持
// 多 goroutine 并发。
type EncryptedValue struct {
	gcm      cipher.AEAD
	argonKey []byte // for magic 0x03 (v0.4+ writes)
	shaKey   []byte // for magic 0x01, 0x02 (v0.3.x reads)
}

// NewEncryptedValue constructs an EncryptedValue from a passphrase. It
// generates a random 16-byte salt, derives the Argon2id key, and also
// pre-computes the SHA-256 key for legacy Open() of 0x01/0x02 values.
// The salt is not stored on the EncryptedValue — Argon2id keys are
// deterministic for a given (passphrase, salt) pair, so Open() of new
// 0x03 values re-derives from a fixed per-DB salt stored alongside
// the key. See workspace.Project for the salt persistence path.
//
// NewEncryptedValue 从 passphrase 构造 EncryptedValue。生成 16 字节随机
// salt，派生 Argon2id key，并预算 SHA-256 key 以用于遗留 0x01/0x02 的 Open。
// salt 不存于 EncryptedValue——Argon2id key 对 (passphrase, salt) 对是
// 确定性的，所以新 0x03 值的 Open 用 per-DB salt（与 key 一起持久化）
// 重派生。salt 持久化路径见 workspace.Project。
func NewEncryptedValue(passphrase string) (*EncryptedValue, error) {
	if passphrase == "" {
		return nil, errors.New("store: passphrase must not be empty")
	}
	salt := make([]byte, argonSalt)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("store: read salt: %w", err)
	}
	argonKey := DeriveKeyArgon2id(passphrase, salt)
	shaKey := DeriveKeySHA256(passphrase)

	block, err := aes.NewCipher(argonKey)
	if err != nil {
		return nil, fmt.Errorf("store: aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("store: cipher.NewGCM: %w", err)
	}
	return &EncryptedValue{gcm: gcm, argonKey: argonKey, shaKey: shaKey}, nil
}

// Seal encrypts plaintext and returns the on-disk value layout for v0.4+:
//
//	0x03 | 12-byte nonce | ciphertext + 16-byte tag
//
// Seal 加密明文，返回 v0.4+ 磁盘值布局：0x03 | 12 字节 nonce | 密文 + 16 字节 tag。
//
// The magic byte (0x03) is bound to the ciphertext as Additional
// Authenticated Data so a tamperer who flips 0x03 → 0x00/0x01/0x02 cannot
// trick Open() into returning ciphertext as plaintext or downgrading to
// the weaker SHA-256 KDF. AAD participates in the GCM auth tag, so any
// magic-byte flip is detected as ErrDecryptFailed.
//
// magic 字节（0x03）作为 Additional Authenticated Data 绑定到密文，攻击
// 者若把 0x03 翻成 0x00/0x01/0x02 无法骗过 Open() 把密文当明文返回，也
// 无法降级到较弱的 SHA-256 KDF。AAD 参与 GCM auth tag 计算，任何 magic
// 字节翻转都会触发 ErrDecryptFailed。
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
	// 通过 AAD 把 magic 字节绑定到密文。磁盘布局首字节是 magic；auth
	// tag 覆盖它。
	sealed := e.gcm.Seal(nonce, nonce, plaintext, []byte{magicEncryptedV2})

	out := make([]byte, 0, 1+len(sealed))
	out = append(out, magicEncryptedV2)
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
//   - 0x01 + nonce + ct + tag  — v0.3.0 encrypted, SHA-256 key, AAD=nil
//   - 0x02 + nonce + ct + tag  — v0.3.1+ encrypted, SHA-256 key, AAD=magic
//   - 0x03 + nonce + ct + tag  — v0.4+   encrypted, Argon2id key, AAD=magic
//
// Bit-flips on the magic byte of an AAD-protected value (0x02, 0x03) are
// detected as ErrDecryptFailed (the GCM auth tag covers the magic byte).
//
// Open 检查存储值，未加密或 e 为 nil 时返回明文；加密值解密失败
// 返回 ErrDecryptFailed（密钥错、值被篡改或截断）。
//
// 支持的磁盘格式：
//   - 0x00 + payload          — v0.2.x 明文（遗留）
//   - 0x01 + nonce + ct + tag — v0.3.0 加密，SHA-256 key，AAD=nil
//   - 0x02 + nonce + ct + tag — v0.3.1+ 加密，SHA-256 key，AAD=magic
//   - 0x03 + nonce + ct + tag — v0.4+   加密，Argon2id key，AAD=magic
//
// AAD 保护值的 magic 字节位翻转（0x02、0x03）会被检测为 ErrDecryptFailed
// （GCM auth tag 覆盖 magic 字节）。
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
	if magic != magicEncryptedLegacy && magic != magicEncrypted && magic != magicEncryptedV2 {
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

	// Pick the KDF key for this magic: SHA-256 for legacy (0x01, 0x02),
	// Argon2id for v0.4+ (0x03). NewEncryptedValue caches both so this
	// dispatch costs no KDF work.
	// 按 magic 选 KDF key：遗留（0x01、0x02）走 SHA-256，v0.4+（0x03）走
	// Argon2id。NewEncryptedValue 已缓存两套 key，此处分发不付 KDF 成本。
	key := e.shaKey
	if magic == magicEncryptedV2 {
		key = e.argonKey
	}
	// Re-bind cipher to the chosen key. NewEncryptedValue constructs gcm
	// from the Argon2id key; for legacy Open we need a separate cipher
	// bound to the SHA-256 key. This is cheap (no KDF, no I/O).
	// 用所选 key 重新绑定 cipher。NewEncryptedValue 用 Argon2id key 构造
	// gcm；遗留 Open 需要绑定到 SHA-256 key 的独立 cipher。这很便宜
	//（无 KDF，无 I/O）。
	var aead cipher.AEAD
	if magic == magicEncryptedV2 {
		aead = e.gcm
	} else {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("store: aes.NewCipher (legacy): %w", err)
		}
		aead, err = cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("store: cipher.NewGCM (legacy): %w", err)
		}
	}

	// AAD is the magic byte. For the legacy 0x01 format AAD=nil (the
	// v0.3.0 implementation); for 0x02 and 0x03 AAD=magic so any flip
	// on the magic byte is detected by the GCM auth tag.
	// AAD 是 magic 字节。遗留 0x01 格式 AAD=nil（v0.3.0 实现）；0x02 和
	// 0x03 格式 AAD=magic，magic 字节任何翻转都会被 GCM auth tag 检测。
	var aad []byte
	if magic != magicEncryptedLegacy {
		aad = []byte{magic}
	}
	plain, err := aead.Open(nil, nonce, ciphertext, aad)
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
