# FG-QiMen

> **把扫描与识别做到极致**：足够全、足够深、足够快、足够稳。
>
> **功能多 ≠ 更好。** 纯扫描器 + 凭据测试器。无漏洞利用、无持久化、无认证后动作——设计使然。

FG-QiMen 是一个纯 CLI 扫描器，通过 Go channel 管线解耦**端口扫描器（生产者）**与
**插件 worker（消费者）**。支持三种运行模式（`scan` / `crack` / `linked`）与两种工作
模式（即扫即走 vs 带持久化 bbolt 状态的项目工作区）。

**名字即定位。** *FG* 取自墨家「非攻」——只识别、不攻击；*QiMen* 取自奇门遁甲
的「奇门」——先测算推演、后落子行动。

[English](README.md) · [Releases](https://github.com/LCUstinian/FG-QiMen/releases) · [更新日志](CHANGELOG.zh-CN.md)

```
┌ FG-QIMEN 0.9.0 ─ project: demo ─ mode: scan──────────────────────────────────────────────────────────────  SCANNING  ┐
│  [ ▶ IDENTIFY ]  ~30s  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▂▂▃▃▄▅▅▆▆▇█│
│  rate: 28.5 hits/s    ports: 142.0/s    probed 18 / 0                                                                │
├──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
├────────────────────────────────────────────────┬─────────────────────────────────────────────────────────────────────┤
│  PROGRESS                                      │  LIVE EVENTS                                                follow ▼│
│  alive    ░░░░░░░░░░░░░░░░░░░░ 18/0            │  14:23:00  [+]  10.0.0.1:22            ssh                          │
│  ports    ░░░░░░░░░░░░░░░░░░░░ 142/0           │  14:23:01  [-]  10.0.0.2:80            http                         │
│  done 142   inflight 0                         │  14:23:02  [+]  10.0.0.3:6379          redis                        │
│  deferred 0   stall 0s                         │  14:23:03  [*]  10.0.0.4:443           https                        │
│                                                │  14:23:04  [~]  10.0.0.5:445           smb                          │
│  TOP PLUGINS                                   │  14:23:05  [-]  10.0.0.6:3306          mysql                        │
│  9          ██████░░░░░░  http-title           │  14:23:06  [+]  10.0.0.7:22            ssh                          │
│  6          ██████░░░░░░  ssh-banner           │  14:23:07  [-]  10.0.0.8:80            http                         │
│  3          ██████░░░░░░  redis                │  14:23:08  [+]  10.0.0.9:6379          redis                        │
│                                                │  14:23:09  [*]  10.0.0.10:443          https                        │
├────────────────────────────────────────────────┴─────────────────────────────────────────────────────────────────────┤
│  ERRORS: (none)                                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│[q] quit  [p] pause  e errors panel  L live overlay  ? toggle help                                                    │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## ✨ 亮点

- ✅ **零利用，设计使然** —— 只扫描、识别、验证凭据；不做利用、不留持久化。攻击流量是最吵的流量——这台扫描器不制造噪音。
- ✅ **先测量，后扫描** —— RTT/丢包画像驱动 mean+4σ 自适应超时与 AIMD 拥塞控制线程池：健康内网快 ≈5×，丢包广域网自动退避。
- ✅ **识别，而不只是连通** —— 每个命中带结构化 product/version/confidence；Web 命中附 title/server + TLS SAN/CN 身份；RDP 附 build/NLA/OS 姿态。
- ✅ **44 插件 · 3,333 条 Web 指纹规则 · 84 条 UDP 探测** —— nmap 级服务识别（自定义规则兼容 EHole），覆盖数据库、远程访问、邮件、文件存储、云。
- ✅ **凭据验证矩阵** —— 44 个插件中 27 个支持凭据验证（SSH、SMB、数据库、邮件、消息队列、云），判定显式分类；默认脱敏（`--show-creds` 显式开启）。
- ✅ **识别质量是被测量的** —— 合成指纹语料库每次 CI 回归精度，垃圾字节守卫对抗性 banner：已知误报保持为零。
- ✅ **v3 lattice TUI** —— 单层 lattice 网格、角色令牌色板、亚字符进度条、抗风暴活体事件流；四级降级 truecolor → 256 色 → 灰度 → 纯 ASCII（`--tui-ascii`）。
- ✅ **逐字节钉住的渲染** —— 27 个 golden 帧（三断点 × 八状态 + overlay + ASCII 变体）在 CI 逐字节对钉：你看到的 UI 就是发布里的 UI。
- ✅ **项目工作区** —— 每项目独立 bbolt 状态、`--project-key` 静态加密：暂停续扫、seen 哈希修剪、导出导入。
- ✅ **内置调度器** —— 一次性 `--at` / `--in`、任意 IANA 时区的周期 `--cron`、常驻 `--daemon` 循环；`--schedule-dry-run` 先验证触发时间再真跑。配合项目状态实现无人值守的周期扫描。
- ✅ **证据优先的输出** —— NDJSON / CSV / SARIF / TXT 多槽按日分桶、按大小/数量轮转；凭据在控制台与结果槽默认脱敏（`--show-creds` 显式开启）。
- ✅ **只读枚举** —— SMB 共享与 FTP 目录走空会话/匿名；仅采元数据，绝不下载文件内容。
- ✅ **扫描器自身的安全卫生** —— 1 MiB HTTP body 上限、CSV 公式注入中和（OWASP）、TLS 与 SSH 主机密钥校验默认开启：安全工具对自己执行它要求别家的标准。
- ✅ **IPv6 一等公民 & 范围感知** —— 单 IP / CIDR / 列表目标；协议交互浮出的域外主机带时间与来源全程记录（可选有界补扫）。
- ✅ **供应链加固** —— cosign 无密钥签名、CycloneDX + SPDX 双 SBOM、SLSA L2 溯源、CI 动作 SHA 钉死。
- ✅ **文档不会漂移** —— flag 与插件表由代码生成、CI 漂移检查，中英双语结构镜像。
- ✅ **11 平台发布矩阵** —— 一条命令构建全部目标；`just` 配方从开发贯穿到发布。

---

## 纯扫描器定位

扫描器 + 凭据测试器，用于授权场景。FG-QiMen 止步于扫描、识别与凭据
验证——无漏洞利用、无认证后动作、无持久化。这是战术选择，不是功能
缺失：

- **攻击流量是最显眼的流量。** 在有防护的内网，最容易触发告警的恰恰
  是未授权访问尝试与 POC 投放；纯扫描可以在防守方察觉之前安静跑完。
  噪声预算留给下一步那一次有针对性的动作。
- **攻击可能把目标打崩。** 为某个版本编写的 POC 打在另一个版本或
  配置上，可能直接打挂服务、甚至打崩主机——侦察瞬间变成生产事故。
  扫描是只读操作，这类风险从一开始就不存在。
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

这三项承诺，就是开头那句四字诀的工程化落地：覆盖**够全**、身份**够深**、
引擎自调速所以**够快**、坏网络下不崩所以**够稳**。

<!-- gendocs:stats begin -->
**44 个服务插件 · 27 个支持凭据测试 · 3333 条内置 Web 指纹规则 · 84 条 UDP 探测载荷**
<!-- gendocs:stats end -->

### 正面对比

| 维度 | 固定参数扫描器 | FG-QiMen |
|---|---|---|
| **超时** | 单一静态单次探测值——filtered 端口每台主机都白等一遍 | 64 样本 RTT 环的 mean+4σ<br>快速局域网 3s → ~600ms（**≈5× 提速**），慢路径保留操作员上限 |
| **并发** | 固定线程数；一个拥塞网段拖垮整轮扫描 | AIMD 线程池——慢启动、健康时加性增长、拥塞/RTT 信号触发乘性回退<br>`--threads` 始终是硬上限 |
| **死目标浪费** | 每个网段每台主机都探 | 两阶段 /24 网关预筛 + 主机排除（CIDR、范围、`192`/`172`/`10` RFC1918 快捷方式）<br>死网段零流量 |
| **服务覆盖** | 仅 TCP | 可选 UDP 探测（nmap payload 库），与 TCP 同构的结构化身份（`--udp`、`--udp-strict`） |
| **身份深度** | "端口开放" + 原始 banner | 结构化 product/version/confidence<br>Web：状态码/标题/Server/指纹 + TLS SAN/CN<br>RDP：build/NLA/OS |
| **状态与恢复** | 一次性运行；Ctrl+C 意味着整轮重来 | bbolt 项目工作区：resume、prune、export/import、cron 调度 |
| **操作员体验** | 日志行刷屏滚过 | 实时 TUI（阶段 ETA、命中流、插件榜、错误分类）或干净纯文本<br>txt/json/csv sink + 日桶 |
| **供应链** | 裸二进制 | cosign 签名、双 SBOM、SLSA L2 来源证明、CI action 锁定 SHA<br>先验证再运行 |

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

stdout 为 TTY 时 TUI **默认开启**。`--no-tui` 强制纯文本输出；`--tui-ascii`
强制纯 ASCII 字形集（针对点阵控制台字体、渲染框线会花屏的 SSH 客户端）。
非 TTY stdout（CI、管道）始终走文本日志。

仪表盘是一个**单层 lattice 网格**，共享边框一次绘制，按 3 断点响应式
布局（窄 <80 / 中 80–119 / 宽 ≥120 列）。宽屏左列 PROGRESS 在上、
TOP PLUGINS 在下，右列 LIVE EVENTS 占满，ERRORS 通栏在底部；更窄的
屏幕全部堆叠。计数为零的格子整格消失。活跃阶段以 zone accent 点亮
其区域边框——唯一被认可的边框换色。

- **头部带**：阶段徽标 + ETA + 60 样本命中率 sparkline；速率行
  （`rate: 28.5 hits/s  ports: 142.0/s  probed 18 / 0`）随 alive 扫描
  实时跳动。
- **PROGRESS**：分阶段进度条 + 实时账本（done / in-flight / deferred）+
  停滞读数（池子静默 ≥15s 显示 `stall 15s ▲`，超过 1 分钟 `!!`）。
- **TOP PLUGINS**：本轮命中最多的插件，条形图渲染。
- **LIVE EVENTS**：固定环形缓冲的最近 64 条事件，定宽严重度令牌（`[+]`
  命中、`[*]` 凭据、`[!]` critical、`[~]` 警告、`[-]` miss）；follow/browse
  双滚动（`↑↓` 进入浏览，`End` 回到跟随）、同源折叠（`×N`）、超 500 ev/s
  自动切换风暴摘要、critical 侧车在风暴中保持关键行可见。跨零点的运行
  自动插入日期分隔行；窄终端 `L` 叠层显示最近 5 条。
- **ERRORS**：紧凑的分类计数行；`e` 展开为 top 分类条形，`E` 收回。分类
  来自 `core.ClassifyError`（先 errors.Is / errors.As，退化为子串匹配）。
- **Footer**：按键提示；`?` 打开完整帮助叠层。

渲染由 27 个 golden 帧（三断点 × 八状态 + overlay + ASCII 变体）在 CI
逐字节对钉，并按四级阶梯降级——truecolor → 256 色 → 灰度 → 纯 ASCII——
同一布局在任何终端都保持可读。

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
- **TUI**：Bubbletea + Lipgloss v3 lattice——角色令牌色板、单一符号表、
  四级降级（truecolor → 256 色 → 灰度 → `--tui-ascii`）；非 TTY 自动回退纯文本。

完整架构文档：[`docs/ARCHITECTURE.zh-CN.md`](docs/ARCHITECTURE.zh-CN.md)。

### 输出格式

结果文件名携带本地时间 `HH-MM-SS` 启动戳（v0.5.1 新增），同日多次 run 不会互相
覆盖，并落在按 `YYYY-MM-DD` 分桶的目录下（示例：`fgqm_result_14-30-22.txt`）。
唯一例外：`fgqm_creds.txt` 无戳——该文件以 `O_APPEND` 打开，去重靠内存 `State`。

| Sink | 内容 |
|---|---|
| `fgqm_result_<time>.txt` / `.ndjson` / `.csv` | 扫描结果——人类可读行 / NDJSON（每行一个 JSON 对象）/ RFC 4180 CSV |
| `fgqm_creds.txt` | 凭据命中（明文；操作员工作文件） |
| `fgqm_rdp_<time>.ndjson` / `.txt` | RDP 深度指纹：主机名、build、NLA 标志、OS |
| `fgqm_web_<time>.ndjson` / `.txt` | 每个 webtitle 命中的结构化 Web 指纹：URL、状态码、标题、Server、命中指纹——https 目标追加 TLS 叶子证书身份（Subject、SAN、Issuer、有效期、协议版本）；SAN/CN 字段经常暴露 banner 匹配永远看不到的内网主机名与域名 |
| `fgqm_alive_<time>.txt` | 每行一个 IP——去重后的主机清单，可直接喂 `nmap -iL` / `masscan --targets` / `curl` 循环 |
| `fgqm_log_<time>.txt` | 本次 run 的日志归档（`HH:MM:SS [level] message`，与控制台流相同的行）；每次扫描自动写一份——纯文本模式（`--no-tui`）同时 tee 到控制台与文件，TUI 模式与 `--silent` 只写文件；凭据命中行含明文密码，文件以 `0600` 创建 |
| `fgqm_discovery_<time>.ndjson` / `.txt` | 协议交互（NBNS、SMB、TLS SAN……）发现的范围外主机：地址、主机名、来源协议、发现时间；每次 run 都记录，要不要扫由 `--expand-scope` 显式开启 |
| `fgqm_shares_<time>.ndjson` / `.txt` | SMB 匿名会话共享枚举（`--share-enum`）：共享名、访问级别、目录元数据——仅取证，绝不下载文件内容 |
| `fgqm_ftp_<time>.ndjson` / `.txt` | FTP 目录遍历（`--ftp-enum`）：匿名或弱口令登录，目录树元数据——刻意与共享发现分开落盘 |
| `fgqm_servers_<time>.ndjson` / `.txt` | 重要服务器聚合清单（域控 / 文件 / 备份 / VPN / 数据库……）：IP、主机名、角色、证据端口 |

说明：

- `<time>` 即 `HH-MM-SS` 启动戳；扩展名 `.ndjson` 是刻意的——按单文档校验
  整个 `.json` 文件的编辑器会把多行输出标为非法。
- 通过 `-ot` / `-oj` / `-oc` 显式指定路径会同时绕过日桶与时间戳。

### 证据、范围与服务器清单

三项 v0.9 能力把「交付证据」的承诺延伸到识别之外：

- **范围外发现**——协议交互触及的陌生主机一律记录时间 + 来源。
  `--expand-scope auto` 对新主机加扫一轮有界扩展（仅 RFC1918 私网、
  与已扫目标同 /24、`--exclude-hosts` 仍生效、上限 256 台）。默认
  `off` = 仅记录 + 重跑提示；扫描器绝不被交互式询问阻塞。
- **只读共享/FTP 枚举**——SMB 匿名会话共享列表与匿名/弱口令 FTP
  目录遍历，深度与条目受限，只取元数据：不下载文件内容、不写入、
  不做认证后动作。共享与 FTP 发现分开落盘，永不合并。
- **重要服务器清单**——命中基础设施角色（域控、文件服务器、备份、
  VPN、数据库……）的主机经 NBNS 主机名充实后聚合进 `fgqm_servers.*`，
  并在 NDJSON 流中带 `important` 标记。

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
fg-qimen [flags]                                # 隐式 scan
fg-qimen scan [target] [flags]                  # 显式 scan；target 可为 CIDR/范围/主机
fg-qimen resume --project <name>                # 恢复项目
fg-qimen projects list                          # 列出项目
fg-qimen projects create <n>                    # 创建项目
fg-qimen projects delete <n>                    # 删除项目
fg-qimen projects info <n>                      # 项目详情
fg-qimen projects export <n> <out.fgq>          # 导出项目为单个 .fgq 文件
fg-qimen projects import <in.fgq> <n>           # 从 .fgq 文件导入
fg-qimen projects prune <n> --before <date>     # 删除早于 <date> 的 seen-hashes（--compact 回收磁盘）
fg-qimen schedules add <name> --cron "<expr>"   # 在项目 DB 中持久化调度
fg-qimen schedules list                         # 查看已排队调度
fg-qimen schedules remove <name>                # 删除调度
fg-qimen version                                # 显示版本
fg-qimen completion bash                        # 生成 shell 补全
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
-H 1.0.0.0/8 -ot r.txt -oj r.ndjson -oc r.csv    # 三种输出 sink 全开
```

具体配方：

```bash
# 最小扫描：256 主机 /24 对默认端口
fg-qimen -H 10.0.0.0/24

# 命名项目 + 字典 + 小线程数
fg-qimen --project corp -H 10.0.0.0/24 -uf users.txt -pf pass.txt -t 50

# 对已保存项目先前见过的主机重新扫描
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
  由 registry 生成的英文表格，附简体中文镜像
  （[FLAGS.zh-CN.md](docs/FLAGS.zh-CN.md)、[PLUGINS.zh-CN.md](docs/PLUGINS.zh-CN.md)）。

---

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
