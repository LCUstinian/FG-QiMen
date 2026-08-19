# FG-QiMen

> A pipeline scanner with project workspaces · 一个带项目工作区的管道扫描器

FG-QiMen is a pure CLI scanner that decouples the **port scanner (producer)** from
the **plugin workers (consumer)** via a Go channel pipeline. It supports three
run modes (`scan` / `crack` / `linked`) and two work modes (ephemeral oneshot
vs persistent project workspace with bbolt state).

FG-QiMen 是一个**纯 CLI 扫描器**，通过 Go channel 管道把**端口扫描（producer）**
和**插件 worker（consumer）**解耦。支持三种运行模式（`scan` / `crack` / `linked`）
和两种工作模式（即扫即走 vs 带 bbolt 状态的增量项目工作区）。

```
┌─ FG-QIMEN v0.3 ──── project: corp-intranet ──── mode: linked ─┐
│ ⟳ Scanning... elapsed 00:01:23  throughput 142 pps             │
├──────────────────────┬──────────────────────────────────────────┤
│ Targets              │ Live Events                              │
│   alive       18     │   ◆ 192.168.1.1:22  [ssh]   OpenSSH 8.9 │
│   ports      142     │   ◆ 192.168.1.1:80  [http]  title=...   │
│   results     42     │   ⚠ 192.168.1.1:22  [ssh/cred] admin/...│
│   creds        3     │   ✗ 192.168.1.5:3306 timeout            │
│   errors       7     │                                          │
├──────────────────────┴──────────────────────────────────────────┤
│ [q] quit                                                       │
└────────────────────────────────────────────────────────────────┘
```

---

## TL;DR

```bash
# ephemeral scan / 即扫即走
fg-qimen -H 192.168.1.0/24

# persistent project with bbolt state / 项目模式 + bbolt 状态
fg-qimen -p corp -H 10.0.0.0/24 -mode linked

# resume a paused project / 续传项目
fg-qimen resume -p corp

# manage projects / 项目管理
fg-qimen projects list
```

---

## Hard rule: scanner, not attack tool / 硬性原则：探测，不攻击

FG-QiMen **deliberately does not include any vulnerability-exploitation
code**. The scanner tries standard authentication handshakes against
known services; on a hit, the credential is written to `creds.txt`
and the run stops — no `Session.Exec`, no webshell, no persistence.
The full no-exploit contract (the explicit list of forbidden
capabilities) lives in [`docs/SECURITY.md`](docs/SECURITY.md).

FG-QiMen **故意不包含任何漏洞利用代码**。扫描器用标准认证握手测试已知
服务；命中时凭据写入 `creds.txt` 即终止——不执行 `Session.Exec`、不上
WebShell、不植入持久化。完整的"不做漏洞利用"契约（明确禁止的能力清单）
见 [`docs/SECURITY.md`](docs/SECURITY.md)。

---

## Features / 功能特性

### Architecture / 架构

- **Pipeline decoupling / 管道解耦**: port scan (producer) → `chan ScanItem` → plugin workers (consumer). All stages honor `context.Context` for cancellation.
  **管道解耦**：端口扫描 (producer) → `chan ScanItem` → 插件 worker (consumer)。所有阶段遵循 `context.Context` 实现取消。
- **Three run modes / 三种运行模式**: `scan` / `crack` / `linked` — see [CLI reference](#cli-reference--cli-参考).
- **Project workspace / 项目工作区**: each project gets its own directory + bbolt DB.
  **项目工作区**：每个项目独立目录 + bbolt DB。
- **Incremental tracking / 增量追踪**: SHA-1-based dedup with optional bbolt persistence; `--resume` reloads the seen-set.
  **增量追踪**：基于 SHA-1 的去重，可选 bbolt 持久化；`--resume` 重载已见集合。
- **TUI / 终端 UI**: Bubbletea + Lipgloss cyberpunk theme (green / amber / red on black); auto-fallback to plain text on non-TTY.
  **TUI**：Bubbletea + Lipgloss 赛博朋克配色（黑底绿/琥珀/红）；非 TTY 自动降级纯文本。

For the full architecture write-up (pipeline diagrams, plugin contract,
session bag wiring), see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

完整架构文档（管线图、插件契约、Session 装配）见
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)。

### Output / 输出

