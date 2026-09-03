# FG-QiMen

> A pipeline scanner with project workspaces

FG-QiMen is a pure CLI scanner that decouples the **port scanner (producer)** from
the **plugin workers (consumer)** via a Go channel pipeline. It supports three
run modes (`scan` / `crack` / `linked`) and two work modes (ephemeral oneshot
vs persistent project workspace with bbolt state).

[中文文档](README.zh-CN.md) · [Releases](https://github.com/LCUstinian/FG-QiMen/releases) · [Changelog](CHANGELOG.md)

```
┌─ FG-QIMEN v0.5 ──── project: corp-intranet ──── mode: linked ─┐
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

## Hard rule: scanner, not attack tool

FG-QiMen **deliberately does not include any vulnerability-exploitation code**.
The scanner tries standard authentication handshakes against known services;
on a hit, the credential is written to `fgqm_creds.txt` and the run stops — no
`Session.Exec`, no webshell, no persistence. The full no-exploit contract
(the explicit list of forbidden capabilities) lives in
[`docs/SECURITY.md`](docs/SECURITY.md).

---

## Quick start

```bash
# ephemeral scan
fg-qimen -H 192.168.1.0/24

# persistent project with bbolt state
fg-qimen --project corp -H 10.0.0.0/24 --mode linked

# resume a paused project
fg-qimen resume --project corp

# list projects
fg-qimen projects list
```

### Build

Requires Go 1.22+ and [`just`](https://github.com/casey/just).

```bash
just build         # → release/fg-qimen[.exe]
just all           # → release/fg-qimen-{os}-{arch}[.exe]
just --list
```

### Basic scan

```bash
# scan a /24 with default ports
fg-qimen -H 192.168.1.0/24

# specific ports
fg-qimen -H 192.168.1.0/24 --ports 22,80,443,3389,8080

# single host
fg-qimen -H 10.0.0.5 --ports 22,80,3306,6379,8080 -t 50

# custom output paths
fg-qimen -H 10.0.0.5 -o myscan.txt -j myscan.json
```

### Project mode

```bash
# one-time project creation
fg-qimen projects create corp-intranet

# populate targets
echo "10.0.0.0/24"   >  runs/projects/corp-intranet/targets.txt
echo "10.0.1.0/24"   >> runs/projects/corp-intranet/targets.txt

# linked mode (scan + credential test in one pass)
fg-qimen --project corp-intranet -f runs/projects/corp-intranet/targets.txt --mode linked \
    -u root admin -p 123456 admin P@ssw0rd

# resume / info
fg-qimen resume --project corp-intranet
fg-qimen projects info corp-intranet
```

### TUI

The TUI is **on by default** when stdout is a TTY. Force plain text with
`--no-tui`.

### Dictionary files

```text
# users.txt (one username per line)
admin
root
test
oracle
postgres
```

```text
# pass.txt (one password per line; `#` lines are skipped)
# top-10 worst passwords
123456
password
admin
root
qwerty
```

```bash
fg-qimen -H 10.0.0.0/24 --ports 22,3306 -uf users.txt -pf pass.txt
fg-qimen scan --mode crack -H targets.txt -uf users.txt -pf pass.txt --project corp
```

---

## Features

### Architecture

- **Pipeline decoupling**: port scan (producer) → `chan ScanItem` → plugin
  workers (consumer). All stages honor `context.Context` for cancellation.
- **Three run modes**: `scan` / `crack` / `linked` (see [CLI reference](#cli-reference)).
- **Project workspace**: each project gets its own directory + bbolt DB.
- **Incremental tracking**: SHA-1-based dedup with optional bbolt persistence;
  `--resume` reloads the seen-set.
- **TUI**: Bubbletea + Lipgloss cyberpunk theme (green / amber / red on
  black); auto-fallback to plain text on non-TTY.

Full architecture write-up: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

### Output formats

Result files carry a `HH-MM-SS` local-time start stamp in the
filename (added in v0.5.1) so two same-day runs don't clobber
each other. The directory is bucketed by `YYYY-MM-DD`; the time
suffix goes on the file. Examples: `fgqm_result_14-30-22.txt`,
`fgqm_creds.txt` (no stamp — the file is opened `O_APPEND` and
the dedup is on the in-memory `State`).

- `fgqm_result_HH-MM-SS.txt` — human-readable lines
- `fgqm_result_HH-MM-SS.json` — NDJSON (one JSON object per line)
- `fgqm_result_HH-MM-SS.csv` — RFC 4180, one row per result
- `fgqm_creds.txt` — credential hits (cleartext; operator's working file)
- `fgqm_rdp_HH-MM-SS.json` / `fgqm_rdp_HH-MM-SS.txt` — RDP deep fingerprint (hostname, build, NLA flag, OS)
- `fgqm_alive.txt` — one IP per line (dedup'd host list for `nmap -iL` / `masscan --targets` / `curl` loops)

Explicit paths via `-ot` / `-oj` / `-oc` bypass both the bucketing
and the stamp.

### Plugins (44 plugins / authenticators)

| Plugin | Default ports | Identify | Credential |
|---|---|---|---|
| `ssh` | 22, 2222, 2200, 22222 | ✅ | ✅ (password only; no Session/Exec) |
| `http` | 80, 443, 8080, 8443, 8000, 8888 | ✅ | – (v0.2+) |
| `webtitle` | 80, 443, 8080, 8443 | ✅ (FingerprintHub 3139 rules + favicon) | – |
| `redis` | 6379, 6380 | ✅ (PING / PONG) | ✅ (RESP AUTH) |
| `mongodb` | 27017, 27018 | ✅ (OP_MSG hello) | ✅ (SCRAM-SHA-256 via OP_MSG) |
| `postgresql` | 5432, 5433, 5434 | ✅ (StartupMessage) | ✅ (lib/pq via `db.PingContext`) |
| `mssql` | 1433, 1434, 2433 | ✅ (TDS via go-mssqldb) | ✅ (TDS Login7 via go-mssqldb) |
| `smb` | 445, 139 | ✅ (SMB magic) | ✅ (SMB2 Session Setup NTLMv2) |
| `smtp` | 25, 465, 587, 2525 | ✅ (EHLO) | – (v0.2+) |
| `snmp` | 161, 162 | ✅ (sysDescr.0 raw) | – (v0.2+) |
| `snmpv3` | 161, 162 | ✅ (GetRequest v3) | |
| `ldap` | 389, 636 | ✅ (BindRequest + SearchRequest) | – (v0.2+) |
| `memcached` | 11211, 11212 | ✅ (text "version\r\n") | ✅ (ASCII "auth" probe) |
| `elasticsearch` | 9200, 9300 | ✅ (HTTP GET /) | ✅ (HTTP Basic) |
| `rdp` | 3389 | ✅ (TPKT/X.224/MCS 4-step) | – (NLA cred test deferred) |
| `rdpnla` | 3389 | ✅ (RDP NLA posture HYBRID/SSL/legacy) | – (NLA cred deferred) |
| `vnc` | 5900–5905 | ✅ (RFB 003.x banner) | ✅ (RFB handshake + DES challenge) |
| `telnet` | 23, 2323 | ✅ (IAC-stripped banner) | ✅ (IAC + prompt + user/pass flow) |
| `oracle` | 1521, 1526, 2483 | ✅ (TNS Connect/Accept) | ✅ (TNS handshake via go-ora) |
| `winrm` | 5985, 5986 | ✅ (GET /wsman) | ✅ (HTTP Basic + WSMan SOAP) |
| `pop3` | 110, 995 | ✅ (+OK greeting) | ✅ (RFC 1939 USER/PASS) |
| `imap` | 143, 993 | ✅ (`* OK` greeting) | ✅ (RFC 3501 LOGIN) |
| `socks5` | 1080 | ✅ (SOCKS5 VER 5) | ✅ (RFC 1928/1929 user/pass) |
| `rsync` | 873, 8873 | ✅ (`@RSYNCD:` greeting) | ✅ (USERNAME + MD5 challenge) |
| `docker` | 2375, 2376 | ✅ (GET /_ping + /info) | ✅ (HTTP Basic to /images/json) |
| `rabbitmq` | 5672 | ✅ (AMQP 0-9-1 header + Start) | ✅ (AMQP PLAIN) |
| `mqtt` | 1883, 8883 | ✅ (MQTT 3.1.1 / 5.0 CONNECT/CONNACK) | |
| `activemq` | 61616 | ✅ (OpenWire stub) | |
| `kafka` | 9092 | ✅ (ApiVersions v0+) | |
| `rocketmq` | 9876 | ✅ (RemotingCommand stub) | |
| `modbus` | 502 | ✅ (Read Device Identification) | ✅ (Read Device ID only; no write) |
| `ipmi` | 623 (UDP) | ✅ (RMCP+ Session Open) | ✅ (RAKP v2.0 HMAC-SHA1) |
| `bacnet` | 47808 (UDP) | ✅ (BACnet/IP Who-Is → I-Am) | ✅ (reachability probe) |
| `ntp` | 123 (UDP) | ✅ (NTPv4 client, Mode=4) | |
| `tftp` | 69 (UDP) | ✅ (RRQ → DATA/ERROR) | |
| `dns` | 53 (UDP) | ✅ (CHAOS version.bind + root A) | |
| `nfs` | 2049 | ✅ (ONC RPC NULL call) | ✅ (RPC NULL; no AUTH_GSS) |
| `jenkins` | 8080, 8443, 50000 | ✅ (Jenkins crumb + version) | |
| `kibana` | 5601 | ✅ (Kibana status API) | |
| `weblogic` | 7001, 7002, 8443 | ✅ (WebLogic console login page) | |
| `aws` | 80 (cloud-metadata) | ✅ (IMDSv1 + IMDSv2 fingerprint) | |
| `azure` | 80 (cloud-metadata) | ✅ (Azure IMDS fingerprint) | |

Credential testing covers **21 services** (SSH + Redis + MongoDB +
PostgreSQL + MSSQL + SMB + Memcached + Elasticsearch + VNC + Telnet + Oracle +
WinRM + POP3 + IMAP + SOCKS5 + Rsync + Docker + RabbitMQ + Modbus + IPMI v2.0 +
BACnet + NFS), all with the no-exploit enforcement
(`fgqm_creds.txt` is the only side-effect).

IPv6 is first-class (single IP / CIDR / comma-list). Custom web-fingerprint
rulesets load via `--web-fingerprint <path-or-url>` (local file or HTTP URL
for live-update from a rules server). RDP NLA posture (HYBRID / SSL / legacy)
is detected by the `rdp-nla` plugin; full CredSSP authentication is
deferred.

---

## CLI reference

```
fg-qimen [flags]
fg-qimen scan [flags]                       # explicit scan
fg-qimen resume --project <name>            # resume project
fg-qimen projects list                      # list projects
fg-qimen projects create <n>                # create project
fg-qimen projects delete <n>                # delete project
fg-qimen projects info <n>                  # show project details
fg-qimen projects export <n> <out.fgq>      # export project to single .fgq file
fg-qimen projects import <in.fgq> <n>      # import from .fgq file
fg-qimen version                 # show version
fg-qimen completion bash         # generate shell completion
```

### Quick start (5 essential flags)

For ~90% of scans you only need these five flags:

| Short | Long | Example | Purpose |
|---|---|---|---|
| `-H` | `--host` | `-H 10.0.0.0/24` | target IP / CIDR / range / comma-list |
| (无) | `--project` | `--project corp` | named project (persists to bbolt; omit for ephemeral) |
| `-u` | `--user` | `-u root admin` | inline usernames |
| `-uf` | `--user-file` | `-uf users.txt` | usernames dictionary file (one per line) |
| `-pf` | `--pass-file` | `-pf pass.txt` | passwords dictionary file (one per line) |

The four most common pairings:

```bash
-H 1.0.0.0/8 -u admin -p root,toor              # host + inline creds
-H 1.0.0.0/8 -uf users.txt -pf passes.txt       # host + wordlists
-H 1.0.0.0/8 -f targets.txt -a                  # hosts file + alive-only
-H 1.0.0.0/8 -ot r.txt -oj r.json -oc r.csv      # all three output sinks
```

Concrete recipes:

```bash
# Minimal scan: 256-host /24 against default ports
fg-qimen -H 10.0.0.0/24

# Named project with dictionaries + small thread count
fg-qimen --project corp -H 10.0.0.0/24 -uf users.txt -pf pass.txt -t 50

# Re-attack a saved project against its previously-seen hosts
fg-qimen resume --project corp

# Crack-only: skip alive + port scan, just try creds
fg-qimen scan --project corp --mode crack -uf users.txt -pf pass.txt

# Behind an HTTP proxy (chains through any plugin's dialer)
fg-qimen -H 10.0.0.0/24 --proxy http://127.0.0.1:8080
```

> **Short-flag convention** (v0.5.1+): all lowercase, mnemonic only,
> 2-letter for namespaces (output-* / user-pass-file). `-H` is the
> sole uppercase (it avoids the `-h`/`--help` collision that cobra
> reserves). See [CHANGELOG](CHANGELOG.md) for the migration
> table from v0.5.0.

### Full flag reference (v0.5.1 — 45 flags, 14 with short aliases)

| Short | Long | Default | Group | Meaning |
|---|---|---|---|---|
| `-H` | `--host` | (empty) | Target | target IP / CIDR / range / comma-list (e.g. `10.0.0.0/24,192.168.1.0/24`) |
| `-f` | `--hosts-file` | (empty) | Target | load targets from file (one host per line; `#` comments skipped) |
|     | `--project` | (empty) | Workspace | project name; empty = ephemeral (no bbolt). No short flag — use long form (e.g. `--project corp`). |
|     | `--project-key` | (empty) | Workspace | passphrase to encrypt the project DB at rest (AES-256-GCM, Argon2id-derived v0.4+). Empty = plaintext. Env: `FG_QIMEN_PROJECT_KEY` |
|     | `--mode` | `scan` | Workspace | `scan` (alive→scan→identify) / `crack` (creds only) / `linked` (scan + creds) |
| `-r` | `--resume` | `false` | Workspace | resume from bbolt seen-set (skip already-seen host:port pairs). New in v0.5.1. |
|     | `--no-state` | `false` | Workspace | disable bbolt, in-memory only; the project is wiped on exit |
|     | `--ports` | `22,80,3306,3389,6379,8080` | Ports | comma-separated port list |
|     | `--exclude-ports` | (empty) | Ports | ports to remove from the resolved list |
|     | `--no-icmp` | `false` | Ports | skip ICMP alive probe (TCP-only mode for hostile networks) |
|     | `--proxy` | (empty) | Network | HTTP/HTTPS proxy URL (e.g. `http://127.0.0.1:8080`). Honored by every TCP dial site via `credential.DialTCP` / `DialTCPAddr` (Phase 2.2). No short flag (use long form). |
|     | `--socks5` | (empty) | Network | SOCKS5 proxy URL (e.g. `socks5://user:pass@127.0.0.1:1080`) |
|     | `--iface` | (empty) | Network | bind outgoing connections to this local IP |
| `-t` | `--threads` | `200` | Concurrency | concurrent workers in the plugin pool |
|     | `--max-workers` | `16` | Concurrency | hard upper bound for `--threads` (caps the auto-scaler) |
|     | `--timeout` | `3s` | Concurrency | per-op timeout (also covers the alive probe, port scan connect, plugin handshake) |
| `-a` | `--alive-only` | `false` | Concurrency | stop after the alive probe; no scan / identify / credential |
| `-u` | `--user` | (empty) | Credentials | inline usernames (comma-separated) |
| `-p` | `--pass` | (empty) | Credentials | inline passwords (comma-separated). v0.5.1: short changed from `-P` to `-p` (Unix-standard mnemonic for password; matches sshpass / passwd / openssl). |
| `-uf` | `--user-file` | (empty) | Credentials | usernames dictionary file (one per line). v0.5.1: short changed from `-U` to `-uf` (nmap-style 2-letter for namespaced flags). |
| `-pf` | `--pass-file` | (empty) | Credentials | passwords dictionary file (one per line). v0.5.1: short changed from `-W` to `-pf`. |
| `-ot` | `--output-txt` | (empty) | Output | path to TXT result file. v0.5.1: short changed from `-o` to `-ot` (2-letter for the output namespace). |
| `-oj` | `--output-json` | (empty) | Output | path to NDJSON result file. v0.5.1: short changed from `-j` to `-oj`. |
| `-oc` | `--output-csv` | (empty) | Output | path to CSV result file (one row per result; stable column order for awk / pandas). New in v0.5.1. |
|     | `--output-sarif` | (empty) | Output | path to SARIF 2.1.0 JSON (one document, for GitHub Code Scanning). No short flag (niche). |
|     | `--rotate-bytes` | `0` | Output | per-file size cap for output rotation (0 = no rotation). Renamed from `--output-rotate-bytes` in v0.4.1; the `output-` prefix was redundant since `rotate` is unique to the output subsystem. |
|     | `--rotate-files` | `0` | Output | total files to keep including active (0 = no rotation). Renamed from `--output-rotate-files` in v0.4.1. |
|     | `--show-creds` | `false` | Output | force cleartext in `fgqm_result.txt` (`fgqm_creds.txt` is always cleartext) |
|     | `--plugins` | (empty) | Output | comma-separated plugin allowlist (e.g. `--plugins ssh,redis,vnc`); empty = all |
|     | `--web-fingerprint` | (empty) | Output | path or URL to extra FingerprintHub-style web rules |
|     | `--http-form-url` | (empty) | Output | HTTP basic-auth URL for the HTTP form-brute plugin (opt-in) |
|     | `--http-form-fields` | `user=$user$,pass=$pass$` | Output | field template for the form-brute plugin |
|     | `--http-form-success` | (empty) | Output | substring that indicates a successful login response |
|     | `--http-form-failure` | `invalid` | Output | substring that indicates a failed login response |
|     | `--http-form-redirect` | (empty) | Output | if set, follow redirects and use this substring to detect success in the final response |
|     | `--silent` | `false` | Behavior | suppress banner / live event log lines |
|     | `--no-tui` | `false` | Behavior | force plain-text output even when stdout is a TTY |
|     | `--no-batch` | `false` | Behavior | disable bbolt batched writes (one fsync per Put instead of per batch) |
| `-v` | `--verbose` | `false` | Behavior | verbose logging (debug-level from plugins) |
|     | `--insecure-tls` | `false` | Safety | skip TLS certificate verification (probe builds; INSECURE — see HARD rule) |
|     | `--insecure-ssh` | `false` | Safety | skip SSH host-key verification (INSECURE — see HARD rule) |
|     | `--known-hosts` | (empty) | Safety | path to `known_hosts` file (sets `InsecureIgnoreHostKey` to false) |

Complete CLI usage templates (one per common workflow) live in
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md). The current
`fg-qimen --help` output is the authoritative reference.

