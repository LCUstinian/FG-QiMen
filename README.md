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
┌─ FG-QIMEN v0.2 ──── project: corp-intranet ──── mode: linked ─┐
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
└─────────────────────────────────────────────────────────────────┘
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

## Features / 功能特性

### Architecture / 架构

- **Pipeline decoupling / 管道解耦**: port scan (producer) → `chan ScanItem` → plugin workers (consumer). All stages honor `context.Context` for cancellation.
  **管道解耦**：端口扫描 (producer) → `chan ScanItem` → 插件 worker (consumer)。所有阶段遵循 `context.Context` 实现取消。
- **Three run modes / 三种运行模式**:
  - `scan` — port scan + Identify only
  - `crack` — skip port scan, run Credential against known ports
  - `linked` — run scan first, then trigger Credential on services that declared `ModeCredential`
- **Project workspace / 项目工作区**: each project gets its own directory + bbolt DB.
  **项目工作区**：每个项目独立目录 + bbolt DB。
- **Incremental tracking / 增量追踪**: SHA-1-based dedup with optional bbolt persistence; `--resume` reloads the seen-set.
  **增量追踪**：基于 SHA-1 的去重，可选 bbolt 持久化；`--resume` 重载已见集合。
- **Two work modes / 两种工作模式**:
  - Ephemeral / 即扫即走 (default): no DB, no resume, write to CWD
  - Project / 增量扫描 (`-p <name>`): `./runs/projects/<name>/` with bbolt + resume
- **TUI / 终端 UI**: Bubbletea + Lipgloss cyberpunk theme (green / amber / red on black); auto-fallback to plain text on non-TTY.
  **TUI**：Bubbletea + Lipgloss 赛博朋克配色（黑底绿/琥珀/红）；非 TTY 自动降级纯文本。

### Output / 输出

- `result.txt` — human-readable lines
- `result.json` — NDJSON (one JSON object per line)
- `creds.txt` — credential hits (only; never any post-auth action)
- `rdp.json` / `rdp.txt` — RDP deep fingerprint (hostname, build, NLA flag, OS)

### Plugins (v0.2) / 插件 (v0.2)

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
| `nfs` | 2049 | ✅ (ONC RPC NULL call) | ✅ (RPC NULL call; no AUTH_GSS) |

30 plugins / authenticators covering enterprise-internal + cloud-native
+ industrial control + building automation services. Credential testing
covers **26 services** in v0.2 (SSH + FTP + MySQL + Redis + Memcached
+ MongoDB + MSSQL + SMB + PostgreSQL + Elasticsearch + VNC + Telnet +
Oracle + WinRM + POP3 + IMAP + SOCKS5 + LDAP + SNMPv2c + Rsync + Docker
+ RabbitMQ + Modbus + IPMI v2.0 + BACnet + NFS), with full no-exploit
enforcement (`creds.txt` is the only side-effect).

30 个插件/认证器覆盖企业内网 + 云原生 + 工业控制 + 楼宇自控服务。
v0.2 凭据测试覆盖 **26 个服务**（SSH + FTP + MySQL + Redis + Memcached +
MongoDB + MSSQL + SMB + PostgreSQL + Elasticsearch + VNC + Telnet +
Oracle + WinRM + POP3 + IMAP + SOCKS5 + LDAP + SNMPv2c + Rsync + Docker
+ RabbitMQ + Modbus + IPMI v2.0 + BACnet + NFS），完整"不做漏洞利用"
约束（`creds.txt` 是唯一副作用）。

### Credential testing (v0.2) / 凭据测试 (v0.2)

