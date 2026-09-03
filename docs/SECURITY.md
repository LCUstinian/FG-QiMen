# Security Model

> FG-QiMen is a scanner + credential tester, not an attack tool. This
> document is the contract between the project and its operators.

## Threat model

**In scope**:
- Authorized internal network reconnaissance (asset discovery, weak
  password detection, port inventory).
- Discovering exposed services that should be firewalled.
- Audit-friendly credential spraying that records hits but takes
  **no post-auth action** (no command execution, no file write, no
  backdoor).

**Out of scope** (and explicitly NOT in the code):
- Exploitation of any vulnerability (CVE-based RCE, deserialization,
  auth bypass).
- Persistence / backdooring / lateral movement.
- Post-credential automation (running commands on a successfully
  authenticated host, dropping webshells, writing SSH keys, etc.).

## HARD rules

The project enforces the following invariants via code review and
`// HARD:` comments at the top of every authenticator file:

1. **No post-auth action.** When `Credential()` returns a hit, the
   pipeline writes `user / pass` to `creds.txt` and **stops**. No
   `ssh.NewSession`, no `Exec`, no `Shell`, no webshell, no file
   write — neither in the code nor as a configurable option.

2. **No CVE-based exploitation.** No EternalBlue, SMBGhost, Redis
   RCE, JDWP, RMI, deserialization attacks. The code does not
   contain these paths and we will not merge them.

3. **No reverse / bind / SOCKS5 server.** FG-QiMen is a *client* of
   services, not a server that gives the operator a foothold.

## What we will never ship / 永远不会包含

The following are explicitly excluded from v0.1 and all future
versions: / v0.1 及所有未来版本明确排除以下内容：

- ❌ MS17-010 (EternalBlue) detection or exploitation
  ❌ MS17-010（永恒之蓝）探测与利用
- ❌ SMBGhost (CVE-2020-0796)
- ❌ Redis write SSH key / cron / webshell / master-slave RCE
  ❌ Redis 写公钥 / 写计划任务 / 写 WebShell / 主从复制 RCE
- ❌ SSH post-auth command execution (no `ssh.NewSession` / `Exec` /
  `Shell` in code)
  ❌ SSH 认证后自动执行命令（代码中**不存在** `ssh.NewSession` /
  `Exec` / `Shell`）
- ❌ MS17-010 shellcode injection / SMB shellcode
  ❌ MS17010 ShellCode 注入
- ❌ JDWP exploitation
- ❌ RMI / JBoss / WebLogic deserialization RCE
  ❌ RMI / JBoss / WebLogic 反序列化 RCE
- ❌ Any CVE-based remote code execution
  ❌ 任何 CVE-based 的远程代码执行
- ❌ Reverse / bind shell / SOCKS5 server (post-exploitation)
  ❌ 反弹 Shell / 正向 Shell / SOCKS5 代理服务端（后渗透）
- ❌ Any post-credential automation (write files, run commands,
  plant backdoors)
  ❌ 凭据成功后的任何自动化操作

### What credential testing means here / 爆破的严格定义

✅ **Allowed / 允许**: try a list of user:pass combinations against
SSH / RDP / FTP / MySQL / Redis / SMB / etc. via the standard
authentication handshake.

✅ **允许**：用 user:pass 字典对 SSH / RDP / FTP / MySQL / Redis /
SMB 等服务做标准认证握手尝试。

✅ **On hit / 命中时**: write `user / pass` to `creds.txt` and stop.
Nothing else. The plugin function returns a `*Result` with `Cred`
set; the pipeline writes it to disk; no `Session.Exec` / no webshell
/ no command runs.

✅ **命中时**：把 `user / pass` 写入 `creds.txt` 然后停止。插件
函数返回带 `Cred` 字段的 `*Result`；管线写盘后即终止；不调用
`Session.Exec`、不上 WebShell、不执行任何命令。

❌ **Never / 严禁**: any post-auth action — running remote commands,
writing remote files, planting persistence, etc.

❌ **严禁**：任何认证后动作——执行远程命令、写远程文件、植入
持久化等。

**Scanner + credential tester = discovery tool. Exploitation =
attack tool. FG-QiMen is only the former.**

**扫描器 + 凭据测试器 = 探测面工具。漏洞利用 = 攻击面工具。
FG-QiMen 只做前者。**

## Encryption at rest

Project mode persists results to a bbolt DB at
`runs/projects/<name>/fg.db`. By default the values are written
plaintext (the seen-set bucket is always plaintext — it stores only
non-secret SHA-1 hashes).

