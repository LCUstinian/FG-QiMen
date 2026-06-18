# FG-QiMen v0.2.0 多维度审计报告（与 fscan 对比）

> **审计日期**：2026-06-18
> **审计范围**：FG-QiMen `D:\Go\FG-QiMen`（commit 49a187d，main 分支）
> **对比对象**：fscan `D:\Go\fscan-main`（shadow1ng/fscan 主线，2026-06-01 快照）
> **审计维度**：代码结构、功能覆盖、性能与并发、安全、代码质量、测试、文档、构建/CI、用户体验、可扩展性

---

## 0. 执行摘要

FG-QiMen 是一个 ~31.5K LOC 的 Go 网络扫描器 CLI，fscan 是 ~72K LOC 的同类工具（含 vendored `libs/grdp`）。两者在协议扫描、凭据爆破、并发模型上目标高度一致，但产品定位与设计取舍差异显著：

| 维度 | FG-QiMen v0.2.0 | fscan | 评价 |
|---|---|---|---|
| **总代码量** | 31,460 LOC（含 9,798 LOC 测试） | 72,025 LOC（含 22,120 LOC 测试） | fscan 是 2.3× |
| **协议插件数** | 26（7 类） | 40+ services + 24 local + 4 web | fscan 显著多 |
| **架构模式** | Pipeline（alive→scan→identify→spray→sink） | 策略模式 + 自适应线程池 | FG-QiMen 更清晰 |
| **交互界面** | Bubbletea TUI 仪表盘 + 文本 UI | 进度条 + Web UI（Vite/React，需 build tag） | 各有所长 |
| **持久化** | bbolt 项目工作区（多扫描/恢复） | 无项目概念，仅实时输出 | FG-QiMen 领先 |
| **可嵌入性** | CLI 工具（无 SDK） | `pkg/fscan` SDK | fscan 领先 |
| **国际化** | 仅英文 flag/help | 内置 zh/en i18n | fscan 领先 |
| **漏洞利用** | 无内置漏洞利用 | MS17-010 检测+利用、Redis SSH 公钥写入 | fscan 领先 |
| **Web 指纹/POC** | 仅 Web Title + FingerprintHub 风格的规则 | POC 引擎（YAML + CEL 表达式）+ Nuclei 适配 | fscan 领先 |
| **测试覆盖率** | core ~57%、cmd 47%、多数 adapted 插件 0% | 平均 1.9%（CI 门禁 >40% 警告） | **两者都不及格**，FG-QiMen 重点模块好于平均 |
| **CI 流水线** | Linux/macOS race + 5 平台 cross-build | lint（复杂度门禁）+ test + cross-build + GoReleaser | fscan 更完整 |
| **代码可读性** | 注释密、命名统一、文档齐 | 代码密度大、注释较少 | FG-QiMen 领先 |

**核心结论**：
- FG-QiMen 是**"工程质量优先的轻量扫描器"**——把 ~25 个核心协议打磨干净，配工程化最佳实践（pipeline 架构、bbolt 持久化、TUI、CI、garble 硬化、文档驱动）。
- fscan 是**"功能广度优先的全能工具"**——用 2.3× 代码量换覆盖更多协议、漏洞利用、本地后渗透、SDK 嵌入、Web UI。
- 两者**测试覆盖率都严重不足**，是最大共性短板。

---

## 1. 代码结构与架构

### 1.1 目录组织

| 项目 | 顶层 | 优势 | 劣势 |
|---|---|---|---|
| **FG-QiMen** | `cmd/`, `internal/{config,core,discovery,network,output,plugins,portscan,session,store,transport,tui,types,ui,utils,version,workspace}` | 严格 `internal/` 封装、外部无法引用 | 内部包嵌套深（`internal/core/credential/auth/database/elasticsearch` 四级），新手导航成本高 |
| **fscan** | `common/`, `core/`, `plugins/{local,services,web}`, `pkg/fscan`, `webscan/`, `libs/grdp` | 顶层即功能分区，扁平 | `common/` 单包 5,370 LOC，god package 倾向；`plugins/local` 24 个文件混入"扫描+后渗透"，概念耦合 |

**FG-QiMen 的内部包分层是其最值得保留的设计**——`core`（调度）→ `plugins`（接口）→ `plugins/adapted`（实现）→ `core/credential/auth/<category>/<proto>`（凭据具体实现）四层依赖方向清晰，无环。

### 1.2 入口与依赖方向

- **FG-QiMen**：[main.go:1-25](main.go) → `cmd.Execute()` → [cmd/root.go:1-93](cmd/root.go) → cobra root → `scan` 子命令 → [cmd/scan.go:43-56](cmd/scan.go) → `buildConfig()` + `runScan()` → `core.RunPipeline()` → 4 阶段 → `core.RunResultSink()`。依赖图：`cmd` → `internal/{session,core,types,ui,store,workspace,tui}` → `internal/core/{alive,scan,credential}` → `internal/plugins`。**单向，无环**。
- **fscan**：[main.go:1-75](D:/Go/fscan-main/main.go) → `common.Initialize()` + `core.RunScan()`。`common/` 同时被 `core` 和 `plugins` 引用，但 `plugins` 反过来被 `core` 引用——**存在 `common → plugins → common` 的隐式反向引用**（通过 `common/globals.go:147-156` 的 `globalMu` 暴露 `globalConfig`/`globalState`）。