| Service | Mechanism | Driver / Library | Tests |
|---|---|---|---|
| SSH | `x/crypto/ssh.NewClientConn` (auth only) | `golang.org/x/crypto/ssh` | reuse upstream |
| FTP | `ftplib.Login` then `Quit` | `github.com/jlaffaye/ftp` | reuse upstream |
| MySQL | `database/sql.Open + PingContext` | `github.com/go-sql-driver/mysql` | reuse upstream |
| Redis | RESP `PING` → `AUTH <pass>` | handcrafted (no third-party) | 4/4 (NoPass / Hit / MissAll / NotRedis) |
| Memcached | ASCII `version` → `auth` | handcrafted | 4/4 (NoAuth / Hit / MissAll / NotMemcached) |
| MongoDB | OP_MSG `saslStart SCRAM-SHA-256` → `saslContinue` → `v=` | handcrafted + `x/crypto/pbkdf2` | 1/1 (Hit) |
| MSSQL | TDS Login7 via `db.PingContext` | `github.com/microsoft/go-mssqldb` | 1/1 (smoke) |
| SMB | SMB2 Session Setup NTLMv2 via `Dialer.DialContext` | `github.com/hirochachacha/go-smb2` | 1/1 (smoke) |

> Smoke tests for MSSQL / SMB stand in for full fake servers (TDS
> PRELOGIN+Login7 / SMB2 Negotiate+Session Setup). A real server
> integration test is a v0.2+ task.
> MSSQL / SMB 的冒烟测试替代了完整的假服务器（TDS PRELOGIN+Login7 /
> SMB2 Negotiate+Session Setup）。真正的服务端集成测试是 v0.2+ 任务。

---

## Hard rule: NO exploit code / 硬性原则：不包含漏洞利用

**FG-QiMen deliberately does NOT include any vulnerability exploitation
capability.** This is non-negotiable. The following are explicitly excluded
from v0.1 and all future versions:

**FG-QiMen 故意不包含任何漏洞利用能力。此为硬性原则。v0.1 及所有未来版本
明确排除以下内容：**

- ❌ MS17-010 (EternalBlue) detection or exploitation
  ❌ MS17-010（永恒之蓝）探测与利用
- ❌ SMBGhost (CVE-2020-0796)
- ❌ Redis write SSH key / cron / webshell / master-slave RCE
  ❌ Redis 写公钥 / 写计划任务 / 写 WebShell / 主从复制 RCE
- ❌ SSH post-auth command execution (no `ssh.NewSession` / `Exec` / `Shell` in code)
  ❌ SSH 认证后自动执行命令（代码中**不存在** `ssh.NewSession` / `Exec` / `Shell`）
- ❌ MS17-010 shellcode injection / SMB shellcode
  ❌ MS17010 ShellCode 注入
- ❌ JDWP exploitation
- ❌ RMI / JBoss / WebLogic deserialization RCE
  ❌ RMI / JBoss / WebLogic 反序列化 RCE
- ❌ Any CVE-based remote code execution
  ❌ 任何 CVE-based 的远程代码执行
- ❌ Reverse / bind shell / SOCKS5 server (post-exploitation)
  ❌ 反弹 Shell / 正向 Shell / SOCKS5 代理服务端（后渗透）
- ❌ Any post-credential automation (write files, run commands, plant backdoors)
  ❌ 凭据成功后的任何自动化操作

### What credential testing means here / 爆破的严格定义

✅ **Allowed / 允许**: try a list of user:pass combinations against SSH / RDP / FTP /
MySQL / Redis / SMB / etc. via the standard authentication handshake.

✅ **允许**：用 user:pass 字典对 SSH / RDP / FTP / MySQL / Redis / SMB 等服务做
标准认证握手尝试。

✅ **On hit / 命中时**: write `user / pass` to `creds.txt` and stop. Nothing else.
The plugin function returns a `*Result` with `Cred` set; the pipeline writes
it to disk; no `Session.Exec` / no webshell / no command runs.

✅ **命中时**：把 `user / pass` 写入 `creds.txt` 然后停止。插件函数返回带
`Cred` 字段的 `*Result`；管线写盘后即终止；不调用 `Session.Exec`、不上
WebShell、不执行任何命令。

❌ **Never / 严禁**: any post-auth action — running remote commands, writing
remote files, planting persistence, etc.

❌ **严禁**：任何认证后动作——执行远程命令、写远程文件、植入持久化等。

**Scanner + credential tester = discovery tool. Exploitation = attack tool.
FG-QiMen is only the former.**

**扫描器 + 凭据测试器 = 探测面工具。漏洞利用 = 攻击面工具。
FG-QiMen 只做前者。**

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

### Directory layout / 目录结构

