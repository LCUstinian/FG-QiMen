# FG-QiMen

> 一个带项目工作区的管道扫描器

FG-QiMen 是一个**纯 CLI 扫描器**，通过 Go channel 管道把**端口扫描（producer）**
和**插件 worker（consumer）**解耦。支持三种运行模式（`scan` / `crack` /
`linked`）和两种工作模式（即扫即走 vs 带 bbolt 状态的增量项目工作区）。

[English](README.md) · [发行版](https://github.com/LCUstinian/FG-QiMen/releases) · [更新日志](CHANGELOG.md)

```
┌─ FG-QIMEN v0.5 ──── project: corp-intranet ──── mode: linked ─┐
│ ⟳ Scanning... elapsed 00:01:23  throughput 142 pps             │
├──────────────────────┬──────────────────────────────────────────┤
│ 目标                 │ 实时事件                                │
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

## 硬性原则：探测，不攻击

FG-QiMen **故意不包含任何漏洞利用代码**。扫描器用标准认证握手测试已知
服务；命中时凭据写入 `creds.txt` 即终止——不执行 `Session.Exec`、不上
WebShell、不植入持久化。完整的"不做漏洞利用"契约（明确禁止的能力清单）
见 [`docs/SECURITY.md`](docs/SECURITY.md)。

---

## 快速开始

```bash
# 即扫即走
fg-qimen -H 192.168.1.0/24

# 项目模式 + bbolt 状态
fg-qimen -p corp -H 10.0.0.0/24 -mode linked

# 续传项目
fg-qimen resume -p corp

# 列出所有项目
fg-qimen projects list
```

### 构建

需要 Go 1.22+ 和 [`just`](https://github.com/casey/just)。

```bash
just build         # → release/fg-qimen[.exe]
just all           # → release/fg-qimen-{os}-{arch}[.exe]
just --list
```

### 基本扫描

```bash
# 扫 /24 网段，使用默认端口
fg-qimen -H 192.168.1.0/24

# 指定端口
fg-qimen -H 192.168.1.0/24 --ports 22,80,443,3389,8080

# 单主机
fg-qimen -H 10.0.0.5 --ports 22,80,3306,6379,8080 -t 50

# 自定义输出路径
fg-qimen -H 10.0.0.5 -o myscan.txt -j myscan.json
```

### 项目模式

```bash
# 一次性创建项目
fg-qimen projects create corp-intranet

# 录入目标
echo "10.0.0.0/24"   >  runs/projects/corp-intranet/targets.txt
echo "10.0.1.0/24"   >> runs/projects/corp-intranet/targets.txt

# linked 模式（扫描 + 凭据测试 一把过）
fg-qimen -p corp-intranet -f runs/projects/corp-intranet/targets.txt -mode linked \
    -u root admin -P 123456 admin P@ssw0rd

# 续传 / 查看信息
fg-qimen resume -p corp-intranet
fg-qimen projects info corp-intranet
```

### TUI 模式

TUI 在 stdout 是 TTY 时**默认开启**。强制纯文本：

```bash
fg-qimen -H 127.0.0.1 --no-tui
```

### 字典文件

```text
# users.txt（每行一个用户名）
admin
root
test
oracle
postgres
```

```text
# pass.txt（每行一个密码；以 # 开头的行跳过）
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

## 功能特性

### 架构

- **管道解耦**：端口扫描 (producer) → `chan ScanItem` → 插件 worker (consumer)。
  所有阶段遵循 `context.Context` 实现取消。
- **三种运行模式**：`scan` / `crack` / `linked`（见 [CLI 参考](#cli-参考)）。
- **项目工作区**：每个项目独立目录 + bbolt DB。
- **增量追踪**：基于 SHA-1 的去重，可选 bbolt 持久化；`--resume` 重载已见集合。
- **TUI**：Bubbletea + Lipgloss 赛博朋克配色（黑底绿/琥珀/红）；非 TTY
  自动降级纯文本。

完整架构文档：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)。

### 输出格式

- `result.txt` — 人类可读
- `result.json` — NDJSON（每行一个 JSON 对象）
- `result.csv` — RFC 4180，每条结果一行
- `creds.txt` — 凭据命中（明文；操作员的工作文件）
- `rdp.json` / `rdp.txt` — RDP 深度指纹（hostname、build、NLA 标志、OS）

### 插件（44 个插件 / 认证器）

| 插件 | 默认端口 | 探测 | 凭据测试 |
|---|---|---|---|
| `ssh` | 22, 2222, 2200, 22222 | ✅ | ✅（仅密码；不调 Session/Exec） |
| `http` | 80, 443, 8080, 8443, 8000, 8888 | ✅ | – (v0.2+) |
| `webtitle` | 80, 443, 8080, 8443 | ✅（FingerprintHub 3139 规则 + favicon） | – |
| `redis` | 6379, 6380 | ✅（PING / PONG） | ✅（RESP AUTH） |
| `mongodb` | 27017, 27018 | ✅（OP_MSG hello） | ✅（SCRAM-SHA-256 via OP_MSG） |
| `postgresql` | 5432, 5433, 5434 | ✅（StartupMessage） | ✅（lib/pq via `db.PingContext`） |
| `mssql` | 1433, 1434, 2433 | ✅（TDS via go-mssqldb） | ✅（TDS Login7 via go-mssqldb） |
| `smb` | 445, 139 | ✅（SMB magic） | ✅（SMB2 Session Setup NTLMv2） |
| `smtp` | 25, 465, 587, 2525 | ✅（EHLO） | – (v0.2+) |
| `snmp` | 161, 162 | ✅（sysDescr.0 原文） | – (v0.2+) |
| `snmpv3` | 161, 162 | ✅（GetRequest v3） | |
| `ldap` | 389, 636 | ✅（BindRequest + SearchRequest） | – (v0.2+) |
| `memcached` | 11211, 11212 | ✅（text "version\r\n"） | ✅（ASCII "auth" 探针） |
| `elasticsearch` | 9200, 9300 | ✅（HTTP GET /） | ✅（HTTP Basic） |
| `rdp` | 3389 | ✅（TPKT/X.224/MCS 四步） | |
| `rdpnla` | 3389 | ✅（RDP NLA 状态 HYBRID/SSL/旧版） | |
| `vnc` | 5900–5905 | ✅（RFB 003.x banner） | ✅（RFB 握手 + DES 挑战） |
| `telnet` | 23, 2323 | ✅（IAC-stripped banner） | ✅（IAC + 提示符 + user/pass 流） |
| `oracle` | 1521, 1526, 2483 | ✅（TNS Connect/Accept） | ✅（TNS 握手 via go-ora） |
| `winrm` | 5985, 5986 | ✅（GET /wsman） | ✅（HTTP Basic + WSMan SOAP） |
| `pop3` | 110, 995 | ✅（+OK 问候） | ✅（RFC 1939 USER/PASS） |
| `imap` | 143, 993 | ✅（`* OK` 问候） | ✅（RFC 3501 LOGIN） |
| `socks5` | 1080 | ✅（SOCKS5 VER 5） | ✅（RFC 1928/1929 user/pass） |
| `rsync` | 873, 8873 | ✅（`@RSYNCD:` 问候） | ✅（USERNAME + MD5 挑战） |
| `docker` | 2375, 2376 | ✅（GET /_ping + /info） | ✅（HTTP Basic to /images/json） |
| `rabbitmq` | 5672 | ✅（AMQP 0-9-1 header + Start） | ✅（AMQP PLAIN） |
| `mqtt` | 1883, 8883 | ✅（MQTT 3.1.1 / 5.0 CONNECT/CONNACK） | |
| `activemq` | 61616 | ✅（OpenWire stub） | |
| `kafka` | 9092 | ✅（ApiVersions v0+） | |
| `rocketmq` | 9876 | ✅（RemotingCommand stub） | |
| `modbus` | 502 | ✅（Read Device Identification） | ✅（仅读设备 ID；不写线圈/寄存器） |
| `ipmi` | 623 (UDP) | ✅（RMCP+ Session Open） | ✅（RAKP v2.0 HMAC-SHA1） |
| `bacnet` | 47808 (UDP) | ✅（BACnet/IP Who-Is → I-Am） | ✅（可达性探针） |
| `ntp` | 123 (UDP) | ✅（NTPv4 client, Mode=4） | |
| `tftp` | 69 (UDP) | ✅（RRQ → DATA/ERROR） | |
| `dns` | 53 (UDP) | ✅（CHAOS version.bind + root A） | |
| `nfs` | 2049 | ✅（ONC RPC NULL call） | ✅（RPC NULL；无 AUTH_GSS） |
| `jenkins` | 8080, 8443, 50000 | ✅（Jenkins crumb + version） | |
| `kibana` | 5601 | ✅（Kibana status API） | |
| `weblogic` | 7001, 7002, 8443 | ✅（WebLogic console 登录页） | |
| `aws` | 80（云元数据） | ✅（IMDSv1 + IMDSv2 指纹） | |
| `azure` | 80（云元数据） | ✅（Azure IMDS 指纹） | |

凭据测试覆盖 **21 个服务**（SSH + Redis + MongoDB + PostgreSQL + MSSQL + SMB +
Memcached + Elasticsearch + VNC + Telnet + Oracle + WinRM + POP3 + IMAP +
SOCKS5 + Rsync + Docker + RabbitMQ + Modbus + IPMI v2.0 + BACnet + NFS），均
强制不做漏洞利用（`creds.txt` 是唯一副作用）。

IPv6 是一等公民（单 IP / CIDR / 逗号列表）。自定义 Web 指纹规则集通过
`--web-fingerprint <path-or-url>` 加载（本地文件或 HTTP URL，可从规则服务
器 live-update）。RDP NLA 状态（HYBRID / SSL / 旧版）由 `rdp-nla` 插件
探测；完整 CredSSP 认证延期。

---

## CLI 参考

```
fg-qimen [flags]
fg-qimen scan [flags]                        # 显式 scan
fg-qimen resume -p <name>                    # 续传项目
fg-qimen projects list                       # 列出项目
fg-qimen projects create <n>                 # 创建项目
fg-qimen projects delete <n>                 # 删除项目
fg-qimen projects info <n>                   # 查看项目详情
fg-qimen projects export <n> <out.fgq>       # 导出项目到单 .fgq 文件
fg-qimen projects import <in.fgq> <n>       # 从 .fgq 文件导入
fg-qimen version                             # 显示版本
fg-qimen completion bash                     # 生成 shell 补全
```

### 5 个最常用 flag（覆盖 ~90% 场景）

| 短 | 长 | 例子 | 用途 |
|---|---|---|---|
| `-H` | `--host` | `-H 10.0.0.0/24` | 目标 IP / CIDR / 范围 / 逗号列表 |
| `-p` | `--project` | `-p corp` | 命名项目（持久化到 bbolt；省则即扫即走） |
| `-u` | `--user` | `-u root admin` | 内联用户名 |
| `-U` | `--user-file` | `-U users.txt` | 用户名字典文件（每行一个） |
| `-W` | `--pass-file` | `-W pass.txt` | 密码字典文件（每行一个；用 `-W` 不用 `-P` 避免与 `-P`/`--pass` 内联冲突） |

实用命令：

```bash
# 最小：256-host /24 扫默认端口
fg-qimen -H 10.0.0.0/24

# 命名项目 + 字典 + 小并发
fg-qimen -p corp -H 10.0.0.0/24 -U users.txt -W pass.txt -t 50

# 续传已有项目
fg-qimen resume -p corp

# 仅凭据测试：跳过 alive + 端口扫描
fg-qimen scan -p corp -mode crack -U users.txt -W pass.txt

# 走 HTTP 代理（自动套到所有插件的拨号器）
fg-qimen -H 10.0.0.0/24 -X http://127.0.0.1:8080
```

### 完整 flag 参考（v0.4.1 — 45 个 flag，17 个有短选项）

| 短 | 长 | 默认 | 分组 | 含义 |
|---|---|---|---|---|
| `-H` | `--host` | （空） | Target | 目标 IP / CIDR / 范围 / 逗号列表（如 `10.0.0.0/24,192.168.1.0/24`） |
| `-f` | `--hosts-file` | （空） | Target | 从文件加载目标（每行一个 host；`#` 开头的行跳过） |
| `-p` | `--project` | （空） | Workspace | 项目名；空 = 即扫即走（无 bbolt） |
|     | `--project-key` | （空） | Workspace | 加密项目 DB 用的 passphrase（AES-256-GCM，v0.4+ 走 Argon2id 派生）。空 = 明文（v0.2.x 兼容）。环境变量：`FG_QIMEN_PROJECT_KEY` |
| `-M` | `--mode` | `scan` | Workspace | `scan`（alive→scan→identify）/ `crack`（仅凭据测试）/ `linked`（scan + 凭据） |
|     | `--resume` | `false` | Workspace | 从 bbolt seen-set 续传（跳过已见过 host:port 对） |
|     | `--no-state` | `false` | Workspace | 禁用 bbolt，纯内存；项目退出时清空 |
|     | `--ports` | `22,80,3306,3389,6379,8080` | Ports | 逗号分隔端口列表 |
|     | `--exclude-ports` | （空） | Ports | 从解析后的端口列表中排除 |
|     | `--no-icmp` | `false` | Ports | 跳过 ICMP alive 探活（敌对网络下的纯 TCP 模式） |
| `-X` | `--proxy` | （空） | Network | HTTP/HTTPS 代理 URL（如 `http://127.0.0.1:8080`）。通过 `credential.DialTCP` / `DialTCPAddr` 在所有 TCP 拨号站点生效（Phase 2.2）。 |
|     | `--socks5` | （空） | Network | SOCKS5 代理 URL（如 `socks5://user:pass@127.0.0.1:1080`） |
|     | `--iface` | （空） | Network | 出站连接绑定的本地 IP |
| `-t` | `--threads` | `200` | Concurrency | plugin 池的并发 worker 数 |
|     | `--max-workers` | `16` | Concurrency | `--threads` 的硬上限（给自动缩放器加 cap） |
|     | `--timeout` | `3s` | Concurrency | 单次操作超时（覆盖 alive 探活、端口扫描 connect、插件握手） |
| `-a` | `--alive-only` | `false` | Concurrency | alive 后就停；不跑 scan / identify / credential |
| `-u` | `--user` | （空） | Credentials | 内联用户名（逗号分隔） |
|     | `--pass` | （空） | Credentials | 内联密码（逗号分隔；`-P` 短选项） |
| `-U` | `--user-file` | （空） | Credentials | 用户名字典文件（每行一个；`-U` 短选项，v0.4.1+） |
| `-W` | `--pass-file` | （空） | Credentials | 密码字典文件（每行一个；`-W` 不用 `-P` 以免与 `-P`/`--pass` 内联冲突） |
| `-o` | `--output-txt` | （空） | Output | TXT 结果文件路径（`-o` 短选项） |
| `-j` | `--output-json` | （空） | Output | NDJSON 结果文件路径（`-j` 短选项） |
|     | `--output-csv` | （空） | Output | CSV 结果文件路径（每条结果一行，列序稳定便于 awk / pandas） |
|     | `--output-sarif` | （空） | Output | SARIF 2.1.0 JSON 路径（单文档，给 GitHub Code Scanning） |
|     | `--rotate-bytes` | `0` | Output | 单文件大小阈值触发轮转（0 = 不轮转）。v0.4.1 从 `--output-rotate-bytes` 改名——`output-` 前缀冗余，因为 `rotate` 在整个 flag 空间里唯一归属输出子系统。 |
|     | `--rotate-files` | `0` | Output | 保留总文件数（含现行，0 = 不轮转）。v0.4.1 从 `--output-rotate-files` 改名。 |
|     | `--show-creds` | `false` | Output | 在 `result.txt` 强制明文凭据（`creds.txt` 始终明文） |
|     | `--plugins` | （空） | Output | 逗号分隔插件白名单（如 `--plugins ssh,redis,vnc`）；空 = 全部 |
|     | `--web-fingerprint` | （空） | Output | 额外 FingerprintHub 风格 web 规则文件或 URL |
|     | `--http-form-url` | （空） | Output | HTTP form-brute 插件的目标 URL（opt-in） |
|     | `--http-form-fields` | `user=$user$,pass=$pass$` | Output | form-brute 插件的字段模板 |
|     | `--http-form-success` | （空） | Output | 命中响应的子串特征 |
|     | `--http-form-failure` | `invalid` | Output | 失败响应的子串特征 |
|     | `--http-form-redirect` | （空） | Output | 设置时跟随重定向，并用此子串在最终响应中判断命中 |
|     | `--silent` | `false` | Behavior | 抑制 banner / 实时事件日志 |
|     | `--no-tui` | `false` | Behavior | 即使 stdout 是 TTY 也强制纯文本输出 |
|     | `--no-batch` | `false` | Behavior | 禁用 bbolt 批量写（每次 Put 都 fsync 而非批量） |
| `-v` | `--verbose` | `false` | Behavior | 详细日志（来自插件的 debug 级） |
|     | `--insecure-tls` | `false` | Safety | 跳过 TLS 证书校验（探测用；不安全——见 HARD 规则） |
|     | `--insecure-ssh` | `false` | Safety | 跳过 SSH 主机密钥校验（不安全——见 HARD 规则） |
|     | `--known-hosts` | （空） | Safety | `known_hosts` 文件路径（设为非空后 `InsecureIgnoreHostKey` 自动转 false） |

完整 CLI 用法模板（按工作流分类）见
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md)。当前 `fg-qimen --help`
输出是权威参考。