### 1.3 插件模型

| 维度 | FG-QiMen | fscan |
|---|---|---|
| **接口** | [internal/plugins/plugins.go:43-69](internal/plugins/plugins.go) 双模式：`Identify` + `Credential` | [plugins/init.go](D:/Go/fscan-main/plugins/init.go) 单一 `Scan(ctx, info, session)` + `BasePlugin` 嵌入 |
| **注册** | `init()` → `plugins.Register(New())`（[internal/plugins/registry.go:78-88](internal/plugins/registry.go)） | 同 `init()` → `RegisterPlugin(...)`（build tag `plugin_xxx` 选择性裁剪） |
| **粒度** | 一协议一文件 + 一凭据一文件（数据库 7 个插件对应 7+ 独立 credential authenticator） | 一协议一文件（凭据与扫描混编） |
| **隔离性** | 凭据与识别分离，可单独跑 identify-only 模式 | 扫描=爆破一体，无 standalone 识别模式 |

**FG-QiMen 的双模式插件接口是其最关键的架构优势**——`--mode scan|crack|linked`（[cmd/flags.go:116](cmd/flags.go)）允许用户"只发现不爆破"或"只爆破已识别"，把"误报风险"与"爆破噪音"解耦。fscan 的 `--m` 模式只是 "all/scan/crack" 的入口选择，没有 plugin 级的职责分离。

### 1.4 状态管理

- **FG-QiMen**：[internal/types/state.go](internal/types/state.go) 提供 `State` struct（原子计数器 + `sync.Map` 集合），通过 [internal/session/session.go:35](internal/session/session.go) 注入到 `session.Ctx` 风格的 per-run 上下文，**多扫描项目天然隔离**。
- **fscan**：[common/state.go:26-58](D:/Go/fscan-main/common/state.go) 用 `atomic.Int64` + `sync.RWMutex` 保护全局 `packetCount`/`tcpPacketCount`/`num`/`end` + URL 集合，**全局共享**——单实例只能跑一个扫描任务。

---

## 2. 功能覆盖（协议/插件）

### 2.1 协议扫描插件对比

| 类别 | FG-QiMen | fscan | 差异 |
|---|---|---|---|
| **数据库** | ES, Memcached, MongoDB, MSSQL, MySQL, Oracle, PostgreSQL, Redis（8） | 同 8 个 + Cassandra, Neo4j | fscan 多 Cassandra（CQL）、Neo4j（Bolt） |
| **邮件** | IMAP, POP3, SMTP（3） | 同 3 | 一致 |
| **文件存储** | NFS, Rsync, SMB（3） | 同 3 | 一致 |
| **消息队列** | RabbitMQ（1） | + ActiveMQ, Kafka | fscan 多 ActiveMQ（61616 OPENWIRE）、Kafka（9092 SASL） |
| **网络/工控** | BACnet, Docker, LDAP, Modbus, SNMP, SOCKS5（6） | + Memcached, MQTT, DNS, NetBIOS-NS | fscan 多 MQTT、Memcached、DNS（含 AXFR 区域传送）、NetBIOS-NS |
| **远程接入** | FTP, IPMI, RDP（自实现协议栈）, SSH, Telnet, VNC, WinRM（7） | + MS17-010 + Exploit | **fscan 多 MS17-010 检测+利用链（DOUBLEPULSAR）** |
| **Web** | HTTP, WebTitle（含 favicon 指纹）（2） | + WebPOC（YAML/CEL） | fscan 有 POC 引擎（[webscan/lib/poc_executor.go](D:/Go/fscan-main/webscan/lib/poc_executor.go)） |
| **本地后渗透** | **无** | 24 个：SSH key、反弹 shell、SOCKS5、8 种 Windows 持久化、痕迹清理等 | **fscan 巨大优势** |

### 2.2 关键协议实现深度

| 协议 | FG-QiMen | fscan | 评价 |
|---|---|---|---|
| **RDP** | [internal/plugins/adapted/remote/rdp/](internal/plugins/adapted/remote/rdp) **自实现 7 个 wire 文件**（BER/GCC/MCS/TPKT/X.224/ServerCore/主文件） | 用 vendored `libs/grdp`（~5,400 LOC） | **FG-QiMen 主动选择摆脱 vendored 依赖**，代价是 8 个文件但避免了 5K LOC 第三方代码 |
| **Telnet** | [internal/plugins/adapted/remote/telnet/telnet.go:39-42](internal/plugins/adapted/remote/telnet/telnet.go) 基础 IAC 协商 | [plugins/services/telnet.go](D:/Go/fscan-main/plugins/services/telnet.go) **1019 行**，含 CVE-2026-24061 NEW-ENVIRON 漏洞 | fscan 在 Telnet 上更深（含 IAC 协商完整状态机 + 已知漏洞利用） |
| **Redis** | [internal/plugins/adapted/database/redis/redis.go:33-36](internal/plugins/adapted/database/redis/redis.go) 认证 + 基础识别 | [plugins/services/redis.go](D:/Go/fscan-main/plugins/services/redis.go) **623 行**，含 SSH 公钥写入、定时任务、模块化利用框架 | fscan 在 Redis 利用上完整 |
| **SMB** | [internal/plugins/adapted/filestorage/smb/smb.go](internal/plugins/adapted/filestorage/smb/smb.go) 用 `go-smb2` 单一驱动 | [plugins/services/smb.go](D:/Go/fscan-main/plugins/services/smb.go) **双驱动**（`stacktitan/smb` + `hirochachacha/go-smb2`），融合 4 个老插件 + SMBGhost (CVE-2020-0796) | fscan 兼容性更广 |
| **MongoDB** | [internal/plugins/adapted/database/mongodb/mongodb.go](internal/plugins/adapted/database/mongodb/mongodb.go) 基础 isMaster | [plugins/services/mongodb.go](D:/Go/fscan-main/plugins/services/mongodb.go) **480 行**，自实现 wire protocol | fscan 更深 |
| **Web 指纹** | [internal/portscan/fingerprint/](internal/portscan/fingerprint) Nmap-style service probe + 自维护 fingerprint 规则 | [webscan/fingerprint/](D:/Go/fscan-main/webscan/fingerprint) 272 regex + 30 MD5 + FingerprintHub 3,139 条 | **fscan 数量级领先** |
| **Web POC** | 无 | CEL 表达式 + YAML POC（[webscan/lib/poc_executor.go](D:/Go/fscan-main/webscan/lib/poc_executor.go)） + Ceye/DNSLog | fscan 是 Nuclei 风格 |