```
FG-QiMen/
├── cmd/                                # Cobra commands (root / scan / resume / projects / version)
├── internal/                           # everything below is module-private (cmd/ + tests only)
│   ├── types/                          # leaf: Config, State, Result, Cred, Target, Logger, TTY, ExpandTargets
│   ├── output/                         # multi-format result sink (txt/ndjson/creds/rdp) + RDPFingerprint
│   ├── store/                          # bbolt persistence wrapper
│   ├── ui/                             # UI interface + TextUI + NopUI (TUI lives in tui/)
│   ├── session/                        # Session bag wiring types/output/store/ui — top of the leaf DAG
│   ├── core/                           # pipeline orchestrator (pipeline.go, scanner.go)
│   │   ├── alive/                      # always-on host discovery: Probe iface + Discovery + ICMP/TCP/system-ping
│   │   ├── credential/                 # pool / protocol / scheduler
│   │   │   └── auth/
│   │   │       ├── database/           # PostgreSQL / MySQL / MSSQL / Oracle / MongoDB / ES / Redis / Memcached
│   │   │       ├── email/              # POP3 / IMAP
│   │   │       ├── filestorage/        # NFS / SMB / Rsync
│   │   │       ├── messaging/          # RabbitMQ
│   │   │       ├── network/            # SNMP / LDAP / Modbus / BACnet / Docker / SOCKS5
│   │   │       └── remote/             # SSH / FTP / Telnet / VNC / WinRM / IPMI
│   │   └── scan/                       # TCP connect + UDP probe + adaptive pool + iterator
│   ├── discovery/                      # LAN-only host discovery: ARP + NetBIOS (opt-in via blank import)
│   ├── portscan/
│   │   └── fingerprint/                # Nmap-style service fingerprint (//go:embed 2.5MB nmap-service-probes.txt)
│   ├── plugins/                        # Plugin interface + registry
│   │   └── adapted/                    # built-in plugins, 7 category subpackages
│   │       ├── database/{elasticsearch,memcached,mongodb,mssql,oracle,postgresql,redis}
│   │       ├── email/{imap,pop3,smtp}
│   │       ├── filestorage/{nfs,rsync,smb}
│   │       ├── messaging/rabbitmq
│   │       ├── network/{bacnet,docker,ldap,modbus,snmp,socks5}
│   │       ├── remote/{ipmi,rdp,telnet,vnc,winrm}
│   │       └── web/
│   │           └── webtitle/           # HTTP probe + redirect follow + favicon mmh3
│   │               └── fingerprint/    # FingerprintHub matcher + //go:embed 1.3MB web_fingerprint_v4.json
│   ├── tui/                            # Bubbletea + Lipgloss dashboard
│   └── workspace/                      # Ephemeral / project workspace (Open, Stats, ProjectsRoot, List, Delete)
├── docs/                               # Project documentation (release notes, design specs)
│   ├── RELEASE_NOTES_v0.2.md           # v0.2 release notes (post-refactor sweep)
│   └── superpowers/specs/              # Design specs (e.g. db-cred-rdp-fingerprint-design.md)
├── test/                               # Test data (committed: targets, users, passes)
├── release/                            # Build outputs (gitignored)
│   ├── fg-qimen[.exe]                  # current platform
│   └── fg-qimen-{os}-{arch}[.exe]      # cross-compiled
├── runs/                               # Scan-run outputs (gitignored)
│   ├── default/                        # ephemeral mode default
│   │   ├── result.txt
│   │   ├── result.json
│   │   ├── creds.txt
│   │   └── rdp.{json,txt}
│   └── projects/<name>/                # project mode default
│       ├── fg.db                       # bbolt state
│       └── (same output files as above)
├── justfile                            # Build / lint / test recipes
├── README.md                           # ← you are here
├── THIRD_PARTY_LICENSES.md
├── LICENSE
├── go.mod / go.sum
└── main.go
```

### Three core stages / 三大核心阶段