---

## 验证发布

每个 GitHub Release 都会发布 **11 个平台二进制**，外加每个二进制的 cosign
签名、签名证书、CycloneDX SBOM：

| 文件 | 用途 |
|---|---|
| `fg-qimen-<platform>` | 编译后的二进制（Linux/macOS/BSD 无 `.exe`） |
| `SHA256SUMS` | 所有二进制的 sha256 校验和 |
| `*.sig` | cosign keyless 签名（OIDC、Sigstore） |
| `*.pem` | 含 OIDC 身份的签名证书 |
| `*.sbom.json` | 平台级 CycloneDX SBOM |
| `FG-QiMen-release.spdx.json` | 跨 11 平台的全量 SPDX SBOM |

### 1. 校验和

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

通过则每行打印 `<binary>: OK`；不匹配会以非零退出码中止。

### 2. 签名（keyless、OIDC）

发布流程用 [cosign](https://github.com/sigstore/cosign) keyless 模式
对接 Sigstore 公共服务实例——仓库不存任何私钥。

```bash
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --signature fg-qimen-<platform>.sig \
  --certificate fg-qimen-<platform>.pem \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-<platform>
```

验证成功会打印已验证二进制的 SHA256 与签名身份的 OIDC subject。证书
固定了 GitHub Actions workflow 身份
（`https://github.com/LCUstinian/FG-QiMen/.github/workflows/release.yml@refs/tags/<TAG>`）。

### 3. SBOM

CycloneDX SBOM 列出二进制链接的所有直接或间接依赖。可直接被
[Dependency-Track](https://dependencytrack.org/) 或任何支持 SBOM 的 SCA
工具摄取。

```bash
# 查看组件清单
jq '.components[] | {name, version, purl}' fg-qimen-<platform>.sbom.json
```

### 4. 源码可重现（可选）

从对应 tag 逐字节重建二进制：

```bash
git checkout <TAG>
go build -trimpath -ldflags='-s -w -buildid=' -o fg-qimen-local .
sha256sum fg-qimen-local
```

哈希必须匹配 `SHA256SUMS` 中对应行。

### 5. 反馈不一致

若以上任何步骤失败，**请勿运行该二进制**。在
<https://github.com/LCUstinian/FG-QiMen/issues> 开 issue 并附失败
步骤的输出与所试 tag。

---

## 本地化策略

- **代码注释**：双语（中英）—— 每个公开函数/结构体/关键逻辑块都有。
- **终端输出**：纯英文（banner、help、日志、错误）。
- **README**：拆分为英文（[README.md](README.md)）+ 简体中文
  （[README.zh-CN.md](README.zh-CN.md)）。
- **CLI flag 名**：英文。

## 优雅退出（Ctrl+C）

- 第一次 **Ctrl+C**：`cancel()` 根 context → 管线排空 → 输出刷盘 →
  bbolt 同步 → 退出码 130。
- 在 `--shutdown-timeout`（默认 5 秒）内的第二次 **Ctrl+C**：强退
  （`os.Exit(1)`）。

---

## 路线图

下一里程碑（完整历史见 [CHANGELOG.md](CHANGELOG.md)）：

- **v0.4**：完整 crack-mode 重构；统一跨插件的代理；逐 attempt 读
  截止审计（v0.3.1 已对 7 个最严重的修了）。
- **v0.5+**：完整的 fake-server 集成测试（MSSQL / SMB / RDP）；输出
  轮转；项目导入/导出；更丰富的 HTTP 指纹。

---

## 致谢

FG-QiMen 站在多个开源项目的肩膀上。所有重用的代码均采用 MIT 许可证；
逐文件的修改历史在源码头部注释里。

**主要灵感来源**：[fscan](https://github.com/shadow1ng/fscan) by
[shadow1ng](https://github.com/shadow1ng)（MIT）—— 管道解耦的扫描器架构、
"探测 + 凭据"插件模式、Nmap 风格的端口指纹框架。FG-QiMen 继承其
**不做漏洞利用** 的策略，并剥离原项目中所有接近"攻击面"的代码路径
（unauthorized-access / write / POC）。

完整第三方许可证文本：[`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md)。

FG-QiMen 源码以 MIT 许可证发布。见 [LICENSE](LICENSE)。

---

## 免责声明

本工具仅供**合法授权的安全测试和学习使用**。请勿对未授权目标进行扫描。
作者不承担任何滥用造成的后果。