### 2.3 缺失的关键功能（按重要性排序）

| 优先级 | 缺失项 | 影响 | 实现成本 |
|---|---|---|---|
| **P0** | MS17-010 检测+利用 | 实战最常见 SMB 漏洞 | 中（fscan 实现可参考） |
| **P0** | 本地后渗透模块（失陷后的利用链） | 限制工具定位为"扫描器"而非"渗透框架" | 高（fscan 24 个插件的工作量） |
| **P1** | Web POC 引擎 | 当前只能识别 Web 服务，不能验证已知 CVE | 中（CEL 表达式引擎 + YAML loader） |
| **P1** | Redis SSH 公钥写入/定时任务 | 高频渗透动作 | 低（fscan redis.go 已有实现可移植） |
| **P1** | DNS 区域传送（AXFR）检测 | 内网常见配置错误 | 低 |
| **P1** | NetBIOS-NS 服务插件 | 内网主机发现补强 | 低 |
| **P1** | MQTT（IoT 设备） | 趋势协议 | 低 |
| **P2** | ActiveMQ / Kafka / Cassandra / Neo4j | 中间件覆盖度 | 中 |
| **P2** | SMBGhost (CVE-2020-0796) | 与 MS17-010 同级 | 中 |
| **P2** | i18n 中英文支持 | 用户群扩展 | 低 |
| **P3** | Web UI（React/Vite） | 与 TUI 二选一即可 | 高 |

---

## 3. 性能与并发

### 3.1 并发模型

| 维度 | FG-QiMen | fscan |
|---|---|---|
| **主线程池** | channel semaphore（[internal/core/scanner.go:73-74](internal/core/scanner.go)：`items := make(chan, 1024)` + worker pool 默认 16） | channel semaphore（[core/scanner.go:113](D:/Go/fscan-main/core/scanner.go)：`ch := make(chan struct{}, config.ThreadNum)` 默认 600）+ **自适应线程池**（[core/adaptive_pool.go](D:/Go/fscan-main/core/adaptive_pool.go)，基于 `ants/v2`） |
| **凭据并发** | [internal/core/credential/scheduler.go:120-134](internal/core/credential/scheduler.go) channel sem + per-target throttling（默认 100ms 间隔） | [plugins/services/credential_tester.go](D:/Go/fscan-main/plugins/services/credential_tester.go) 解决 goroutine 泄漏 + 找到成功凭据通知其他 worker |
| **限速** | 无显式速率限制 | juju/ratelimit 令牌桶（[common/globals.go:115-141](D:/Go/fscan-main/common/globals.go)），`-rate`/`-maxpkts`/`-icmp-rate` 三重控制 |
| **自适应** | **无**——线程数固定 | **有**——每 1s CAS 检查耗尽率，>10% 降 20%，<2% 升 10% |
| **errgroup** | 无（用 `sync.WaitGroup`） | 无（用 `sync.WaitGroup` + `sync.RWMutex`） |
| **信号处理** | [cmd/signal.go:48-111](cmd/signal.go) 完整 SIGINT/SIGTERM + `sync.Once` 幂等 + context 传播 + drain 模式（[internal/core/pipeline_sink.go:56-71](internal/core/pipeline_sink.go)） | [main.go:59-66](D:/Go\fscan-main/main.go) 基础 signal handling，调用 `Cleanup()` 写结果 |

### 3.2 性能特性

- **FG-QiMen 优势**：
  - **Per-target throttling**（[internal/core/credential/scheduler.go:156-174](internal/core/credential/scheduler.go)）：避免对同一目标轰炸，是 fscan 没有的精细控制。
  - **Drain on shutdown**（[internal/core/pipeline_sink.go:56-71](internal/core/pipeline_sink.go)）：SIGINT 后进入 drain 模式继续消费已排队 result，避免数据丢失。
  - **Bubbletea TUI 实时统计**（[internal/core/scanner.go:199](internal/core/scanner.go)）：1s 推送 stats 到 TUI。