---

## Verifying releases

Each GitHub Release ships **11 platform binaries** plus per-binary cosign
signatures, signing certificates, and CycloneDX SBOMs:

| File | Purpose |
|---|---|
| `fg-qimen-<platform>` | Compiled binary (no `.exe` on Linux/macOS/BSD) |
| `SHA256SUMS` | sha256 checksums for every binary |
| `*.sig` | cosign keyless signature (OIDC, Sigstore) |
| `*.pem` | signing certificate embedding the OIDC identity |
| `*.sbom.json` | CycloneDX SBOM for the binary (per-platform) |
| `FG-QiMen-release.spdx.json` | Full SPDX SBOM across all 11 platforms |

### 1. Checksums

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

A clean run prints `<binary>: OK` for each line.

### 2. Signature (keyless, OIDC)

The release pipeline uses
[cosign](https://github.com/sigstore/cosign) in keyless mode against
Sigstore's public good instance — no secret keys in the repository.

```bash
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --signature fg-qimen-<platform>.sig \
  --certificate fg-qimen-<platform>.pem \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-<platform>
```

A successful verify prints the SHA256 of the verified binary and the OIDC
identity that signed it. The certificate pins the GitHub Actions workflow
identity (`https://github.com/LCUstinian/FG-QiMen/.github/workflows/release.yml@refs/tags/<TAG>`).

### 3. SBOM

The CycloneDX SBOM lists every direct + transitive dependency the binary
links against. It can be ingested directly by
[Dependency-Track](https://dependencytrack.org/) or any SBOM-aware SCA tool.

```bash
# inspect components
jq '.components[] | {name, version, purl}' fg-qimen-<platform>.sbom.json
```

### 4. Source reproducibility (optional)

To rebuild a binary byte-for-byte from the matching tag:

```bash
git checkout <TAG>
go build -trimpath -ldflags='-s -w -buildid=' -o fg-qimen-local .
sha256sum fg-qimen-local
```

The hash must match the corresponding line in `SHA256SUMS`.

### 5. Reporting a discrepancy

If any of the above fails, **do not run the binary**. Open a GitHub issue at
<https://github.com/LCUstinian/FG-QiMen/issues> with the failing step's
output and the tag you tried.

---

## Localization

- **Code comments**: bilingual (Chinese + English) on every public function,
  struct, and key logic block.
- **Terminal output**: 100% English (banner, help, log, error).
- **README**: split — English ([README.md](README.md)) + Simplified Chinese
  ([README.zh-CN.md](README.zh-CN.md)).
- **CLI flag names**: English.

## Graceful Ctrl+C

- First **Ctrl+C**: `cancel()` root context → pipeline drains → output
  flush → bbolt `Sync()` → exit code 130.
- Second **Ctrl+C** within `--shutdown-timeout` (default 5s): hard exit
  (`os.Exit(1)`).

---

## Roadmap

Next milestones (see [CHANGELOG.md](CHANGELOG.md) for per-version history):

- **v0.4**: full crack-mode refactor; proxy unification across plugins;
  per-attempt read-deadline audit (closed for 7 worst offenders in v0.3.1).
- **v0.5+**: full fake-server integration tests (MSSQL / SMB / RDP);
  output rotation; project import/export; richer HTTP fingerprinting.

---

## Attribution

FG-QiMen stands on the shoulders of several open-source projects. All reused
code is MIT-licensed; the per-file modification history lives in the source
headers.

**Primary inspiration**: [fscan](https://github.com/shadow1ng/fscan) by
[shadow1ng](https://github.com/shadow1ng) (MIT) — the pipeline-decoupled
scanner architecture, the service Identify + Credential plugin pattern,
and the Nmap-style port-fingerprint framework. FG-QiMen inherits the
**no-exploit** policy and drops every unauthorized-access / write / POC path
the original carried.

Full attribution bundle with third-party license texts:
[`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md).

The FG-QiMen source is released under the MIT License. See
[LICENSE](LICENSE).

---

## Disclaimer

This tool is for **authorized security testing and learning only**. Do not
scan targets without permission. The authors are not responsible for misuse.