| Stage | Package | Probe / Authenticator | Plugin / Driver |
|---|---|---|---|
| Host discovery (always on) / 主机存活（始终启用） | `internal/core/alive` | `alive.TCPProbe` (TCP-ping) + `alive.ICMPProbe` (raw socket) + `alive.SystemPingProbe` (cmd) | orchestrated by `alive.New(opts).Run()` |
| Host discovery (LAN, opt-in) / 主机存活（LAN，opt-in） | `internal/discovery` | `discovery.ARPProbe` (OS ARP table) + `discovery.NBNSProbe` (NetBIOS UDP 137) | registers via `init()` into `alive.RegisteredLANProbes()`; cmd/root.go blank-imports the package to enable them |
| Port scan / 端口扫描 | `internal/core/scan` | `scan.TCPConnectProbe` (3-way handshake) + `scan.UDPProbe` | orchestrated by `scan.NewScanner(opts).Run()` |
| Service fingerprinting / 服务指纹 | `internal/portscan/fingerprint` | nmap-service-probes.txt (Nmap PSL) — `fingerprint.NewVScan().MatchBanner(banner)` → service + version | wired into `internal/core/pipeline.go` as stage 0 of each iteration |
| HTTP fingerprinting / HTTP 指纹 | `internal/plugins/adapted/web/webtitle` | Nmap-style protocol detect + redirect follow + favicon mmh3 + FingerprintHub 3139 rules | `webtitle.WebTitlePlugin` (Identify) |
| Credential test / 凭据测试 | `internal/core/credential` | authenticators split under `auth/{database,email,filestorage,messaging,network,remote}/` — 26 protocols (database 8 + email 2 + filestorage 3 + messaging 1 + network 6 + remote 6) | orchestrated by `credential.Scheduler` (one-target inline via `core` pipeline); each protocol self-registers via `init()` so cmd/root.go's blank imports wire them up; coverage enforced by `TestRegistryHasAllAuthenticators` |

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

---

## Architecture overview / 架构概览

```
hostiter ──ch(host)──> portscan ──ch("host:port")──> pluginWorker
                                                       │
                                                       ├─ Identify plugin ─┐
                                                       └─ Credential plugin┴─> sink
sink = output (TXT + NDJSON + creds) + store (bbolt dedup)
```

### Project Workspace / 项目工作区

```
runs/projects/<name>/
├── fg.db           # bbolt state (hash dedup + results + creds)
├── targets.txt     # user-imported targets
├── result.txt      # scan / identify results (human-readable)
├── result.json     # NDJSON
├── creds.txt       # credential hits (no post-auth action)
├── rdp.json        # RDP deep fingerprint (NDJSON, v0.2+)
└── rdp.txt         # RDP deep fingerprint (text, v0.2+)
```

### Plugin contract / 插件契约

Every plugin implements:

每个插件实现：

```go
type Plugin interface {
    Name() string
    Ports() []int
    Modes() Mode                       // ModeIdentify | ModeCredential | both
    Identify(ctx, host, port) *Result  // banner / version / title
    Credential(ctx, host, port, creds []Cred) *Result  // test user:pass
}
```

The hard rule for `Credential()`:

`Credential()` 的硬性原则：

- On hit: return `*Result` with `Cred` set; pipeline writes to `creds.txt`; done.
  命中：返回带 `Cred` 字段的 `*Result`；管线写入 `creds.txt`；结束。
- NEVER call `ssh.NewSession` / `Exec` / `Shell` / any post-auth API.
  **绝不**调用 `ssh.NewSession` / `Exec` / `Shell` 或任何认证后 API。

This is enforced by code review and the `// HARD:` comments at the top of
each authenticator under `internal/core/credential/auth/<category>/`.

通过 code review 和 `internal/core/credential/auth/<category>/` 下每个 authenticator 顶部的 `// HARD:` 注释强制。

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

Global flags (subset; run `fg-qimen --help` for the full list):