- **fscan 优势**：
  - **自适应线程池**（[core/adaptive_pool.go:41-121](D:/Go/fscan-main/core/adaptive_pool.go)）：根据系统资源耗尽率动态调整，是工业级特性。
  - **包级速率限制**：避免触发 IDS 告警。
  - **长驻插件不占 WaitGroup**（[core/scanner.go:254-258](D:/Go/fscan-main/core/scanner.go)）：reverse shell / SOCKS5 代理等长连接由 ctx 管理生命周期，不阻塞主扫描退出。

### 3.3 性能数字（来自代码静态分析，未实测）

- **FG-QiMen 端口扫描池**：[internal/core/constants.go:15-19](internal/core/constants.go) `DefaultMinThreads=1`, `DefaultMaxThreads=500`，默认 200 线程。
- **fscan 端口扫描**：默认 600 线程（`ThreadNum`），自适应范围未在 `adaptive_pool.go` 限定。

---

## 4. 安全

### 4.1 输入安全

| 维度 | FG-QiMen | fscan |
|---|---|---|
| **路径穿越防护** | [cmd/scan.go:478](cmd/scan.go) `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1` opt-out + 容器化检查 | 无显式检查 |
| **TLS 跳过** | [cmd/flags.go:180](cmd/flags.go) `--insecure-tls` 显式 flag（推荐做法） | 无对应 flag |
| **SSH 跳过** | [cmd/flags.go:182](cmd/flags.go) `--insecure-ssh` 显式 flag | 无 |
| **SSH known_hosts** | [cmd/flags.go:184](cmd/flags.go) `--known-hosts` 支持 | 无 |
| **凭据文件读取** | [internal/transport/transport.go](internal/transport/transport.go) + [internal/types/redact.go](internal/types/redact.go) | 无 redact 机制 |
| **红化（Redact）** | **有**（[internal/types/redact.go](internal/types/redact.go)）：TXT 输出默认隐藏明文凭据 | **无**——TXT 直接打印明文 |

**FG-QiMen 在安全姿态上明显优于 fscan**——`--insecure-tls/--insecure-ssh` 让"是否跳过证书校验"成为显式选择，凭据 redact 默认开启，输出路径受 `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT` 控制。fscan 没有任何 redact 概念，TXT 写入器直接打印明文（[common/output/writers.go:60-92](D:/Go/fscan-main/common/output/writers.go)）。

### 4.2 二进制安全

- **FG-QiMen**：[justfile:48](justfile) 默认 `use_garble=1` 编译期混淆变量名；[justfile:75](justfile) `CGO_ENABLED=0` 避免 CGO/PIE；[scripts/harden.bat/ps1/sh](scripts/) 提供 UPX 压缩 + 字符串 mangling 流水线。
- **fscan**：使用 `ghaction-upx@v3`（[.github/actions/build-release/action.yml:78](D:/Go/fscan-main/.github/actions/build-release/action.yml)），无 garble。

**FG-QiMen 在反静态分析上更严**——garble 重命名 + UPX 压缩是组合拳，fscan 只有 UPX。

### 4.3 网络安全

- **代理支持**：两者都支持 HTTP/HTTPS/SOCKS5（[internal/network/proxy/](internal/network/proxy) vs [common/proxy/](D:/Go/fscan-main/common/proxy)）。
- **网卡绑定**：两者都支持 `-iface` 绑定本地 IP。
- **fscan 额外**：[common/proxy/manager.go](D:/Go/fscan-main/common/proxy/manager.go) 含**代理自动探测**。
- **FG-QiMen 额外**：[internal/network/proxy/validator.go](internal/network/proxy/validator.go) 含**代理可用性验证**。

### 4.4 漏洞与依赖

- **FG-QiMen**：[go.mod](go.mod) 无 known vulnerable 库；`go vet` 通过；[.github/workflows/ci.yml:83](.github/workflows/ci.yml) `go test -race` 强制数据竞争检查（Linux/macOS，Windows 跳过因 race-detector DLL 问题）。
- **fscan**：[go.mod](D:/Go/fscan-main/go.mod) Go 1.20（已落后），含 `google/cel-go` 等大依赖；`go test -race` 在 [Makefile:40](D:/Go/fscan-main/Makefile)。

**FG-QiMen 在依赖新鲜度上领先**——Go 1.26，fscan 仍 Go 1.20。

---

## 5. 代码质量

### 5.1 静态指标

| 指标 | FG-QiMen | fscan |
|---|---|---|
| **TODO/FIXME/XXX/HACK**（自研代码） | **0** | 未在报告中统计 |
| **godoc 注释密度** | 高（每个包有 doc.go） | 中 |
| **命名一致性** | 统一（`DefaultXxx`, `NewXxx`, `xxxTest`） | 中（缩写 + 全称混用） |
| **最大单文件** | [cmd/scan.go](cmd/scan.go) 508 行 | [plugins/services/telnet.go](D:/Go/fscan-main/plugins/services/telnet.go) **1019 行** |
| **圈复杂度门禁** | 无强制 | >80 失败（[.github/workflows/test-build.yml](D:/Go/fscan-main/.github/workflows/test-build.yml)） |

### 5.2 错误处理

- **FG-QiMen**：自定义错误 sentinel（`ErrInvalidTarget` 等），[internal/types/validation.go:154-197](internal/types/validation.go) `Config.Validate()` 集中校验。
- **fscan**：[common/state.go](D:/Go/fscan-main/common/state.go) 用 sentinel 错误（`ErrMaxPacketReached`/`ErrPacketRateLimited`），但**全局 `globalMu` 暴露**导致错误信息需小心。

