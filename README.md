# FG-QiMen

> A pipeline scanner with project workspaces

FG-QiMen is a pure CLI scanner that decouples the **port scanner (producer)** from
the **plugin workers (consumer)** via a Go channel pipeline. It supports three
run modes (`scan` / `crack` / `linked`) and two work modes (ephemeral oneshot
vs persistent project workspace with bbolt state).

[中文文档](README.zh-CN.md) · [Releases](https://github.com/LCUstinian/FG-QiMen/releases) · [Changelog](CHANGELOG.md)

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

## Hard rule: scanner, not attack tool

FG-QiMen **deliberately does not include any vulnerability-exploitation code**.
The scanner tries standard authentication handshakes against known services;
on a hit, the credential is written to `creds.txt` and the run stops — no
`Session.Exec`, no webshell, no persistence. The full no-exploit contract
(the explicit list of forbidden capabilities) lives in
[`docs/SECURITY.md`](docs/SECURITY.md).

---

## Quick start

```bash
# ephemeral scan
fg-qimen -H 192.168.1.0/24

# persistent project with bbolt state
fg-qimen -p corp -H 10.0.0.0/24 -mode linked

# resume a paused project
fg-qimen resume -p corp

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
fg-qimen -p corp-intranet -f runs/projects/corp-intranet/targets.txt -mode linked \
    -u root admin -P 123456 admin P@ssw0rd

# resume / info
fg-qimen resume -p corp-intranet
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
fg-qimen -H 10.0.0.0/24 -p 22,3306 --user-file users.txt --pass-file pass.txt
fg-qimen scan --mode crack -H targets.txt --user-file users.txt --pass-file pass.txt -p corp
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

- `result.txt` — human-readable lines
- `result.json` — NDJSON (one JSON object per line)
- `result.csv` — RFC 4180, one row per result
- `creds.txt` — credential hits (cleartext; operator's working file)
- `rdp.json` / `rdp.txt` — RDP deep fingerprint (hostname, build, NLA flag, OS)

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
| `ldap` | 389, 636 | ✅ (BindRequest + SearchRequest) | – (v0.2+) |
| `memcached` | 11211, 11212 | ✅ (text "version\r\n") | ✅ (ASCII "auth" probe) |
| `elasticsearch` | 9200, 9300 | ✅ (HTTP GET /) | ✅ (HTTP Basic) |
| `rdp` | 3389 | ✅ (TPKT/X.224/MCS 4-step) | – (NLA cred test is v0.4+ deferral) |
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
| `mqtt` | 1883, 8883 | ✅ (MQTT 3.1.1 / 5.0 CONNECT/CONNACK) | – (added v0.4) |
| `modbus` | 502 | ✅ (Read Device Identification) | ✅ (Read Device ID only; no write) |
| `ipmi` | 623 (UDP) | ✅ (RMCP+ Session Open) | ✅ (RAKP v2.0 HMAC-SHA1) |
| `bacnet` | 47808 (UDP) | ✅ (BACnet/IP Who-Is → I-Am) | ✅ (reachability probe) |
| `ntp` | 123 (UDP) | ✅ (NTPv4 client, Mode=4) | – (added v0.3.1) |
| `tftp` | 69 (UDP) | ✅ (RRQ → DATA/ERROR) | – (added v0.3.1) |
| `dns` | 53 (UDP) | ✅ (CHAOS version.bind + root A) | – (added v0.3.1) |
| `nfs` | 2049 | ✅ (ONC RPC NULL call) | ✅ (RPC NULL; no AUTH_GSS) |

Credential testing covers **26 services** (SSH + FTP + MySQL + Redis + Memcached +
MongoDB + MSSQL + SMB + PostgreSQL + Elasticsearch + VNC + Telnet + Oracle +
WinRM + POP3 + IMAP + SOCKS5 + LDAP + SNMPv2c + Rsync + Docker + RabbitMQ +
Modbus + IPMI v2.0 + BACnet + NFS), all with the no-exploit enforcement
(`creds.txt` is the only side-effect).

IPv6 is first-class (single IP / CIDR / comma-list). Custom web-fingerprint
rulesets load via `--web-fingerprint <path-or-url>` (local file or HTTP URL
for live-update from a rules server). RDP NLA posture (HYBRID / SSL / legacy)
is detected by the `rdp-nla` plugin; full CredSSP authentication is deferred
to v0.4+.

---

## CLI reference

```
fg-qimen [flags]
fg-qimen scan [flags]            # explicit scan
fg-qimen resume -p <name>        # resume project
fg-qimen projects list           # list projects
fg-qimen projects create <n>     # create project
fg-qimen projects delete <n>     # delete project
fg-qimen projects info <n>       # show project details
fg-qimen version                 # show version
fg-qimen completion bash         # generate shell completion
```

### Top-10 flags

| Short | Long | Default | Meaning |
|---|---|---|---|
| `-H` | `--host` | (empty) | target IP / CIDR / range / comma-list |
| `-f` | `--hosts-file` | (empty) | load targets from file |
| `-p` | `--project` | (empty) | project name (`""` = ephemeral) |
|     | `--mode` | `scan` | `scan` / `crack` / `linked` |
|     | `--ports` | `22,80,3306,3389,6379,8080` | comma-separated ports |
|     | `--exclude-ports` | (empty) | ports to exclude |
|     | `--resume` | `false` | resume from bbolt seen-set |
|     | `--no-state` | `false` | disable bbolt, in-memory only |
| `-t` | `--threads` | `200` | concurrent workers |
|     | `--timeout` | `3s` | per-op timeout |

Full 28-flag reference grouped by area (Target / Workspace / Ports / Network /
Concurrency / Credentials / Output / Behavior / Safety) and CLI usage templates:
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md) or `fg-qimen --help`.

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
