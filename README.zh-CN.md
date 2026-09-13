# FG-QiMen

> **功能多 ≠ 更好。** 纯扫描器 + 凭据测试器。无漏洞利用、无持久化、无认证后动作——设计使然。

> 把扫描与识别做到极致：足够全、足够深、足够快、足够稳。

FG-QiMen 是一个纯 CLI 扫描器，通过 Go channel 管线解耦**端口扫描器（生产者）**与
**插件 worker（消费者）**。支持三种运行模式（`scan` / `crack` / `linked`）与两种工作
模式（即扫即走 vs 带持久化 bbolt 状态的项目工作区）。

[English](README.md) · [Releases](https://github.com/LCUstinian/FG-QiMen/releases) · [更新日志](CHANGELOG.zh-CN.md)

```
┌─ FG-QIMEN <version> ── project: corp-intranet ── mode: linked ─┐
│  [ ▶ IDENTIFY ]  ETA ~12s  alive ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░ 18/24  ports ▓▓░░░░░░░░░░░░░░░░░░░░ 142/8000  rate 142 pps · 28 hits/s  ▁▂▃▅▇▅▃▂▁
├────────────────────────────────────────────────────────────────┤
│ LIVE EVENTS                                                     │
│   [14:23:01] ✓ 10.0.0.5:22      ssh                            │
│   [14:23:02] ✓ 10.0.0.7:80      http                           │
│   [14:23:04] ✓✓ 10.0.0.12:3306  mysql     [admin/admin OK!]    │
│   [14:23:07] ⚠ 10.0.0.18:443    https     [TLS handshake fail]│
│   [14:23:09] ✗ 10.0.0.22:23     telnet                         │
├──────────────────────┬─────────────────────────────────────────┤
│ STAGE                │ TOP PLUGINS                             │
│   alive       18/24  │   [ssh     12] ███████████░░░░░░░       │
│   ports    142/8000  │   [http      7] ███████░░░░░░░░░░░       │
│   results      23    │   [mysql     2] ██░░░░░░░░░░░░░░░░       │
│   creds        2     │   [redis     1] █░░░░░░░░░░░░░░░░░       │
│   errors      7     │   [https     1] █░░░░░░░░░░░░░░░░░       │
├──────────────────────┴─────────────────────────────────────────┤
│ ERRORS: timeout 42  refused 15  dns 7  reset 3                 │
├────────────────────────────────────────────────────────────────┤
│ [q] quit  [p] pause  e errors panel  L live overlay  ? toggle help │
└────────────────────────────────────────────────────────────────┘
```

---

## 纯扫描器定位

扫描器 + 凭据测试器，用于授权场景。FG-QiMen 止步于扫描、识别与凭据
验证——无漏洞利用、无认证后动作、无持久化。这是战术选择，不是功能
缺失：

- **攻击流量是最显眼的流量。** 在有防护的内网，最容易触发告警的恰恰
  是未授权访问尝试与 POC 投放；纯扫描可以在防守方察觉之前安静跑完。
  噪声预算留给下一步那一次有针对性的动作。
- **现成的攻击天然价值有限。** 对随机目标投放通用 exploit，几乎总是
  输给人工读完结构化结果后挑出的那一个目标。
- **交付物是决策级数据。** 结构化服务身份、Web/TLS 指纹、凭据命中
  ——机器负责测绘地形，人负责瞄准下一步动作。

完整契约见 [`docs/SECURITY.zh-CN.md`](docs/SECURITY.zh-CN.md)。

---

## 为什么是 FG-QiMen

一个二进制，两种姿态：既是日常资产盘点用的**普通内网扫描器**，也是
实战中的**低噪声、高自由度红队/APT 侦察工具**——后者场景下每一包
流量都要"物有所值"。支撑两者的不是"功能更多"，而是三项承诺：

1. **先测量，再扫描。** 环境画像先对目标集采样 RTT 与丢包，再从活数据
   推导超时与并发：64 样本 RTT 环的 mean+4σ 超时、慢启动 + AIMD 拥塞控制
   线程池。快速局域网把 3s 等待压到 ~600ms（**≈5× 提速**）；丢包严重的
   广域网主动退避，而不是陷入重试风暴。你显式设置的值永远赢。
2. **识别，而不只是连通。** 每个服务命中都带结构化
   `product`/`version`/`confidence` 身份三元组——TCP 默认开启，UDP
   （`--udp`）探测可选，payload 来自 nmap。Web 命中额外给出状态码/标题/
   Server/命中指纹；HTTPS 再加 **TLS 叶子证书身份**（SAN/CN 字段经常
   暴露 banner 匹配永远看不到的内网主机名）；RDP 给出 build/NLA/OS 姿态。
3. **交付证据，而不是承诺。** 每个发布都带 cosign 无密钥签名、CycloneDX +
   SPDX SBOM、SLSA L2 来源证明——运行前可验证，也可从 tag 重建后逐字节
   比对哈希。

### 正面对比

| 维度 | 固定参数扫描器 | FG-QiMen |
|---|---|---|
| **超时** | 单一静态单次探测值——filtered 端口每台主机都白等一遍 | 64 样本 RTT 环的 mean+4σ：快速局域网 3s → ~600ms（**≈5× 提速**），慢路径保留操作员上限 |
| **并发** | 固定线程数；一个拥塞网段拖垮整轮扫描 | AIMD 线程池——慢启动、健康时加性增长、拥塞/RTT 信号触发乘性回退；`--threads` 始终是硬上限 |
| **死目标浪费** | 每个网段每台主机都探 | 两阶段 /24 网关预筛 + 主机排除（CIDR、范围、`192`/`172`/`10` RFC1918 快捷方式）——死网段零流量 |
| **服务覆盖** | 仅 TCP | 可选 UDP 探测（nmap payload 库），与 TCP 同构的结构化身份（`--udp`、`--udp-strict`） |
| **身份深度** | "端口开放" + 原始 banner | 结构化 product/version/confidence；Web：状态码/标题/Server/指纹 + TLS SAN/CN；RDP：build/NLA/OS |
| **状态与恢复** | 一次成型；Ctrl+C 意味着整轮重来 | bbolt 项目工作区：resume、prune、export/import、cron 调度 |
| **操作员体验** | 日志行刷屏滚过 | 实时 TUI（阶段 ETA、命中流、插件榜、错误分类）或干净纯文本；txt/json/csv sink + 日桶 |
| **供应链** | 裸二进制 | cosign 签名、双 SBOM、SLSA L2 来源证明、CI action 锁定 SHA——先验证再运行 |

### 实际收益

- **健康的 /24 局域网**：画像收紧超时预算，线程池爬满并发，一轮扫描
  秒级完成——产出结构化 JSON，可直接喂给其他工具。
- **丢包的 VPN/广域网**：线程池乘性退避而不是硬打——更少的假"不可达"
  结论，更少的重试风暴。
- **被打断的扫描**：Ctrl+C 排空管线并落盘状态；`resume` 接着跑，
  而不是从零再来。

---

## 快速开始

```bash
# 即扫即走
fg-qimen -H 192.168.1.0/24

# 持久化项目（bbolt 状态）
fg-qimen --project corp -H 10.0.0.0/24 --mode linked

# 恢复暂停的项目
fg-qimen resume --project corp

# 列出项目
fg-qimen projects list
```

### 构建

需要 Go 1.26+ 与 [`just`](https://github.com/casey/just)。

```bash
just build         # → release/fg-qimen[.exe]
just all           # → release/fg-qimen-{os}-{arch}[.exe]
just --list
```

### 基本扫描

```bash
# /24 网段 + 默认端口
fg-qimen -H 192.168.1.0/24

# 指定端口
fg-qimen -H 192.168.1.0/24 --ports 22,80,443,3389,8080

# 单主机
fg-qimen -H 10.0.0.5 --ports 22,80,3306,6379,8080 -t 50

# 自定义输出路径
fg-qimen -H 10.0.0.5 -ot myscan.txt -oj myscan.json
```

> **提示 — 别让扫描输出污染仓库根**：默认工作区（结果 sink、bbolt 状态、日桶）
> 建在相对 cwd 的 `./fgqm_workspace`。用 `--workspace <dir>` 或环境变量
> `FGQI_WORKSPACE`（flag 优先）把它指到别处，调试 run 和临时扫描就不会弄脏项目目录。

> **提示 — Windows 下加速存活探测**：非管理员无法打开 ICMP raw socket，存活探测
> 会退化为逐主机 spawn `ping.exe`（可用但较慢）。用管理员终端运行扫描器即可启用
> 快速 ICMP 路径。

### 项目模式

```bash
# 一次性创建项目
fg-qimen projects create corp-intranet

# 填入目标
echo "10.0.0.0/24"   >  fgqm_workspace/projects/corp-intranet/targets.txt
echo "10.0.1.0/24"   >> fgqm_workspace/projects/corp-intranet/targets.txt

# linked 模式（扫描 + 凭据测试一轮完成）
fg-qimen --project corp-intranet -f fgqm_workspace/projects/corp-intranet/targets.txt --mode linked \
    -u root,admin -p 123456,admin P@ssw0rd

# 恢复 / 详情
fg-qimen resume --project corp-intranet
fg-qimen projects info corp-intranet

# 保留策略：删除早于截止日期的恢复状态（seen-hashes）。
# 结果 / 凭据文件永不触碰。--yes 跳过确认提示。
fg-qimen projects prune corp-intranet --before 2026-09-01 --compact --yes
```

### TUI

stdout 为 TTY 时 TUI **默认开启**。用 `--no-tui` 强制纯文本输出。

仪表盘由 3 断点响应式布局（窄 <80 / 中 80–119 / 宽 ≥120 列）驱动的六个区域
组成；宽终端把 STAGE 与 TOP PLUGINS 并排放，窄终端堆叠：

- **Header**：右侧带 ETA 的分阶段 `[ ▶ STAGE ]` 徽标（`[ ▶ ALIVE ]   ETA ~12s`）；
  扫描速率 hits/s 与 ports/s（EWMA 平滑）加 60 样本 hits/s 迷你走势图；
  alive 扫描进行中 "alive N/M" 计数随探测完成实时跳动（不再是直到 alive
  阶段结束才从 0 跳变）。
- **LIVE EVENTS**：固定环形缓冲的最后 20 条事件（永不增长），按严重度着色
  （`✓` 凭据命中、`✗` 错误、`⚠` 警告）；每条命中红色闪烁约 200ms。窄终端隐藏；
  `L` 叠层显示最近 5 条。
- **STAGE**（左/上）：alive 与 ports 以 `▓/░` 进度条对照总量渲染；
  results / creds / errors 保持纯计数。
- **TOP PLUGINS**（右/下）：本轮命中最多的 5 个插件，固定宽度条形图，左侧
  `[plugin N]` 名称 + `████░░` 占比条。
- **ERRORS**（底部）：紧凑的 `ERRORS: timeout 42  refused 15` 一行；`e` 展开为
  top-4 分类条形，`E` 收回。分类来自 `core.ClassifyError`（先 errors.Is /
  errors.As，退化为子串匹配）。
- **Footer**：按键提示——`[q] quit  [p] pause  e errors panel
  L live overlay  ? toggle help`（`?` 打开完整帮助叠层）。

以上全部读取 `internal/types.State` 上的 `CountersView` 投影，视图层与扫描器
内部 channel 布局解耦。

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
# pass.txt（每行一个密码；`#` 行跳过）
# top-10 worst passwords
123456
password
admin
root
qwerty
```

```bash
fg-qimen -H 10.0.0.0/24 --ports 22,3306 -uf users.txt -pf pass.txt
fg-qimen scan --mode crack -f targets.txt -uf users.txt -pf pass.txt --project corp
```

---

## 功能特性

### 架构

- **管线解耦**：端口扫描（生产者）→ `chan ScanItem` → 插件 worker（消费者）。
  所有阶段遵循 `context.Context` 取消语义。
- **三种运行模式**：`scan` / `crack` / `linked`（见 [CLI 参考](#cli-参考)）。
- **项目工作区**：每个项目独立目录 + bbolt DB。
- **增量追踪**：SHA-1 去重 + 可选 bbolt 持久化；`--resume` 重载 seen-set。
- **TUI**：Bubbletea + Lipgloss 赛博朋克主题（黑底绿/琥珀/红）；非 TTY 自动
  回退纯文本。

完整架构文档：[`docs/ARCHITECTURE.zh-CN.md`](docs/ARCHITECTURE.zh-CN.md)。

### 输出格式

结果文件名携带本地时间 `HH-MM-SS` 启动戳（v0.5.1 新增），同日多次 run 不会互相
覆盖。目录按 `YYYY-MM-DD` 分桶；时间戳放在文件名上。示例：
`fgqm_result_14-30-22.txt`；`fgqm_creds.txt` 无戳——该文件以 `O_APPEND` 打开，
去重靠内存 `State`。

- `fgqm_result_HH-MM-SS.txt` — 人类可读行
- `fgqm_result_HH-MM-SS.json` — NDJSON（每行一个 JSON 对象）
- `fgqm_result_HH-MM-SS.csv` — RFC 4180，每条结果一行
- `fgqm_creds.txt` — 凭据命中（明文；操作员工作文件）
- `fgqm_rdp_HH-MM-SS.json` / `fgqm_rdp_HH-MM-SS.txt` — RDP 深度指纹（主机名、build、NLA 标志、OS）
- `fgqm_web_HH-MM-SS.json` / `fgqm_web_HH-MM-SS.txt` — 每个 webtitle 命中的结构化 Web 指纹：URL、状态码、标题、Server、命中指纹，以及 https 目标的 TLS 叶子证书身份（Subject、SAN、Issuer、有效期、协议版本）。SAN/CN 字段经常暴露 banner 匹配永远看不到的内网主机名与域名。
- `fgqm_alive_HH-MM-SS.txt` — 每行一个 IP（去重后的主机清单，可直接喂 `nmap -iL` / `masscan --targets` / `curl` 循环）。与其他带时间戳 sink（`fgqm_result_*`、`fgqm_rdp_*`）相同的日桶（`YYYY-MM-DD/`）+ `HH-MM-SS` 文件名戳。
- `fgqm_log_HH-MM-SS.txt` — 本次 run 的日志归档（与控制台流相同的 `[*]`/`[+]`/`[!]` 行，格式 `HH:MM:SS [level] message`）。与结果文件相同的日桶 + 时间戳；每次扫描自动写一份。纯文本模式（`--no-tui`）同时 tee 到控制台与文件；TUI 模式与 `--silent` 只写文件——屏幕保持干净，日志不再丢失。凭据命中行含明文密码，文件以 `0600` 创建（与 `fgqm_creds.txt` 同策略）。

通过 `-ot` / `-oj` / `-oc` 显式指定路径会同时绕过日桶与时间戳。

### 插件与凭据覆盖

完整插件名册——名称、默认端口、Identify/Credential 能力——直接从二进制的活体
registry 生成，并在 CI 中机器校验：

- [`docs/PLUGINS.md`](docs/PLUGINS.md) — 每个已注册插件的默认端口与能力矩阵
- [`docs/FLAGS.md`](docs/FLAGS.md) — 每个 CLI flag 的分组与默认值

凭据测试覆盖 `PLUGINS.md` 中每个标 ✅ 的服务（authenticator registry），全部在
无漏洞利用强制约束下运行（`fgqm_creds.txt` 是唯一副作用）。

IPv6 一等公民（单 IP / CIDR / 逗号列表）。自定义 Web 指纹规则集经
`--web-fingerprint <path>` 加载（FG-QiMen 原生 JSON 或 EHole 格式；与内置规则
合并）。RDP NLA 姿态（HYBRID / SSL / 传统）由 `rdp-nla` 插件检测；完整 CredSSP
认证暂缓。

---

## CLI 参考

```
fg-qimen [flags]                             # 隐式 scan
fg-qimen scan [target] [flags]               # 显式 scan；target 可为 CIDR/范围/主机
fg-qimen resume --project <name>             # 恢复项目
fg-qimen projects list                       # 列出项目
fg-qimen projects create <n>                 # 创建项目
fg-qimen projects delete <n>                 # 删除项目
fg-qimen projects info <n>                   # 项目详情
fg-qimen projects export <n> <out.fgq>       # 导出项目为单个 .fgq 文件
fg-qimen projects import <in.fgq> <n>        # 从 .fgq 文件导入
fg-qimen projects prune <n> --before <date>  # 删除早于 <date> 的 seen-hashes（--compact 回收磁盘）
fg-qimen schedules add <name> --cron "<expr>" # 在项目 DB 中持久化调度
fg-qimen schedules list                      # 查看已排队调度
fg-qimen schedules remove <name>             # 删除调度
fg-qimen version                             # 显示版本
fg-qimen completion bash                     # 生成 shell 补全
```

### 快速上手（6 个核心 flag）

约 90% 的扫描只需要这 6 个 flag：

| 短参 | 长参 | 示例 | 用途 |
|---|---|---|---|
| `-H` | `--host` | `-H 10.0.0.0/24` | 目标 IP / CIDR / 范围 / 逗号列表 |
| — | `--project` | `--project corp` | 命名项目（持久化到 bbolt；省略即即扫即走） |
| `-u` | `--user` | `-u root,admin` | 内联用户名（多个用逗号分隔） |
| `-p` | `--pass` | `-p admin,root` | 内联密码（多个用逗号分隔） |
| `-uf` | `--user-file` | `-uf users.txt` | 用户名字典文件（每行一个） |
| `-pf` | `--pass-file` | `-pf pass.txt` | 密码字典文件（每行一个） |

最常见的四种搭配：

```bash
-H 1.0.0.0/8 -u admin -p root,toor              # 主机 + 内联凭据
-H 1.0.0.0/8 -uf users.txt -pf passes.txt       # 主机 + 字典
-H 1.0.0.0/8 -f targets.txt -a                  # 主机文件 + 仅存活
-H 1.0.0.0/8 -ot r.txt -oj r.json -oc r.csv      # 三种输出 sink 全开
```

具体配方：

```bash
# 最小扫描：256 主机 /24 对默认端口
fg-qimen -H 10.0.0.0/24

# 命名项目 + 字典 + 小线程数
fg-qimen --project corp -H 10.0.0.0/24 -uf users.txt -pf pass.txt -t 50

# 对已保存项目先前见过的主机再次攻击
fg-qimen resume --project corp

# 纯爆破：跳过存活 + 端口扫描，直接试凭据
fg-qimen scan --project corp --mode crack -uf users.txt -pf pass.txt

# 走 HTTP 代理（所有插件的 dialer 链式生效）
fg-qimen -H 10.0.0.0/24 --proxy http://127.0.0.1:8080
```

> **短参约定**（v0.5.1 起）：全小写、助记符式，命名空间用 2 字母
> （output-* / user-pass-file）。`-H` 是唯一大写（避开 cobra 保留的
> `-h`/`--help` 冲突）。从 v0.5.0 迁移的对照表见
> [更新日志](CHANGELOG.zh-CN.md)。

### 完整 flag 参考

完整 flag 表在 [`docs/FLAGS.md`](docs/FLAGS.md)——它**从二进制实际使用的同一份
registry 生成**，因此永远不会与 `fg-qimen --help` 漂移（一旦漂移，CI 守卫测试
会让构建变红）。`fg-qimen --help` 仍是权威的终端渲染。

每种常见工作流的完整 CLI 用法模板在
[`docs/CONFIGURATION.zh-CN.md`](docs/CONFIGURATION.zh-CN.md)。

---

## 发布物校验

每个 GitHub Release 附带 **11 个标准平台二进制 + 2 个加固版**
（linux-amd64、windows-amd64——garble + UPX，刻意不可复现）以及每个二进制的
cosign 签名、签名证书与 CycloneDX SBOM：

| 文件 | 用途 |
|---|---|
| `fg-qimen-<platform>` | 编译的标准二进制（Linux/macOS/BSD 无 `.exe`） |
| `fg-qimen-<platform>-hardened` | 加固二进制（garble 混淆 + UPX 压缩；Windows 带 `.exe` 后缀） |
| `SHA256SUMS` | 每个二进制的 sha256 校验和（13 条目） |
| `*.sig` | cosign 无密钥签名（OIDC、Sigstore） |
| `*.pem` | 内嵌 OIDC 身份的签名证书 |
| `*.sbom.json` | 该二进制的 CycloneDX SBOM（每平台一份） |
| `FG-QiMen-release.spdx.json` | 覆盖全部发布产物的完整 SPDX SBOM |

### 1. 校验和

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

干净通过时每行打印 `<binary>: OK`；任何不匹配都会以非零退出码中止。

### 2. 签名（无密钥，OIDC）

发布管线使用 [cosign](https://github.com/sigstore/cosign) 的无密钥模式对接
Sigstore 公共 good 实例——仓库里没有任何秘密密钥。

```bash
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --signature fg-qimen-<platform>.sig \
  --certificate fg-qimen-<platform>.pem \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-<platform>
```

验证成功会打印被验证二进制的 SHA256 与签名者 OIDC 身份。证书绑定 GitHub
Actions 工作流身份
（`https://github.com/LCUstinian/FG-QiMen/.github/workflows/release.yml@refs/tags/<TAG>`）。

### 3. SBOM

CycloneDX SBOM 列出二进制链接的每个直接 + 传递依赖。可直接导入
[Dependency-Track](https://dependencytrack.org/) 或任何 SBOM 感知的 SCA 工具。

```bash
# 查看组件
jq '.components[] | {name, version, purl}' fg-qimen-<platform>.sbom.json
```

### 4. 源码可复现性（可选）

从对应 tag 逐字节重建二进制：

```bash
git checkout <TAG>
go build -trimpath -ldflags='-s -w -buildid=' -o fg-qimen-local .
sha256sum fg-qimen-local
```

哈希必须与 `SHA256SUMS` 对应行一致。

### 5. 上报差异

以上任一步失败，**不要运行该二进制**。在
<https://github.com/LCUstinian/FG-QiMen/issues> 提 issue，附失败步骤输出与你
尝试的 tag。

---

## 本地化

- **代码注释**：双语（中文 + 英文）覆盖所有公开函数、结构与关键逻辑块。
- **终端输出**：100% 英文（banner、help、日志、错误）。
- **README**：拆分——英文（[README.md](README.md)）+ 简体中文
  （[README.zh-CN.md](README.zh-CN.md)）。
- **CLI flag 名**：英文。
- **生成文档**（[FLAGS.md](docs/FLAGS.md)、[PLUGINS.md](docs/PLUGINS.md)）：
  英文，与终端输出政策一致——它们是 registry 的渲染物，不是散文。

## 优雅 Ctrl+C

- 第一次 **Ctrl+C**：`cancel()` 根 context → 管线排空 → 输出 flush →
  bbolt `Sync()` → 退出码 130。
- `--shutdown-timeout`（默认 5s）内的第二次 **Ctrl+C**：硬退出
  （`os.Exit(1)`）。

---

## Roadmap

进行中的工作看 [CHANGELOG.zh-CN.md](CHANGELOG.zh-CN.md) 的 `[Unreleased]`
小节。当前主题：自适应扫描打磨、UDP 服务覆盖扩展、供应链加固。

---

## 致谢

FG-QiMen 站在多个开源项目的肩膀上。所有复用代码均为 MIT 许可；逐文件的修改
历史见源码头注释。

**主要灵感来源**：[shadow1ng](https://github.com/shadow1ng) 的
[fscan](https://github.com/shadow1ng/fscan)（MIT）——管线解耦的扫描器架构、
服务 Identify + Credential 插件范式、Nmap 风格的端口指纹框架。FG-QiMen 继承
**无漏洞利用**政策，并删除了原项目携带的所有未授权访问 / 写入 / POC 路径。

完整的第三方许可文本汇编：
[`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md)。

FG-QiMen 源码以 MIT 许可发布。见 [LICENSE](LICENSE)。

---

## 免责声明

本工具**仅用于授权安全测试与学习**。未经许可请勿扫描任何目标。作者不对滥用
负责。