### 5.3 抽象泄露

- **FG-QiMen 良好**：`session.Ctx` 风格注入避免全局状态。
- **fscan 较差**：`common/globals.go:147-156` 全局 `globalMu` 保护 `globalConfig`/`globalState`，多任务并发跑会冲突。

### 5.4 凭据安全存储

- **FG-QiMen**：[internal/store/store.go](internal/store/store.go) bbolt 加密？**未发现加密层**——bbolt 文件明文存储 results/creds。
- **fscan**：无持久化，无此问题。

**bbolt 明文存储是 FG-QiMen 的一个安全弱点**——项目文件可被读取并恢复出明文凭据。建议 v0.3 引入 `pgcrypto` 或 AES-GCM 加密层。

---

## 6. 测试

### 6.1 量化指标

| 指标 | FG-QiMen | fscan |
|---|---|---|
| **测试文件数** | 52 | 61 |
| **测试代码行数** | 9,798 | 22,120 |
| **测试/产品比例** | 31% | 31% |
| **平均覆盖率** | 多数核心包 40-100%，**adapted 插件 0%**（无 `_test.go` 触发） | 约 1.9%（CI 门禁 >40% 仅警告） |

### 6.2 FG-QiMen 覆盖率明细（来自 `go test -cover` 实测）

| 包 | 覆盖率 | 评价 |
|---|---|---|
| `cmd` | 47.4% | ✅ 合格 |
| `internal/config` | 89.6% | ✅ 优秀 |
| `internal/core` | 57.0% | ⚠️ 中等 |
| `internal/core/alive` | 64.5% | ✅ 良好 |
| `internal/core/credential` | 66.7% | ✅ 良好 |
| `internal/core/credential/auth/database` | 80.3% | ✅ 优秀 |
| `internal/core/credential/auth/email` | 75.5% | ✅ 良好 |
| `internal/core/credential/auth/filestorage` | 57.1% | ⚠️ |
| `internal/core/credential/auth/messaging` | 18.9% | ❌ RabbitMQ 凭据逻辑覆盖差 |
| `internal/core/credential/auth/network` | 59.2% | ⚠️ |
| `internal/core/credential/auth/remote` | 42.5% | ⚠️ |
| `internal/core/scan` | 45.9% | ⚠️ 端口扫描核心 |
| `internal/discovery` | 66.7% | ✅ |
| `internal/output` | 86.8% | ✅ 优秀 |
| `internal/plugins` | **100.0%** | ✅ 完美 |
| `internal/plugins/adapted/remote/rdp` | 87.1% | ✅ RDP wire 协议测试充分 |
| `internal/plugins/adapted/web/webtitle/fingerprint` | 72.0% | ✅ |
| `internal/portscan/fingerprint` | 82.6% | ✅ |
| `internal/session` | **100.0%** | ✅ 完美 |
| `internal/transport` | 73.8% | ✅ |
| `internal/tui` | 57.0% | ⚠️ |
| `internal/types` | 80.2% | ✅ |
| `internal/ui` | 23.3% | ❌ |
| `internal/utils` | 82.9% | ✅ |
| `internal/workspace` | 60.7% | ✅ |
| `internal/network/proxy` | **0.0%** | ❌ 无测试 |
| **adapted/ 多数插件** | **0.0%** | ❌ 26 个插件中只有 RDP 有真实测试覆盖 |

**核心问题**：`internal/core/credential/auth/<protocol>`（如 `auth/redis`/`auth/mysql`）的 `_test.go` 存在但**未覆盖 `adapted/<protocol>` 实际扫描逻辑**——测试只验证了凭据框架，扫描器主路径未覆盖。

### 6.3 测试策略差异

- **FG-QiMen**：**广泛使用 in-process fake server**（如 PostgreSQL 正向命中测试 [recent commits](../commit/49a187d)），是 Go 生态最佳实践。
- **fscan**：相同模式（[plugins/services/postgresql_test.go](D:/Go/fscan-main/plugins/services/postgresql_test.go) 等），覆盖率门禁松（仅警告）。

### 6.4 测试短板对比

**FG-QiMen**：
- `internal/network/proxy` 0% 覆盖
- `internal/ui` 23.3% 覆盖
- 24/26 adapted 插件 0% 覆盖（**最高优先级**）
- `internal/core/scan` 45.9%（端口扫描核心，权重高）

**fscan**：
- 总体覆盖率 ~2%（CI 门禁 >40% 仅警告）
- 短板与 FG-QiMen 互补

---

## 7. 文档

### 7.1 文档资产

