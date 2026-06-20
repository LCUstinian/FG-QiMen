# Security Policy

> GitHub recognises a SECURITY.md at the **repository root** (not
> under `docs/`) for the security advisory flow. The full
> security model is in [`docs/SECURITY.md`](docs/SECURITY.md);
> this file is the **GitHub-required** policy.

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.3.1+ (current) | ✅ |
| v0.2.x | Best-effort |
| v0.1.x | End-of-life |

## Reporting a Vulnerability

**Please open a GitHub Security Advisory** (private disclosure):

👉 [https://github.com/LCUstinian/FG-QiMen/security/advisories/new](https://github.com/LCUstinian/FG-QiMen/security/advisories/new)

Or email the maintainer directly (see `.github/CODEOWNERS` when
present). **Do not file a public issue for security-sensitive
findings.**

### What to include

1. **Affected version** (commit SHA or release tag).
2. **Affected component** (plugin, core, output, etc.).
3. **Reproduction steps** — minimal command + input.
4. **Impact** (RCE / credential leak / DoS / etc.).
5. **Suggested fix** (optional but appreciated).

### Response SLA

| Severity | Acknowledgement | Patch |
|----------|-----------------|-------|
| **Critical** (RCE / auth bypass) | 24 hours | 7 days |
| **High** (cred leak / priv esc) | 3 days | 30 days |
| **Medium** (DoS / info leak) | 7 days | 90 days |
| **Low** (cosmetic / hardening) | 14 days | Best-effort |

## Security Model (TL;DR)

Full details in [`docs/SECURITY.md`](docs/SECURITY.md). The HARD
rules:

- **No post-auth action.** Credential tests write `user/pass` to
  `creds.txt` and stop. No `ssh.NewSession`, no `Exec`, no shell.
- **No CVE-based exploitation.** No EternalBlue, SMBGhost, Redis
  RCE, JDWP, RMI, WebLogic deserialization.
- **No reverse/bind/SOCKS5 server.** FG-QiMen is a *client*.

These are enforced by:

- A `// HARD:` comment block on every authenticator.
- A CI lint that greps for `ssh.NewSession` / `.Shell(` / `os/exec`
  in credential + plugin paths (`scripts/lint-hard-rule.sh`).
- Coverage gate (`internal/` ≥ 50% statement coverage).
- cosign-signed + SBOM-attached releases.

## Encryption

- At-rest bbolt values are AES-256-GCM (post-v0.3.1).
- Per-value magic-byte AAD binding prevents bit-flip → "plaintext"
  confusion.
- Key derivation: v0.3.x used SHA-256(passphrase); v0.4+ uses
  **Argon2id** with OWASP-2024 parameters (time=3, memory=64 MiB,
  parallelism=4, salt=16 B). Old SHA-256 KDF stays in `Open()` so
  v0.3.x DBs remain readable. See `internal/store/crypto.go` for
  the magic-byte dispatch (`0x01`/`0x02` → SHA-256, `0x03` →
  Argon2id).

## Out-of-Scope

- Network scanners that require root / raw sockets (FG-QiMen uses
  `net.Dial` only — no raw packets).
- Binary exploitation of FG-QiMen itself (the project is single-
  static-binary; the attack surface is the bbolt file, the config
  files, and the env vars — all governed by the HARD rule).

## Credits

The HARD no-exploit policy is the project's primary competitive
moat in the Chinese scanner landscape. It is documented in the
README under "Hard rule: NO exploit code" and in
[`docs/SECURITY.md`](docs/SECURITY.md).