To enable encryption, set `FG_QIMEN_PROJECT_KEY`:

```bash
# Generate a strong key:
export FG_QIMEN_PROJECT_KEY=$(openssl rand -hex 32)

# Run the scan; new writes are AES-256-GCM encrypted:
fg-qimen -p myproject -H 10.0.0.0/24 -mode linked
```

Key derivation:
  - v0.3.x (legacy, still readable): SHA-256(passphrase) → 32-byte key.
  - v0.4+ (current, new writes): Argon2id(passphrase, salt) with
    OWASP-2024 parameters (time=3, memory=64 MiB, parallelism=4,
    salt=16 B). The salt is per-DB and cached in `EncryptedValue`,
    so the cost is paid once at project open.

The on-disk format is documented in `internal/store/crypto.go`:

```
+--------+------------------+--------------------+
| magic  |      nonce       | ciphertext + tag   |
| 1 byte |  12 bytes (GCM)  |  N bytes           |
+--------+------------------+--------------------+
```

- `0x00` magic: plaintext (legacy v0.2.x)
- `0x01` magic: v0.3.0 encrypted, SHA-256 key, AAD=nil (legacy)
- `0x02` magic: v0.3.1+ encrypted, SHA-256 key, AAD=magic (legacy, readable)
- `0x03` magic: v0.4+   encrypted, Argon2id key, AAD=magic (current)

`Open()` dispatches on the magic byte to the right KDF, so v0.3.x
DBs remain readable on v0.4+ builds; new writes always use `0x03`
once `FG_QIMEN_PROJECT_KEY` is set.

The magic byte is bound to the ciphertext via GCM AAD. Bit-flips
on the magic byte are detected as `ErrDecryptFailed` rather than
silently converting an encrypted row to "plaintext" (P1.4 of the
audit roadmap).

## Credential redaction

- `fgqm_creds.txt` is **always cleartext** — this is the operator's
  working file.
- `fgqm_result.txt`, `fgqm_result.json`, and `fgqm_result.csv` are redacted to
  a length-only fingerprint by default (e.g. `admin / ********
  (len=8)`). Pass `--show-creds` to embed cleartext in those files
  too.
- TUI / stderr mirror the same redaction gate.

## Input validation

- Hosts are expanded via `types.ExpandTargets` which validates CIDR
  / range / hostname syntax.
- The `--host` flag value is not shell-evaluated; subprocesses
  (system `ping`, `arp`) are invoked with `exec.Command` so command
  injection is impossible.
- Host values starting with `-` are **rejected** (the system `ping`
  binary would interpret them as flags). P1.9 of the audit roadmap.
- Workspace project names go through `workspace.ValidateProjectName`
  (allow-list, no `..`, no absolute paths).
- Output paths go through `cmd/scan.go:safeOutputPath` which
  enforces the project-root boundary unless
  `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1` is set.

## SSH host key verification

By default, SSH credential spraying requires a populated
`known_hosts` file. The flag is `--known-hosts <path>`. If neither
the flag nor a default file is set, the spray **refuses to run**
with an explicit error to stderr.

To opt out (NOT recommended in production):

```bash
fg-qimen -p myproject --insecure-ssh -H 10.0.0.0/24 -mode linked
```

## TLS verification

HTTPS probes verify the certificate chain + hostname by default.
The flag `--insecure-tls` disables verification for known-trusted
self-signed test environments.

## Known limitations

- **No SSRF / loopback guard.** A scan of `127.0.0.0/8` or
  `169.254.0.0/16` (link-local) is fully allowed. This is by design
  — operators scan loopback on purpose during testing. A future
  `--no-loopback` flag could add an opt-in warning.
- **No DNS-rebinding protection.** A target that resolves to a
  different IP between resolve and connect will be scanned at the
  second IP. Standard Go `net.Resolver` behaviour; not a
  scanner-specific concern.
- **Memory disclosure of cleartext.** Credential strings live in
  Go-managed heap memory; a process memory dump can recover them
  pre-GC. To minimise: pass `--insecure-ssh=false`,
  `--no-batch=false`, set `FG_QIMEN_PROJECT_KEY`, and prefer file
  dictionaries over inline `-u`/`-P` for shared multi-user hosts.

## Reporting vulnerabilities

Please open a GitHub Security Advisory (private disclosure). Do not
file a public issue for security-sensitive findings.