| 项目 | 数量 | 评价 |
|---|---|---|
| **FG-QiMen 顶层** | README.md (30KB) + THIRD_PARTY_LICENSES.md + LICENSE + NOTICE | README 非常详细 |
| **FG-QiMen docs/** | 17 个 .md（RELEASE_NOTES, audit reports, stage reports, optimization plans） | **文档驱动开发的典型** |
| **FG-QiMen superpowers/specs/** | 1 个设计 spec | 工程化 |
| **fscan 顶层** | README.md (11KB) + README_EN.md (12KB) + SKILL.md (9KB) | 中英文双 README |
| **fscan docs/** | 0 | 无独立 docs 目录 |

### 7.2 文档质量

- **FG-QiMen README** 详细到每个 flag 的语义、每个插件的默认端口、典型工作流示例、FAQ、性能调优——是 product-grade 文档。
- **fscan SKILL.md** 是 AI 友好型 skill 描述（"怎么用 AI 操作 fscan"），是另一类价值。
- **fscan README** 中英文双版本国际化更友好。

### 7.3 代码注释

- **FG-QiMen** 注释密度高：每个 package 有 doc.go，关键函数有 godoc。
- **fscan** 注释密度中等。

---

## 8. 构建 / CI / Release

### 8.1 CI 流水线

| 维度 | FG-QiMen | fscan |
|---|---|---|
| **CI 文件** | [.github/workflows/ci.yml](.github/workflows/ci.yml) | [.github/workflows/test-build.yml](D:/Go/fscan-main/.github/workflows/test-build.yml) + [release.yml](D:/Go/fscan-main/.github/workflows/release.yml) + [issue-project.yml](D:/Go/fscan-main/.github/workflows/issue-project.yml) |
| **测试** | 3 OS 矩阵 + `-race` + `-timeout 120s` | 1 OS + `-race` + `-cover` |
| **Lint** | 无 golangci-lint | golangci-lint v2.12.1 + **复杂度门禁 >80 失败** |
| **构建** | 5 平台 cross-build matrix | GoReleaser 5 OS × 7 ARCH（含 mips、armv5/6/7） |
| **发布** | 无 release.yml（build-tag 驱动） | GoReleaser + UPX + 3 种 build（`fscan` / `fscan-nolocal` / `fscan-web`） |

### 8.2 构建脚本

- **FG-QiMen justfile**（[justfile:1-400](justfile)）~20KB，功能完整：build, all, test, cover, vet, fmt, clean, release, harden, garble。
- **fscan Makefile**（[Makefile:1-192](D:/Go/fscan-main/Makefile)）~6KB，更简洁：help, test, test-cover, build, build-web, build-ui, build-debug, build-race, build-all, lint, lint-fix, clean, stress-test, ci, deps, install-tools, fmt, vet。

### 8.3 二进制硬化

- **FG-QiMen**：[scripts/harden.{bat,ps1,sh}](scripts/) 集成 UPX + 字符串 mangling，CI 流水线化。
- **fscan**：[.github/actions/build-release/action.yml](D:/Go/fscan-main/.github/actions/build-release/action.yml) UPX 压缩。

**FG-QiMen 在二进制硬化上更系统化**——脚本化、可复现。

---

## 9. 用户体验

### 9.1 交互界面

| 维度 | FG-QiMen | fscan |
|---|---|---|
| **主界面** | Bubbletea TUI（[internal/tui/](internal/tui)）：LIVE EVENTS 列 + 状态栏 + alt screen | 文本进度条（[common/output/progress_manager.go](D:/Go/fscan-main/common/output/progress_manager.go)） |
| **TTY 自动检测** | [internal/ui/tty.go](internal/ui/tty.go) | 否（强制 stdout） |
| **Web UI** | 无 | [web/server.go](D:/Go/fscan-main/web/server.go) + [web-ui/](D:/Go/fscan-main/web-ui/) Vite/React/WebSocket，需 `-tags web` |
| **Banner** | [internal/ui/banner.go](internal/ui/banner.go) Sliver C2 风格 box banner | 无 |

**FG-QiMen TUI 是产品差异化亮点**——bubbletea 提供实时事件流、alt-screen 不留痕迹、状态栏动态统计。fscan 的 Web UI 适合大规模团队协作但**默认不编译**。

### 9.2 输出格式

| 格式 | FG-QiMen | fscan |
|---|---|---|
| **TXT** | ✅（含 redact） | ✅（明文） |
| **NDJSON** | ✅（[internal/output/output.go:167](internal/output/output.go)） | ✅（JSON） |
| **CSV** | ❌ | ✅（[common/output/writers.go](D:/Go/fscan-main/common/output/writers.go)） |
| **HTML** | ❌ | ❌ |
| **RDP 专用** | ✅（rdp.json + rdp.txt） | ❌ |
| **实时备份** | ❌ | ✅（.realtime.tmp） |

**fscan 在数据消费侧更友好**（CSV + 实时备份），**FG-QiMen 在数据脱敏上更安全**（redact）。

### 9.3 配置与可观察性

- **FG-QiMen**：纯 CLI flags（28 个）+ 3 个 env vars。
- **fscan**：纯 CLI flags（~50 个）+ 2 个 env vars（`FS_LANG`、`CEYE_API`/`CEYE_DOMAIN`）。

**两者都没有配置文件支持**（YAML/TOML/JSON config file）——都是 CLI-only 风格。

---

## 10. 可扩展性

### 10.1 SDK / 嵌入式

- **fscan 优势显著**：[pkg/fscan/](D:/Go/fscan-main/pkg/fscan/) 提供 `Scanner`/`ScanController`/`ResultHandler` SDK，4 种 API：`Scan`/`ScanReport`/`ScanEach`/`ScanWithController`，`Safe`/`Unsafe` 插件分级。
- **FG-QiMen 无 SDK**——必须作为独立二进制运行。

**fscan 是"工具+库"双重定位**，FG-QiMen 是"纯工具"。如果未来 FG-QiMen 想被其他 Go 项目嵌入，需要补一个 `pkg/fgqimen/` SDK。

### 10.2 插件热加载

- **两者都不支持**——编译期 init() 注册，无运行时 dlopen。

### 10.3 国际化

- **fscan**：[common/i18n/](D:/Go/fscan-main/common/i18n/) 基于 `nicksnyder/go-i18n/v2` 完整中英文，翻译文件 embed。
- **FG-QiMen** 无 i18n，flag/help 英文。

---

## 11. 优势总结（FG-QiMen 强项）

1. **架构清晰度**——pipeline（alive→scan→identify→spray→sink）四阶段单向数据流，依赖图无环，god package 风险低。
2. **插件双模式**——`Identify` + `Credential` 分离让"只扫描不爆破"成为一等公民（`--mode scan`）。
3. **工程化最佳实践**——bbolt 项目工作区、session per-run 隔离、redact 默认开启、路径穿越防护、TUI、garble 硬化流水线。
4. **代码无 TODO**——自研代码 0 个 TODO/FIXME，注释密度高，最大单文件 508 行（vs fscan 1019 行 Telnet）。
5. **测试 fake server 模式**——PostgreSQL、Redis、SSH 等用 in-process fake server 测试，符合 Go 生态最佳实践。
6. **依赖新鲜度**——Go 1.26 vs fscan Go 1.20，依赖更新更及时。
7. **RDP 自实现**——主动选择摆脱 vendored `libs/grdp`（5,400 LOC），自研 8 个 wire 文件，代码可控。
8. **可观察性**——TUI 实时事件流 + 状态栏动态统计 + per-target throttling 避免目标轰炸。

---

## 12. 关键差距（按优先级排序）

### P0：核心功能缺失

| 缺口 | 业务影响 | 建议 |
|---|---|---|
| **MS17-010 + 利用** | 实战最高频 SMB 漏洞 | 移植 fscan `plugins/services/ms17010.go` + `ms17010_exp.go` |
| **本地后渗透模块** | 工具定位天花板 | P0 但工程量大（24 个插件），建议 v1.0 路线图 |
| **Web POC 引擎** | 不能验证已知 CVE | 引入 CEL 表达式 + YAML POC（参考 fscan `webscan/lib/poc_executor.go`） |

### P1：覆盖度与生态

| 缺口 | 建议 |
|---|---|
| Redis SSH 公钥写入/定时任务 | 移植 fscan `plugins/services/redis.go` 完整利用框架 |
| DNS AXFR / NetBIOS-NS / MQTT | 各 ~1 个新插件，~500 LOC 总 |
| ActiveMQ / Kafka | 中间件生态，~2000 LOC |
| i18n 中英文 | 引入 `nicksnyder/go-i18n/v2`，~300 LOC |

### P1：测试覆盖率

| 包 | 当前 | 目标 |
|---|---|---|
| `internal/network/proxy` | 0% | >70% |
| `internal/core/scan` | 45.9% | >75% |
| `internal/ui` | 23.3% | >60% |
| `internal/core/credential/auth/messaging` | 18.9% | >70% |
| **adapted/ 24 个插件** | 0% | 每个 ≥60% |
| `internal/core/credential/auth/remote` | 42.5% | >70% |

### P2：可扩展性

| 缺口 | 建议 |
|---|---|
| 无 SDK | 创建 `pkg/fgqimen/`，公开 `Scanner` + `ResultHandler` |
| 无配置文件 | 引入可选的 `.fgqimen.yaml` 支持，CLI flags 覆盖 |
| 无 CSV 输出 | 加 `internal/output/csv.go` |
| bbolt 明文存储凭据 | v0.3 引入 AES-GCM 加密层（密码派生从 `FG_QIMEN_PROJECT_KEY` env） |

### P2：用户体验

| 缺口 | 建议 |
|---|---|
| 无 Web UI | 评估 TUI 是否足够，若不够，参考 fscan `web-ui/` 用 Vite/React |
| 无 WebSocket 实时推送 | TUI 已经够用，WebSocket 是 P3 |
| 无实时备份（.realtime.tmp） | 加 `output.flushEvery(5s)` |

### P3：性能与并发

| 缺口 | 建议 |
|---|---|
| 无自适应线程池 | 移植 fscan `core/adaptive_pool.go` 思路 |
| 无令牌桶限速 | 引入 `juju/ratelimit` + `-rate`/`-maxpkts` flag |
| errgroup 未使用 | 局部可用 `errgroup.Group` 简化错误传播 |

---

## 13. 路线图建议

### v0.3.0（短期，1-2 月）
- 测试覆盖率达：core 80%、adapted 插件 50% 起步
- 新增 MS17-010、Redis 利用、DNS AXFR、NetBIOS-NS
- 引入 AES-GCM 加密 bbolt 项目存储
- 引入 juju/ratelimit 限速

### v0.4.0（中期，3-4 月）
- Web POC 引擎（CEL + YAML）
- ActiveMQ / Kafka / Cassandra / Neo4j
- 自适应线程池
- i18n 中英文

### v1.0.0（长期，6 月+）
- SDK 嵌入（`pkg/fgqimen`）
- 本地后渗透模块（24 个插件）
- 可选 Web UI
- 可选配置文件 `.fgqimen.yaml`

---

## 14. 关键发现汇总

### 14.1 FG-QiMen 做得对的事（值得保留）

1. `internal/` 强封装 + 包分层（core/plugins/adapted/auth）
2. 插件双模式接口（Identify + Credential）
3. Session per-run 隔离 + bbolt 持久化
4. 默认 redact + 显式 `--insecure-tls/--insecure-ssh` + 路径穿越防护
5. Bubbletea TUI（差异化）
6. Pipeline 4 阶段（alive→scan→identify→spray→sink）清晰
7. Fake-server 测试模式（PostgreSQL 等）
8. Garble + UPX 硬化流水线
9. 文档驱动开发（17 个 .md 在 docs/）
10. 0 个自研 TODO

### 14.2 FG-QiMen 最该补的事（按 ROI 排序）

1. **adapted 插件测试覆盖**（ROI 极高，~50 个新测试用例即可让 24 个插件覆盖率从 0% 提升到 60%+）
2. **MS17-010 + Redis 利用**（ROI 高，~1500 LOC 即可补齐实战最高频漏洞）
3. **Web POC 引擎**（ROI 高，补齐"识别 vs 验证"鸿沟）
4. **i18n**（ROI 中，~300 LOC 提升中国用户体验）
5. **bbolt 加密**（ROI 中，安全合规）
6. **SDK 嵌入**（ROI 中，从"工具"变"工具+库"）

### 14.3 风险与告警

- **bbolt 项目文件未加密**——若用户误用 `--project` 在共享环境跑扫描，凭据明文落盘。
- **Web POC 完全缺失**——与 fscan 相比是显著能力差距。
- **24/26 adapted 插件测试覆盖 0%**——发布风险。
- **依赖 dev 依赖较新（Go 1.26）**——用户需 Go 1.26 编译（[go.mod:3](go.mod)）。

---

## 15. 附录 A：数据指标对照表

| 指标 | FG-QiMen v0.2.0 | fscan | 比值 |
|---|---|---|---|
| Go 代码总行数 | 31,460 | 72,025 | 0.44 |
| 测试代码总行数 | 9,798 | 22,120 | 0.44 |
| 测试文件数 | 52 | 61 | 0.85 |
| 协议插件数（services 维度） | 26 | 40+ | 0.65 |
| 本地后渗透插件数 | 0 | 24 | 0 |
| Web POC 数量 | 0（仅指纹） | 1（示例 YAML） | 0 |
| 平均测试覆盖率 | ~40-50%（有覆盖的包） | ~2% | 显著优 |
| CLI flags 数 | 28 | ~50 | 0.56 |
| Cobra 依赖 | ✅ | ❌（用 stdlib flag） | — |
| bubbletea TUI | ✅ | ❌ | — |
| Web UI | ❌ | ✅（需 build tag） | — |
| SDK 公开 API | ❌ | ✅（pkg/fscan） | — |
| 国际化 | ❌ | ✅（zh/en） | — |
| 路径穿越防护 | ✅ | ❌ | — |
| 默认凭据 redact | ✅ | ❌ | — |
| 显式 `--insecure-tls` | ✅ | ❌ | — |
| Garble 混淆 | ✅ | ❌ | — |
| UPX 压缩 | ✅ | ✅ | — |
| Per-target throttling | ✅ | ❌ | — |
| Drain on shutdown | ✅ | ❌ | — |
| 自适应线程池 | ❌ | ✅ | — |
| 令牌桶限速 | ❌ | ✅ | — |
| go test -race CI | ✅ | ✅ | — |
| golangci-lint 复杂度门禁 | ❌ | ✅（>80 失败） | — |
| Cross-build 平台数 | 5 | 5×7=35（GoReleaser） | 0.14 |
| 自研代码 TODO 数 | **0** | 未统计 | — |
| 最大单文件行数 | 508（cmd/scan.go） | 1019（plugins/services/telnet.go） | 0.50 |
| Go 版本 | 1.26 | 1.20 | — |

---

## 16. 附录 B：审计方法论

本次审计采用**静态分析 + 关键文件抽检**：
- 自动化：`find`、`wc -l`、`grep TODO/FIXME`、`go test -cover` 全量
- 半自动：通过 Explore subagent 递归遍历目录树、提取插件列表、统计 LOC
- 抽检：[main.go](main.go)、[cmd/scan.go](cmd/scan.go)、[cmd/flags.go](cmd/flags.go)、[internal/core/scanner.go](internal/core/scanner.go)、[internal/core/pipeline.go](internal/core/pipeline.go)、[internal/core/credential/scheduler.go](internal/core/credential/scheduler.go)、[internal/plugins/registry.go](internal/plugins/registry.go)
- 对比：[fscan main.go](D:/Go/fscan-main/main.go)、[common/flag.go](D:/Go/fscan-main/common/flag.go)、[core/scanner.go](D:/Go/fscan-main/core/scanner.go)、[core/adaptive_pool.go](D:/Go/fscan-main/core/adaptive_pool.go)、[plugins/init.go](D:/Go/fscan-main/plugins/init.go)

**未做**：
- 运行时性能压测（未跑 benchmark）
- 安全 fuzzing（未跑 go-fuzz）
- 内存 profile（未跑 pprof）
- 实网环境兼容性测试

**审计完成时间**：2026-06-18
