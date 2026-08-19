# Security Policy

> GitHub recognises a `SECURITY.md` at the **repository root** for the
> security advisory flow. The full security model is in
> [`docs/SECURITY.md`](docs/SECURITY.md); this file is the
> **GitHub-required** policy.

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

Full details in [`docs/SECURITY.md`](docs/SECURITY.md). The three
HARD rules:

- **No post-auth action.** Credential tests write `user/pass` to
  `creds.txt` and stop. No `ssh.NewSession`, no `Exec`, no shell.
- **No CVE-based exploitation.** No EternalBlue, SMBGhost, Redis
  RCE, JDWP, RMI, WebLogic deserialization.
- **No reverse/bind/SOCKS5 server.** FG-QiMen is a *client*.

Enforced by `// HARD:` comments on every authenticator, a CI lint
that greps for `ssh.NewSession` / `.Shell(` / `os/exec` in credential
+ plugin paths, and cosign-signed + SBOM-attached releases.

## Encryption (TL;DR)

Project DBs (`runs/projects/<name>/fg.db`) are AES-256-GCM at rest
when `FG_QIMEN_PROJECT_KEY` is set. Key derivation: v0.3.x used
SHA-256 (legacy, still readable); v0.4+ uses Argon2id with
OWASP-2024 parameters. Magic-byte AAD binding prevents bit-flip
→ "plaintext" confusion. Full on-disk format and KDF dispatch in
[`docs/SECURITY.md`](docs/SECURITY.md) and `internal/store/crypto.go`.

## Out-of-Scope

- Network scanners that require root / raw sockets (FG-QiMen uses
  `net.Dial` only — no raw packets).
- Binary exploitation of FG-QiMen itself (the project is single-
  static-binary; the attack surface is the bbolt file, the config
  files, and the env vars — all governed by the HARD rule).