| Short | Long | Default | Meaning |
|---|---|---|---|
| `-H` | `--host` | (empty) | target IP / CIDR / range / comma-list |
| `-f` | `--hosts-file` | (empty) | load targets from file |
| `-p` | `--project` | (empty) | project name (`""` = ephemeral) |
|     | `--mode` | `scan` | `scan` / `crack` / `linked` |
|     | `--resume` | false | resume from bbolt seen-set |
|     | `--no-state` | false | disable bbolt, in-memory only |
|     | `--ports` | `22,80,3306,3389,6379,8080` | comma-separated ports |
|     | `--exclude-ports` | (empty) | ports to exclude |
| `-a` | `--alive-only` | false | only host discovery; skip port scan |
| `-t` | `--threads` | `200` | concurrent workers |
|     | `--timeout` | `3s` | per-op timeout |
| `-u` | `--user` | (empty) | credential users (repeatable) |
| `-P` | `--pass` | (empty) | credential passwords (repeatable) |
|     | `--user-file` | (empty) | username dictionary |
|     | `--pass-file` | (empty) | password dictionary |
| `-o` | `--output-txt` | auto | result.txt path |
| `-j` | `--output-json` | auto | result.json path |
|     | `--silent` | false | suppress info log to console |
|     | `--no-tui` | false | force plain text mode |
|     | `--no-icmp` | false | skip ICMP probe |
| `-v` | `--verbose` | false | debug logging |
|     | `--shutdown-timeout` | `5s` | graceful drain timeout |

Dictionary file examples / 字典文件示例:

`--user-file` (one username per line) / `--user-file`（每行一个用户名）:

```
admin
root
test
oracle
postgres
```

`--pass-file` (one password per line; `#` lines are skipped) /
`--pass-file`（每行一个密码；`#` 开头的行被跳过）:

```
# top-10 worst passwords
123456
password
admin
root
qwerty
```

Usage / 用法:

```bash
fg-qimen -H 10.0.0.0/24 -p 22,3306 --user-file users.txt --pass-file pass.txt
fg-qimen scan --mode crack -H targets.txt --user-file users.txt --pass-file pass.txt -p corp
```

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

---

## Graceful Ctrl+C / 优雅退出

- First **Ctrl+C**: `cancel()` root context → pipeline drains → output flush
  → bbolt `Sync()` → exit code 130.
  第一次 **Ctrl+C**：`cancel()` 根 context → 管线排空 → 输出刷盘 → bbolt 同步 → 退出码 130。
- Second **Ctrl+C** within `--shutdown-timeout` (default 5s): hard exit (`os.Exit(1)`).
  在 `--shutdown-timeout`（默认 5 秒）内的第二次 **Ctrl+C**：强退 (`os.Exit(1)`)。

---

## Roadmap / 路线图

- **v0.1**: core architecture, ephemeral / project modes, TUI, ~13 service Identify plugins, 8 Credential authenticators (SSH / FTP / MySQL / Redis / Memcached / MongoDB / MSSQL / SMB), basic port scan, bbolt state.
  **v0.1**：核心架构、即扫即走/项目模式、TUI、约 13 个服务识别插件、8 个凭据认证器（SSH / FTP / MySQL / Redis / Memcached / MongoDB / MSSQL / SMB）、基础端口扫描、bbolt 状态。
- **v0.2 (current)**: full `cmd/ + internal/*` layout (7-phase refactor — see [RELEASE_NOTES_v0.2.md](docs/RELEASE_NOTES_v0.2.md)); credential authenticators expanded to 27 across 6 categories (database / email / filestorage / messaging / network / remote); `plugins/adapted/` split into 7 web/database/email/filestorage/messaging/network/remote subpackages; RDP deep fingerprint shipped; UDP port probe; ARP + NetBIOS LAN discovery (opt-in via blank import of `internal/discovery`); portfinger promoted to top-level `internal/portscan/fingerprint`; `common/` decomposed into types / output / store / ui / session leaf packages; `cmd/root.go` split into root / scan / resume / projects / version.
  **v0.2（当前）**：完整 `cmd/ + internal/*` 布局（7 阶段重构，见 [RELEASE_NOTES_v0.2.md](docs/RELEASE_NOTES_v0.2.md)）；凭据认证器扩展为 6 个类别下共 27 个；`plugins/adapted/` 拆为 7 个 web/database/email/filestorage/messaging/network/remote 子包；RDP 深度指纹落地；UDP 端口探测；ARP + NetBIOS LAN 发现（通过 blank import `internal/discovery` 启用）；portfinger 升级到顶层 `internal/portscan/fingerprint`；`common/` 拆为 types/output/store/ui/session 叶子包；`cmd/root.go` 拆为 root / scan / resume / projects / version。