- `result.txt` — human-readable lines
- `result.json` — NDJSON (one JSON object per line)
- `result.csv` — RFC 4180 one row per result
- `creds.txt` — credential hits (always cleartext; the operator's working file)
- `rdp.json` / `rdp.txt` — RDP deep fingerprint (hostname, build, NLA flag, OS)

### Plugins (44 plugins / authenticators) / 插件（44 个）

| Plugin | Default ports | Identify | Credential |
|---|---|---|---|
| `ssh` | 22, 2222, 2200, 22222 | ✅ | ✅ (password only, no Session/Exec) |
| `http` | 80, 443, 8080, 8443, 8000, 8888 | ✅ | – (v0.2+) |
| `webtitle` | 80, 443, 8080, 8443 | ✅ (FingerprintHub 3139 rules + favicon) | – |
| `redis` | 6379, 6380 | ✅ (PING / PONG) | ✅ (RESP AUTH, 4/4 unit tests) |
| `mongodb` | 27017, 27018 | ✅ (OP_MSG hello) | ✅ (SCRAM-SHA-256 via OP_MSG) |
| `postgresql` | 5432, 5433, 5434 | ✅ (StartupMessage) | ✅ (lib/pq via `db.PingContext`) |
| `mssql` | 1433, 1434, 2433 | ✅ (TDS via go-mssqldb) | ✅ (TDS Login7 via go-mssqldb) |
| `smb` | 445, 139 | ✅ (SMB magic) | ✅ (SMB2 Session Setup NTLMv2 via go-smb2) |
| `smtp` | 25, 465, 587, 2525 | ✅ (EHLO via net/smtp) | – (v0.2+) |
| `snmp` | 161, 162 | ✅ (sysDescr.0 raw) | – (v0.2+) |
| `ldap` | 389, 636 | ✅ (BindRequest + SearchRequest) | – (v0.2+) |
| `memcached` | 11211, 11212 | ✅ (text "version\r\n") | ✅ (ASCII "auth" probe, 4/4 unit tests) |
| `elasticsearch` | 9200, 9300 | ✅ (HTTP GET /) | ✅ (HTTP Basic auth probe) |
| `rdp` | 3389 | ✅ (TPKT/X.224/MCS 4-step, extracts hostname+build+NLA) | – (v0.3+, NLA cred test is explicit deferral) |
| `vnc` | 5900-5905 | ✅ (RFB 003.x banner) | ✅ (RFB handshake + DES challenge via go-vnc) |
| `telnet` | 23, 2323 | ✅ (IAC-stripped banner) | ✅ (IAC + prompt + user/pass flow, hand-rolled) |
| `oracle` | 1521, 1526, 2483 | ✅ (TNS Connect/Accept probe) | ✅ (TNS handshake via go-ora) |
| `winrm` | 5985, 5986 | ✅ (GET /wsman probe) | ✅ (HTTP Basic + WSMan SOAP) |
| `pop3` | 110, 995 | ✅ (+OK greeting) | ✅ (RFC 1939 USER/PASS) |
| `imap` | 143, 993 | ✅ (`* OK` greeting) | ✅ (RFC 3501 LOGIN) |
| `socks5` | 1080 | ✅ (SOCKS5 VER 5 greeting) | ✅ (RFC 1928/1929 user/pass) |
| `ldap` (cred) | 389, 636 | – (already Identify via `ldap` row above) | ✅ (simple bind via go-ldap/ldap/v3) |
| `snmp` | 161 (UDP) | ✅ (v2c GET sysDescr) | ✅ (community string via gosnmp; UDP) |
| `rsync` | 873, 8873 | ✅ (`@RSYNCD:` greeting) | ✅ (USERNAME + MD5 challenge-response) |
| `docker` | 2375, 2376 | ✅ (GET /_ping + /info version) | ✅ (HTTP Basic to /images/json) |
| `rabbitmq` | 5672 | ✅ (AMQP 0-9-1 protocol header + Start) | ✅ (AMQP PLAIN via raw frame) |
| `modbus` | 502 | ✅ (Read Device Identification) | ✅ (Read Device ID only; no write to coils/registers) |
| `ipmi` | 623 (UDP) | ✅ (RMCP+ Session Open) | ✅ (RAKP v2.0 HMAC-SHA1) |
| `bacnet` | 47808 (UDP) | ✅ (BACnet/IP Who-Is → I-Am) | ✅ (reachability probe) |
| `ntp` | 123 (UDP) | ✅ (NTPv4 client, Mode=4 check) | – (added v0.3.1) |
| `tftp` | 69 (UDP) | ✅ (RRQ → DATA/ERROR) | – (added v0.3.1) |
| `dns` | 53 (UDP) | ✅ (CHAOS version.bind + root A) | – (added v0.3.1) |
| `nfs` | 2049 | ✅ (ONC RPC NULL call) | ✅ (RPC NULL call; no AUTH_GSS) |

44 plugins / authenticators covering enterprise-internal + cloud-native
+ industrial control + building automation + UDP services (NTP / TFTP /
DNS) + cloud-metadata (AWS IMDS / Azure IMDS) + web frameworks
(Jenkins / Kafka / ActiveMQ / RocketMQ / Kibana / WebLogic / RDPv8) in
v0.3.1+. Credential testing covers **26 services** in v0.2
(SSH + FTP + MySQL + Redis + Memcached + MongoDB + MSSQL + SMB +
PostgreSQL + Elasticsearch + VNC + Telnet + Oracle + WinRM + POP3 +
IMAP + SOCKS5 + LDAP + SNMPv2c + Rsync + Docker + RabbitMQ + Modbus +
IPMI v2.0 + BACnet + NFS), with full no-exploit enforcement
(`creds.txt` is the only side-effect). IPv6 targets are first-class
(single IP, CIDR, comma-list). Custom web-fingerprint rulesets
can be loaded via `--web-fingerprint <path-or-url>` — accepts a
local file path or an HTTP(S) URL for live-update from a rules
server. RDP NLA posture (HYBRID / SSL / legacy) is detected by the
`rdp-nla` plugin; full CredSSP authentication remains a v0.4+ task
per the README deferral.

44 个插件/认证器覆盖企业内网 + 云原生 + 工业控制 + 楼宇自控 + UDP 服务
（NTP / TFTP / DNS）+ 云元数据（AWS IMDS / Azure IMDS）+ Web 框架
（Jenkins / Kafka / ActiveMQ / RocketMQ / Kibana / WebLogic / RDPv8）
于 v0.3.1+。IPv6 目标为一等公民。Web 指纹支持
`--web-fingerprint <path-or-url>`（本地文件或 HTTP URL live-update）。
RDP NLA 状态（HYBRID / SSL / 旧版）由 `rdp-nla` 插件探测；完整
CredSSP 认证仍是 v0.4+ 项（README 已 defer）。
v0.2 凭据测试覆盖 **26 个服务**（SSH + FTP + MySQL + Redis + Memcached +
MongoDB + MSSQL + SMB + PostgreSQL + Elasticsearch + VNC + Telnet +
Oracle + WinRM + POP3 + IMAP + SOCKS5 + LDAP + SNMPv2c + Rsync + Docker
+ RabbitMQ + Modbus + IPMI v2.0 + BACnet + NFS），完整"不做漏洞利用"
约束（`creds.txt` 是唯一副作用）。

---

## Quick start / 快速开始

### Build / 构建

Requires Go 1.22+ and [`just`](https://github.com/casey/just).

```bash
# Build for current platform → release/fg-qimen[.exe]
just build

# Cross-compile to all platforms (windows / linux / darwin × amd64 / arm64)
# → release/fg-qimen-{os}-{arch}[.exe]
just all

# List all recipes
just --list
```

### Basic scan / 基本扫描

```bash
# Scan a /24 with default ports
fg-qimen -H 192.168.1.0/24

# Scan specific ports only
fg-qimen -H 192.168.1.0/24 --ports 22,80,443,3389,8080

# Scan a single host
fg-qimen -H 10.0.0.5 --ports 22,80,3306,6379,8080 -t 50

# Save to specific files
fg-qimen -H 10.0.0.5 -o myscan.txt -j myscan.json
```

### Project mode / 项目模式

```bash
# Create a project (one-time per project)
fg-qimen projects create corp-intranet

# Populate targets
echo "10.0.0.0/24" > runs/projects/corp-intranet/targets.txt
echo "10.0.1.0/24" >> runs/projects/corp-intranet/targets.txt

# Linked mode (scan + credential test in one pass)
fg-qimen -p corp-intranet -f runs/projects/corp-intranet/targets.txt -mode linked \
    -u root admin -P 123456 admin P@ssw0rd

# Resume an interrupted scan
fg-qimen resume -p corp-intranet

# Project info / stats
fg-qimen projects info corp-intranet
```

### TUI mode / TUI 模式

The TUI is **on by default** when stdout is a TTY. To force plain text:

TUI 在 stdout 是 TTY 时**默认开启**。强制纯文本：

```bash
fg-qimen -H 127.0.0.1 --no-tui
```

### Dictionary files / 字典文件

```bash
# users.txt (one username per line)
admin
root
test
oracle
postgres

# pass.txt (one password per line; `#` lines are skipped)
# top-10 worst passwords
123456
password
admin
root
qwerty
```

```bash
fg-qimen -H 10.0.0.0/24 -p 22,3306 --user-file users.txt --pass-file pass.txt
fg-qimen scan --mode crack -H targets.txt --user-file users.txt --pass-file pass.txt -p corp
```

---

## CLI reference / CLI 参考

```
fg-qimen [flags]
fg-qimen scan [flags]          # explicit scan
fg-qimen resume -p <name>      # resume project
fg-qimen projects list         # list projects
fg-qimen projects create <n>   # create project
fg-qimen projects delete <n>   # delete project
fg-qimen projects info <n>     # show project details
fg-qimen version               # show version
fg-qimen completion bash       # generate shell completion
```

### Top-10 flags / 常用 10 个 flag

| Short | Long | Default | Meaning |
|---|---|---|---|
| `-H` | `--host` | (empty) | target IP / CIDR / range / comma-list |
| `-f` | `--hosts-file` | (empty) | load targets from file |
| `-p` | `--project` | (empty) | project name (`""` = ephemeral) |
|     | `--mode` | `scan` | `scan` / `crack` / `linked` |
|     | `--ports` | `22,80,3306,3389,6379,8080` | comma-separated ports |
|     | `--exclude-ports` | | ports to exclude |
|     | `--resume` | false | resume from bbolt seen-set |
|     | `--no-state` | false | disable bbolt, in-memory only |
| `-t` | `--threads` | `200` | concurrent workers |
|     | `--timeout` | `3s` | per-op timeout |

For the full 28-flag reference grouped by area (Target / Workspace /
Ports / Network / Concurrency / Credentials / Output / Behavior /
Safety) and CLI usage templates, see
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md) or run `fg-qimen --help`.

完整 28 个 flag 按域分组（Target / Workspace / Ports / Network / Concurrency
/ Credentials / Output / Behavior / Safety）与 CLI 用法模板，见
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md) 或跑 `fg-qimen --help`。

---

## Verifying releases / 验证发布

Each GitHub Release ships five binary artifacts plus one cryptographic
checksum file, one keyless signature, one signing certificate, and one
SBOM per artifact. / 每个 GitHub Release 都会发布 5 个二进制 artifact
外加 1 个加密校验和文件、1 个 keyless 签名、1 个签名证书、每个 artifact
一份 SBOM。

| File | Purpose |
|---|---|
| `fg-qimen-<platform>`     | The compiled binary / 编译后的可执行文件 |
| `SHA256SUMS`              | sha256 checksums for every binary / 所有二进制的 sha256 校验和 |
| `*.sig`                   | cosign keyless signature over the binary (OIDC, Sigstore) / cosign keyless 签名（OIDC、Sigstore） |
| `*.pem`                   | signing certificate embedding the OIDC identity / 含 OIDC 身份的签名证书 |
| `*.sbom.json`             | CycloneDX SBOM for the binary (per-platform) / 平台级 CycloneDX SBOM |

### 1. Checksums / 校验和

```bash
# Download the binary + SHA256SUMS from the release page, then:
# 下载二进制 + SHA256SUMS 到同目录后：
sha256sum -c SHA256SUMS --ignore-missing
```

A clean run prints `<binary>: OK` for each line; a mismatch aborts
with non-zero exit. / 通过打印 `<binary>: OK`；不匹配会以非零退出
码中止。

### 2. Signature (keyless, OIDC) / 签名（keyless、OIDC）

The release pipeline uses [cosign](https://github.com/sigstore/cosign)
in keyless mode against Sigstore's public good instance — no secret
keys are stored in the repository. / 发布流程用 cosign keyless 模式
对接 Sigstore 公共服务实例——仓库不存任何私钥。

```bash
# Install cosign (one-time). / 装 cosign（一次性）。
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

# Verify the signature against the OIDC-signed certificate.
# The certificate pins the GitHub Actions workflow identity
# (https://github.com/LCUstinian/FG-QiMen/.github/workflows/release.yml@refs/tags/<TAG>).
# 用 OIDC 签名证书验签。证书固定了 GitHub Actions workflow 身份。
COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --signature fg-qimen-<platform>.sig \
  --certificate fg-qimen-<platform>.pem \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-<platform>
```

A successful verify prints the verified SHA256 and the OIDC identity
that signed it. / 验证成功会打印已验证的 SHA256 与签名身份的 OIDC
subject。

### 3. SBOM / 软件物料清单

The CycloneDX SBOM lists every direct + transitive dependency that
the binary links against. / CycloneDX SBOM 列出二进制链接的所有
直接或间接依赖。

```bash
# Install cdxgen or cyclonedx-cli (one-time). / 装 cdxgen 或
# cyclonedx-cli（一次性）。
# Inspect with jq: / 用 jq 查看：
jq '.components[] | {name, version, purl}' fg-qimen-<platform>.sbom.json
```

The SBOM is in CycloneDX 1.5 JSON; it can be ingested directly by
[Dependency-Track](https://dependencytrack.org/) or any other
SBOM-aware SCA tool. / SBOM 为 CycloneDX 1.5 JSON；可直接被
Dependency-Track 或任何支持 SBOM 的 SCA 工具摄取。

### 4. Source reproducibility (optional) / 源码可重现（可选）

To rebuild a binary byte-for-byte from the matching tag: / 从对应 tag
逐字节重建二进制：

```bash
git checkout <TAG>
go build -trimpath -ldflags='-s -w -buildid=' -o fg-qimen-local .
sha256sum fg-qimen-local
```

The hash must match the corresponding line in `SHA256SUMS`. / 哈希
必须匹配 `SHA256SUMS` 中对应行。

### 5. Reporting a discrepancy / 反馈不一致

If any of the above fails, **do not run the binary**. Open a GitHub
issue at <https://github.com/LCUstinian/FG-QiMen/issues> with the
output of the failing step and the tag you tried. / 若以上任何步骤
失败，**请勿运行该二进制**。在
<https://github.com/LCUstinian/FG-QiMen/issues> 开 issue 并附失败
步骤的输出与所试 tag。

---

## Localization / 本地化

- **Code comments / 代码注释**: bilingual (Chinese + English) — every public
  function, struct, and key logic block has both.
  **双语**（中英）—— 每个公开函数/结构体/关键逻辑块都有双语注释。
- **Terminal output / 终端输出**: 100% English (banner, help, log, error).
  **纯英文**（banner、help、日志、错误）。
- **README**: bilingual (Chinese sections + English sections).
  **双语并列**。
- **CLI flag names / CLI flag 名**: English.
  **英文**。

## Graceful Ctrl+C / 优雅退出

- First **Ctrl+C**: `cancel()` root context → pipeline drains → output flush
  → bbolt `Sync()` → exit code 130.
  第一次 **Ctrl+C**：`cancel()` 根 context → 管线排空 → 输出刷盘 → bbolt 同步 → 退出码 130。
- Second **Ctrl+C** within `--shutdown-timeout` (default 5s): hard exit (`os.Exit(1)`).
  在 `--shutdown-timeout`（默认 5 秒）内的第二次 **Ctrl+C**：强退 (`os.Exit(1)`)。

---

## Roadmap / 路线图

See [CHANGELOG.md](CHANGELOG.md) for the per-version history. 当前版本 = v0.3.1+
（含第二批发修复）。Next milestones:

- **v0.4**: full crack-mode refactor; proxy unification across plugins;
  per-attempt read-deadline audit (already closed in v0.3.1 second-batch
  for the 7 worst offenders).
- **v0.5+**: full fake-server integration tests (MSSQL / SMB / RDP);
  output rotation; project import/export; richer HTTP fingerprinting.

完整版本历史见 [CHANGELOG.md](CHANGELOG.md)。

---

## Attribution / 致谢

FG-QiMen stands on the shoulders of several open-source projects.
All reused code is MIT-licensed; the per-file modification history
lives in the source headers.

FG-QiMen 站在多个开源项目的肩膀上。所有重用的代码均采用 MIT
许可证；逐文件的修改历史在源码头部注释里。

**Primary inspiration**: [fscan](https://github.com/shadow1ng/fscan)
by [shadow1ng](https://github.com/shadow1ng) (MIT) — the
pipeline-decoupled scanner architecture, the service Identify +
Credential plugin pattern, and the Nmap-style port-fingerprint
framework. FG-QiMen inherits the **no-exploit** policy and drops
every unauthorized-access / write / POC path the original carried.

**主要灵感来源**：fscan（MIT）。FG-QiMen 在此基础上剥离任何接近
"攻击面"的代码路径。

For the full attribution bundle with third-party license texts, see
[`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md).

完整第三方许可证文本见 [`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md)。

The FG-QiMen source is released under the MIT License. See [LICENSE](LICENSE).

---

## Disclaimer / 免责声明

This tool is for **authorized security testing and learning only**. Do not
scan targets without permission. The authors are not responsible for
misuse.

本工具仅供**合法授权的安全测试和学习使用**。请勿对未授权目标进行扫描。
作者不承担任何滥用造成的后果。