- **v0.3+**: full fake-server integration tests for MSSQL / SMB / RDP; smarter credential-scheduler with per-plugin rate limits; output rotation; project import/export; richer HTTP fingerprinting (CMS / WAF detection); first-class IPv6 target support.
  **v0.3+**：MSSQL / SMB / RDP 完整假服务器集成测试；带按插件限速的智能凭据调度；输出滚动；项目导入导出；更丰富的 HTTP 指纹（CMS / WAF 检测）；一等公民 IPv6 目标支持。

---

## Attribution / 致谢

FG-QiMen stands on the shoulders of several open-source projects.
All reused code is MIT-licensed; the per-file modification history
lives in the source headers.

FG-QiMen 站在多个开源项目的肩膀上。所有重用的代码均采用 MIT
许可证；逐文件的修改历史在源码头部注释里。

### Inspiration / 灵感来源

- **[fscan](https://github.com/shadow1ng/fscan)** by [shadow1ng](https://github.com/shadow1ng) (MIT) — the
  pipeline-decoupled scanner architecture, the service Identify +
  Credential plugin pattern, and the Nmap-style port-fingerprint
  framework that FG-QiMen's `internal/plugins/adapted/web/webtitle/`, `internal/portscan/fingerprint/`,
  and most `internal/plugins/adapted/{database,email,filestorage,messaging,network,remote,web}/*` plugins are based on. Hard rule:
  FG-QiMen inherits the **no-exploit** policy and drops every
  unauthorized-access / write / POC path the original carried.
  fscan 本身亦未包含利用代码，FG-QiMen 在此之上进一步剥离了
  任何接近"攻击面"的实现。

  中文：fscan 是 [shadow1ng](https://github.com/shadow1ng) 的 MIT
  许可项目。FG-QiMen 的管道解耦架构、Identify+Credential 插件
  模式、Nmap 风格 port-fingerprint 框架均借鉴自 fscan。fscan
  与 FG-QiMen 都坚持**不做漏洞利用**——本项目在此基础上进一步
  剥离了任何接近"攻击面"的代码路径。

### Code dependencies / 代码依赖

- [`golang.org/x/crypto/ssh`](https://pkg.go.dev/golang.org/x/crypto/ssh) — SSH authentication (auth only).
- [`github.com/jlaffaye/ftp`](https://github.com/jlaffaye/ftp) — FTP client.
- [`github.com/go-sql-driver/mysql`](https://github.com/go-sql-driver/mysql) — MySQL driver.
- [`github.com/microsoft/go-mssqldb`](https://github.com/microsoft/go-mssqldb) — MSSQL driver.
- [`github.com/hirochachacha/go-smb2`](https://github.com/hirochachacha/go-smb2) — SMB2/3 client.
- [`golang.org/x/crypto/pbkdf2`](https://pkg.go.dev/golang.org/x/crypto/pbkdf2) — PBKDF2 for MongoDB SCRAM-SHA-256.
- [`github.com/charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea) + [`lipgloss`](https://github.com/charmbracelet/lipgloss) — TUI.
- [`go.etcd.io/bbolt`](https://github.com/etcd-io/bbolt) — embedded KV for project state.
- [`github.com/spf13/cobra`](https://github.com/spf13/cobra) — CLI framework.
- [`github.com/0x727/FingerprintHub`](https://github.com/0x727/FingerprintHub) — community fingerprint database (3139 JSON rules).

### Embedded data / 内嵌数据

- **nmap-service-probes** (Nmap Public Source License) — embedded
  in `internal/portscan/fingerprint/` for service fingerprinting.

### License / 许可证

The FG-QiMen source is released under the MIT License. See [LICENSE](LICENSE).


---

## Disclaimer / 免责声明

This tool is for **authorized security testing and learning only**. Do not
scan targets without permission. The authors are not responsible for
misuse.

本工具仅供**合法授权的安全测试和学习使用**。请勿对未授权目标进行扫描。
作者不承担任何滥用造成的后果。
