# FG-QiMen v0.3 全面优化方案（不含漏洞利用）

> **方案版本**：v0.3 路线图
> **制定日期**：2026-06-18
> **依据**：[audit-report-vs-fscan.md](audit-report-vs-fscan.md) 全面审计 + 当前代码基线（commit 49a187d）
> **范围声明**：本方案**不包含**漏洞利用方向（MS17-010、Redis SSH 公钥写入、SMB 漏洞利用、本地后渗透模块、Web POC 引擎、TLS/SSH 攻击载荷等）。**仅聚焦**工程质量、性能、安全防御、用户体验、可扩展性、可观察性。

---

## 0. 执行摘要

### 0.1 现状速览

| 维度 | 状态 | 主要问题 |
|---|---|---|
| 测试覆盖率 | 重点模块 60-100%，**24/26 adapted 插件 0%** | 最大单一风险 |
| 协议覆盖 | 26 个识别插件 | 缺 DNS/NetBIOS-NS/MQTT 等 |
| 架构 | pipeline 4 阶段清晰 | 内部包嵌套深，godoc 入口成本高 |
| 性能 | channel sem + worker pool | 无自适应线程池、无令牌桶限速 |
| 安全 | 默认 redact、显式 `--insecure-*` | **bbolt 凭据明文落盘** |
| UX | TUI 差异化强 | 无 CSV、无 i18n |
| CI | 3 OS race + 5 平台构建 | 无 golangci-lint、无 release.yml |
| SDK | 无公开 API | 不能被其他 Go 项目嵌入 |
| 文档 | 17 个 .md 完善 | 缺少 API 文档（go doc） |

### 0.2 优化主题（10 大方向）

| # | 主题 | 优先级 | 涉及包数 | 估时（人天） |
|---|---|---|---|---|
| 1 | **测试覆盖率补齐** | **P0** | 30+ | 15 |
| 2 | **代码质量 & 架构** | P1 | 10 | 8 |
| 3 | **性能 & 并发** | P1 | 5 | 6 |
| 4 | **安全防御** | **P0** | 4 | 4 |
| 5 | **用户体验** | P1 | 6 | 7 |
| 6 | **协议覆盖（识别）** | P2 | 3 | 5 |
| 7 | **文档 & API 文档** | P2 | 5 | 4 |
| 8 | **CI / Release** | P1 | 3 | 3 |
| 9 | **可扩展性（SDK + Config）** | P2 | 4 | 6 |
| 10 | **可观察性（Metrics/Tracing）** | P3 | 6 | 4 |

**总估时**：~62 人天（约 3 个月 1 人，或 1.5 月 2 人并行）

### 0.3 路线图分期

| 版本 | 周期 | 主要内容 |
|---|---|---|
| **v0.3.0** | 6 周 | 测试补齐 + bbolt 加密 + 协议补全（DNS/NetBIOS-NS/MQTT）+ 性能基线 + i18n + golangci-lint |
| **v0.4.0** | 8 周 | 自适应线程池 + 令牌桶限速 + SDK + Config File + 可观察性 |
| **v0.5.0** | 8 周 | SDK（`pkg/fgqimen`）+ Config File + Prometheus 指标 + 协议识别补全（NTP/UPnP/CoAP/RTSP/SIP 等） |
| **v1.0.0** | 12 周 | 完整 API 稳定 + 第三方插件机制 + 分布式扫描协调（可选） |

---

## 1. 优化原则

### 1.1 设计原则
- **不破坏向后兼容**：CLI flags / 输出格式 / bbolt 文件格式升级需 `--migrate` 或版本字段
- **不引入新的全局状态**：除已有 `proxy.GetGlobalDialer()` 外
- **不引入新依赖除非必要**：优先用 stdlib + 现有依赖
- **测试先行**：所有新功能必须先有 `_test.go`
- **可观测性是产品功能，不是事后补丁**

### 1.2 范围声明（明确不做）
❌ MS17-010 / SMB / 任何 CVE 漏洞利用代码
❌ Redis SSH 公钥写入 / 定时任务写入
❌ 本地后渗透模块（反弹 shell / 持久化 / 痕迹清理）
❌ Web POC 引擎（CEL 表达式 / YAML POC 加载）
❌ LDAP NTHash / Kerberos 攻击
❌ Telnet CVE-2026-24061 NEW-ENVIRON 利用
❌ 任何"识别即利用"的混合插件

**本方案的"协议覆盖"专注于"安全识别"——告诉用户"这个端口跑什么、什么版本、有没有已知 Banner 模式"——而非"如何打"**。

---

## 2. 主题一：测试覆盖率补齐（P0 / 15 人天）

### 2.1 现状
- `internal/network/proxy`：**0%** ❌
- `internal/ui`：**23.3%** ❌
- `internal/core/credential/auth/messaging`（RabbitMQ）：**18.9%** ❌
- `internal/core/credential/auth/remote`：**42.5%** ⚠️
- `internal/core/scan`：**45.9%** ⚠️
- `internal/core/credential/auth/filestorage`：**57.1%** ⚠️
- `internal/core/credential/auth/network`：**59.2%** ⚠️
- **24/26 `adapted/` 插件识别逻辑：0%** ❌（虽然 `auth/<plugin>` 有测试但 `adapted/<plugin>` 实际扫描路径无覆盖）

### 2.2 目标
- 所有 P0 包覆盖率 ≥ 80%
- 所有 `adapted/<protocol>` 插件识别逻辑 ≥ 60%（用 in-process fake server）
- 引入覆盖率门禁：CI 失败如果 < 70%

### 2.3 实施步骤

#### 步骤 1：补 `internal/network/proxy` 测试（0% → 80%）
- [ ] [internal/network/proxy/manager.go:42-148](internal/network/proxy/manager.go) `Manager.GetDialer()`：单元测试覆盖单例、并发初始化、`ResetGlobalManager` 重置
- [ ] `socks5.go` / `http.go` / `validator.go`：fake SOCKS5 / HTTP proxy server 测试 dial 行为
- [ ] `DirectDialer.DialContext`：用 `net.Pipe()` 模拟连接
- [ ] 边界：nil config、超时、context 取消

#### 步骤 2：补 `internal/core/scan` 测试（45.9% → 75%）
- [ ] [internal/core/scan/pool.go](internal/core/scan/pool.go) 自适应线程池的 min/max/scale 路径
- [ ] [internal/core/scan/udp_probe.go](internal/core/scan/udp_probe.go) UDP 探测的 send/recv/timeout
- [ ] [internal/core/scan/prescreen.go](internal/core/scan/prescreen.go) 端口预筛选
- [ ] [internal/core/scan/iterator.go](internal/core/scan/iterator.go) 端口迭代器（边界、范围、解析）
- [ ] [internal/core/scan/retry.go](internal/core/scan/retry.go) 重试退避

#### 步骤 3：补 `adapted/<plugin>` 识别路径（0% → 60%）
每个协议按 `internal/core/credential/auth/<plugin>_test.go` 模式，补 `internal/plugins/adapted/<category>/<plugin>/<plugin>_test.go`，用 in-process fake server：
- 已有 fake server 模式可参考（[internal/core/credential/auth/database/postgresql_test.go](internal/core/credential/auth/database/)）
- 每个插件至少 3 个用例：banner 正确识别、协议错误无响应、连接超时
- 协议列表：elasticsearch, memcached, mongodb, mssql, mysql, oracle, redis, imap, pop3, smtp, nfs, rsync, smb, rabbitmq, bacnet, docker, ldap, modbus, snmp, socks5, ipmi, telnet, vnc, winrm, ssh, ftp（**26 个插件**）
- 估时：每插件 0.3 人天 × 26 = ~8 人天

#### 步骤 4：补 `internal/ui` / `internal/core/credential/auth/messaging` / `auth/remote`
- [ ] [internal/ui/factory.go](internal/ui/factory.go) factory pattern 选择逻辑
- [ ] [internal/ui/banner.go](internal/ui/banner.go) box banner 渲染
- [ ] [internal/core/credential/auth/messaging/rabbitmq_test.go](internal/core/credential/auth/messaging/)：AMQP 协议 + Mgmt API 路径
- [ ] [internal/core/credential/auth/remote/](internal/core/credential/auth/remote/)：ssh/telnet/vnc/winrm 真实协议路径

#### 步骤 5：建立覆盖率门禁
- [ ] 在 [justfile](justfile) 加 `cover-check` 任务：
  ```makefile
  cover-check:
      go test -coverprofile=coverage.out -covermode=atomic ./...
      go tool cover -func=coverage.out | awk '/^total/ {if ($3+0 < 70) exit 1}'
  ```
- [ ] [.github/workflows/ci.yml](.github/workflows/ci.yml) 增加 `cover-check` 步骤，< 70% 失败

### 2.4 验收标准
- `go test -cover ./...` 输出：所有 P0 包 ≥ 80%，adapted 插件 ≥ 60%
- CI 在覆盖率 < 70% 时失败
- 总覆盖率（按语句计）从当前 ~35% 提升到 ≥ 65%

---

## 3. 主题二：代码质量 & 架构（P1 / 8 人天）

### 3.1 问题列表

| 位置 | 问题 | 严重度 |
|---|---|---|
| [cmd/scan.go:508](cmd/scan.go) | 单文件 508 行，混合 config 构建、scan 启动、UI 协调、清理 | 中 |
| `internal/core/credential/auth/<plugin>/` | 同一插件有 credential + adapted 两个文件分散，跨目录引用 | 中 |
| [internal/core/pipeline.go:138](internal/core/pipeline.go) + [internal/core/pipeline_workers.go](internal/core/pipeline_workers.go) + [internal/core/pipeline_sink.go](internal/core/pipeline_sink.go) | 三个文件职责交叉，pipeline 生命周期散落 | 中 |
| `internal/core/credential/scheduler.go:220` | 220 行单文件，混合 semaphore、per-target throttling、stop-on-hit | 中 |
| [main.go:1-25](main.go) | `os.Exit(1)` 直接退出，绕过 cobra 错误处理 | 低 |

### 3.2 目标
- 降低单文件最大行数（从 508 → < 400）
- 拆分 [cmd/scan.go](cmd/scan.go) 为多个职责清晰的小文件
- 引入 `internal/scan/` 统一识别 + 凭据入口（消除 `core/credential` 和 `plugins/adapted` 之间的目录跳跃）

### 3.3 实施步骤

#### 步骤 1：拆分 [cmd/scan.go](cmd/scan.go)（508 → 4 个文件）
- [ ] `cmd/scan.go` (200行) — `runScan` 主流程
- [ ] `cmd/scan_config.go` (120行) — `buildConfig()` + 校验
- [ ] `cmd/scan_cleanup.go` (80行) — `cleanup()` + signal 协调
- [ ] `cmd/scan_init.go` (108行) — proxy init + session + UI 初始化

#### 步骤 2：合并 `plugins/adapted/<plugin>` 和 `core/credential/auth/<plugin>` 为同包
当前结构：协议 X 的"识别"在 `internal/plugins/adapted/<cat>/X/X.go`，"凭据"在 `internal/core/credential/auth/<cat>/X.go`
- [ ] 重构为 `internal/scan/<cat>/X/` 单一包，含 `identify.go` + `credential.go` + `wire/` 子包（如 RDP）
- [ ] 优点：插件自包含，新人无需跨 4 层目录理解
- [ ] 风险：需更新 `import` 路径；bbolt 项目文件不存 import 路径，影响小

#### 步骤 3：[main.go](main.go) 改进
```go
// 当前：
if err := cmd.Execute(); err != nil {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(1)
}

// 改为：
if err := cmd.Execute(); err != nil {
    // cobra 已输出错误，仅退出
    os.Exit(1)
}
```

#### 步骤 4：拆分 [internal/core/credential/scheduler.go](internal/core/credential/scheduler.go)
- [ ] `scheduler.go` (100行) — Scheduler struct + 公共 API
- [ ] `scheduler_throttle.go` (60行) — per-target throttling
- [ ] `scheduler_sem.go` (60行) — channel semaphore

### 3.4 验收标准
- 单文件最大行数 < 400
- `go vet ./...` 通过
- `gofmt -s -l` 无输出
- 所有现有测试通过

---

## 4. 主题三：性能 & 并发（P1 / 6 人天）

### 4.1 现状问题

| 位置 | 问题 |
|---|---|
| [internal/core/scan/pool.go](internal/core/scan/pool.go) | 线程数固定，无自适应（fscan 有 [adaptive_pool.go](D:/Go/fscan-main/core/adaptive_pool.go)） |
| 整体 | 无令牌桶限速，扫描大网段可能触发 IDS |
| [internal/core/scanner.go:178-185](internal/core/scanner.go) | plugin worker 默认 16，无 OOM 防护 |
| [internal/core/pipeline.go](internal/core/pipeline.go) | 全局 channel buffer 1024，瓶颈测试未见 |

### 4.2 目标
- 引入自适应线程池（参考 fscan 实现）
- 引入令牌桶限速
- 增加 benchmark 基线测试

### 4.3 实施步骤

#### 步骤 1：自适应线程池
- [ ] 新建 `internal/core/scan/adaptive_pool.go`
  - 基于 CPU 核数 + 内存 + 当前 goroutine 数动态调整
  - 策略：耗尽率 > 10% → 降 25%，< 2% → 升 25%
  - 已在 [internal/core/constants.go:42-62](internal/core/constants.go) 预留了 `DefaultAdjustInterval/DefaultScaleDown/DefaultScaleUp/DefaultMaxSamples/DefaultMinSamples` 常量——直接使用
- [ ] 添加 `--adaptive-pool` flag（[cmd/flags.go](cmd/flags.go)），默认开启
- [ ] 在 `internal/core/scan/pool.go` 中替换固定池为自适应池

#### 步骤 2：令牌桶限速
- [ ] 新建 `internal/ratelimit/bucket.go`，实现 stdlib `golang.org/x/time/rate.Limiter` 包装
- [ ] 添加 `--rate` flag（每秒包数）、`--max-packets` flag（总包数上限）
- [ ] 在 [internal/core/alive/icmp.go](internal/core/alive/icmp.go) 和 [internal/core/scan/tcp_connect.go](internal/core/scan/tcp_connect.go) 入口处限速

#### 步骤 3：OOM 防护
- [ ] plugin worker 队列满时丢弃并告警（[internal/core/scanner.go:178-185](internal/core/scanner.go)）
- [ ] `internal/core/scanner.go` 增加 memory pressure 检查（`runtime.MemStats`）

#### 步骤 4：基准测试
- [ ] 补全 [internal/core/benchmark_test.go](internal/core/benchmark_test.go)：端口扫描、credential scheduler、pipeline 端到端
- [ ] 在 CI 中跑 benchmark（不强制通过，仅记录趋势）

### 4.4 验收标准
- 默认开启自适应线程池 + 限速
- 100K 端口扫描无 OOM
- benchmark 数字记录到 docs/perf-baseline.md

---

## 5. 主题四：安全防御（P0 / 4 人天）

> **声明**：本主题是"被扫描端安全"和"FG-QiMen 自身安全"，**不涉及攻击**。

### 5.1 问题列表

| 位置 | 问题 | 严重度 |
|---|---|---|
| [internal/store/store.go:119-156](internal/store/store.go) | bbolt 数据库明文存储凭据 | **P0** |
| [internal/store/store.go:1-196](internal/store/store.go) | 无 mmap 加密层 | **P0** |
| [cmd/scan.go](cmd/scan.go) | 无 `--max-runtime` 防止长时间失控 | P1 |
| [internal/core/scanner.go](internal/core/scanner.go) | 无速率限制（已在主题三解决） | P1 |
| [internal/output/output.go:91-115](internal/output/output.go) | 输出文件权限 0o644，凭据文件应 0o600 | P2 |
| [justfile:48](justfile) | `use_garble=1` 默认开，但 README 未声明此为安全特性 | P3 |

### 5.2 目标
- bbolt 凭据加密（AES-256-GCM）
- 输出文件权限收紧
- 引入运行时上限

### 5.3 实施步骤

#### 步骤 1：bbolt 加密（P0）
- [ ] 引入依赖：`github.com/etcd-io/bbolt` 已支持 `Options.WithEncryptionKey`（需 bbolt ≥ 1.4.0）
  - 当前 [go.mod](go.mod) 已是 `bbolt v1.4.3`，**已支持**
- [ ] 添加 `--project-key` flag（默认从 `FG_QIMEN_PROJECT_KEY` env 读取；未设置时 warn 并使用 ephemeral 模式）
- [ ] [internal/store/store.go](internal/store/store.go) `NewStore()` 接收 `key []byte`，传给 `bolt.Open(path, 0o600, &bolt.Options{EncryptionKey: key})`
- [ ] 升级路径：检测无加密 bbolt 文件，提示用户用 `--migrate-encrypt` 重新创建
- [ ] 关键：保留 backward compat——bbolt 1.4.3 加密文件头不同，需用 `bolt.Open` 自动检测

#### 步骤 2：凭据文件权限收紧
- [ ] [internal/output/output.go:91-115](internal/output/output.go) `OpenOutput()`：`creds.txt` 改为 0o600
- [ ] 添加 `--umask` flag（默认 0o077，敏感文件 0o600）

#### 步骤 3：运行时上限
- [ ] 添加 `--max-runtime` flag（默认无上限）
- [ ] 在 [cmd/scan.go](cmd/scan.go) 主流程中用 `context.WithTimeout` 包装
- [ ] 触发后优雅退出（走 `signal.go` 的 drain 路径）

#### 步骤 4：审计日志
- [ ] 新建 `internal/audit/log.go`，记录所有 bbolt 打开、凭据查询、output 写入事件
- [ ] 添加 `--audit-log <path>` flag
- [ ] 日志格式 JSON Lines，包含时间戳、actor、action、target

### 5.4 验收标准
- bbolt 项目文件用错误密码打开返回错误（不 panic）
- `creds.txt` 文件权限为 0o600
- 运行时超过 `--max-runtime` 后优雅退出

---

## 6. 主题五：用户体验（P1 / 7 人天）

### 6.1 问题列表

| 位置 | 问题 | 用户影响 |
|---|---|---|
| [internal/output/output.go:65-71](internal/output/output.go) | 输出格式仅 TXT/NDJSON/creds/RDP，缺 CSV/HTML | 数据消费方需自行解析 |
| [cmd/flags.go:106-188](cmd/flags.go) | 28 个 flag，无分组 | 新手难找 |
| [internal/ui/banner.go](internal/ui/banner.go) | Banner ASCII 字符在 Windows Console 可能乱码 | Windows 用户体验差 |
| 整体 | 无 i18n，仅英文 flag/help | 中国用户友好度低 |
| 整体 | TUI 仅 TTY 检测，无 TUI 配置 | 没法禁用 TUI 动画或调色板 |
| [cmd/scan.go](cmd/scan.go) | 错误信息英文，无错误码 | 自动化集成困难 |

### 6.2 目标
- 添加 CSV 输出
- flag 分组 + 帮助文本改进
- i18n 中英文（基于 [nicksnyder/go-i18n/v2](D:/Go/fscan-main/go.mod)）
- 错误码系统

### 6.3 实施步骤

#### 步骤 1：CSV 输出（1 人天）
- [ ] 新建 [internal/output/csv.go](internal/output/csv.go)
- [ ] 添加 `--output-csv` flag
- [ ] 复用 `flushCloser` 模式
- [ ] 列：time, host, port, protocol, plugin, state, info, user, pass（脱敏）

#### 步骤 2：flag 分组 + 帮助文本（1 人天）
- [ ] 用 cobra `Flags()` API 分组：
  - **Target**: `--host`, `--hosts-file`, `--exclude-ports`
  - **Scan**: `--mode`, `--ports`, `--threads`, `--timeout`
  - **Credential**: `--user`, `--pass`, `--user-file`, `--pass-file`
  - **Output**: `--output-txt`, `--output-json`, `--output-csv`, `--silent`
  - **Network**: `--proxy`, `--socks5`, `--iface`
  - **Safety**: `--insecure-tls`, `--insecure-ssh`, `--known-hosts`
- [ ] 每组有 `cmd.SetUsageTemplate` 定制 help

#### 步骤 3：i18n 中英文（3 人天）
- [ ] 引入 `github.com/nicksnyder/go-i18n/v2`
- [ ] 新建 [internal/i18n/](internal/i18n/) 包，含 `en.toml` 和 `zh.toml`
- [ ] 翻译所有 flag help、所有 UI 文本、所有 log 消息
- [ ] 添加 `--lang` flag（默认从 `LANG` env 推断）
- [ ] [cmd/flag.go:299-325](cmd/flag.go)（参考 fscan 实现）— 在 flag 解析前先处理 `--lang`

#### 步骤 4：错误码系统（2 人天）
- [ ] 定义错误码常量在 [internal/types/errors.go](internal/types/errors.go)：
  - `E001` invalid target
  - `E002` invalid port
  - `E003` invalid credential
  - `E101` bbolt open failed
  - `E102` bbolt decrypt failed
  - `E201` proxy dial failed
  - `E301` all plugins failed
  - `E999` unknown
- [ ] `--json-errors` flag 输出 JSON 格式错误（带 code、message、hint）
- [ ] 错误消息含 `Hint:` 字段（修复建议）

### 6.4 验收标准
- CSV 输出文件用 Excel/LibreOffice 打开正常
- `fg-qimen --help` 中文/英文切换正常
- 所有错误输出可被脚本解析（错误码稳定）

---

## 7. 主题六：协议覆盖（识别而非利用）（P2 / 5 人天）

### 7.1 现状
FG-QiMen v0.2.0 有 26 个协议识别插件。**本主题不引入任何漏洞利用代码**——只补全"识别 Banner / 协议握手 / 服务版本"。

### 7.2 缺失识别能力的低风险协议

| 协议 | 端口 | 识别方法 | 估时 | 优先级 |
|---|---|---|---|---|
| **DNS** | 53/UDP+TCP | AXFR 探测（zone transfer 尝试）+ 版本.bind 查询 | 1 人天 | P1 |
| **NetBIOS-NS** | 137/UDP | 节点状态查询（无利用，纯识别） | 0.5 人天 | P1 |
| **MQTT** | 1883/8883 | CONNECT 报文 banner 抓取 | 0.5 人天 | P1 |
| **NTP** | 123/UDP | monlist 探测（仅识别 NTP 服务存在，不读取列表内容） | 0.3 人天 | P2 |
| **UPnP/SSDP** | 1900/UDP | M-SEARCH 探测 | 0.5 人天 | P2 |
| **CoAP** | 5683/UDP | GET /.well-known/core 探测 | 0.5 人天 | P3 |
| **WebSocket** | 动态 | HTTP Upgrade 探测 | 0.3 人天 | P3 |
| **RTSP** | 554/TCP | OPTIONS 探测 | 0.3 人天 | P3 |
| **SIP** | 5060/UDP | OPTIONS 探测 | 0.3 人天 | P3 |
| **SOCKS4** | 1080 | SOCKS4 握手探测 | 0.3 人天 | P3 |

**总计 ~4.5 人天**

### 7.3 实施步骤
对每个新协议：
- [ ] 创建 `internal/scan/<protocol>/<protocol>.go` 插件文件
- [ ] 默认端口注册
- [ ] `Identify` 模式：仅读取 banner/握手响应，**不发送任何修改状态的命令**
- [ ] `Credential` 模式（如适用）：仅密码认证，**不实现任何利用**
- [ ] `_test.go` 用 in-process fake server
- [ ] 更新 [README.md](README.md) 协议列表

### 7.4 验收标准
- 每个新协议 0 个写操作（只读）
- 所有新协议测试通过
- 协议总数从 26 提升到 35-36

---

## 8. 主题七：文档 & API 文档（P2 / 4 人天）

### 8.1 现状
- 17 个 .md 文档在 [docs/](docs/)
- **无 godoc API 文档自动生成**
- **无 examples/ 目录**（fscan 有 [examples/](D:/Go/fscan-main/examples/)）
- **无 CHANGELOG.md**（v0.1 → v0.2 变更散落 commits）

### 8.2 目标
- pkg.go.dev 友好：godoc 完整、所有 exported 符号有注释
- examples/ 目录：3 个使用示例
- CHANGELOG.md 标准格式
- CONTRIBUTING.md

### 8.3 实施步骤

#### 步骤 1：godoc 完整化（2 人天）
- [ ] 审计所有 `*.go` 文件的 exported 符号
- [ ] 补全缺失的 godoc 注释
- [ ] 重点：[internal/types/](internal/types/)、[internal/session/](internal/session/)、[internal/workspace/](internal/workspace/)
- [ ] 在 [justfile](justfile) 加 `doc` 任务：`go doc -all ./... > docs/api-reference.md`
- [ ] CI 加 `go doc -all ./... | grep -E "^func [A-Z]" | awk '{ ... }'` 检查 exported 函数是否都有注释

#### 步骤 2：examples/ 目录（1 人天）
- [ ] [examples/basic/main.go](examples/basic/main.go) — 最小化命令行调用
- [ ] [examples/pipeline/main.go](examples/pipeline/main.go) — 直接调用内部 pipeline API
- [ ] [examples/project/main.go](examples/project/main.go) — bbolt 项目模式
- [ ] 每个 example 有 README.md 说明用途

#### 步骤 3：CHANGELOG.md（0.5 人天）
- [ ] 创建 [CHANGELOG.md](CHANGELOG.md)，遵循 [Keep a Changelog](https://keepachangelog.com/) 格式
- [ ] 倒序记录 v0.1.0, v0.2.0
- [ ] 未来版本按时间倒序追加

#### 步骤 4：CONTRIBUTING.md（0.5 人天）
- [ ] 创建 [CONTRIBUTING.md](CONTRIBUTING.md)
- [ ] 包含：开发环境、justfile 用法、测试要求、PR 流程、代码规范

### 8.4 验收标准
- `go doc -all ./internal/...` 无 "missing comment" 警告
- examples/ 3 个 example 编译通过
- CHANGELOG.md 存在

---

## 9. 主题八：CI / Release（P1 / 3 人天）

### 9.1 现状
- [.github/workflows/ci.yml](.github/workflows/ci.yml) 有 test + cross-build
- **无 golangci-lint**
- **无 release.yml**（仅 build-tag 驱动）
- **无 CodeQL / SAST**
- **无 Dependabot**
- **无 changelog 自动生成**

### 9.2 目标
- 引入 golangci-lint v2（与 fscan 同版本）
- GoReleaser 自动化发布
- Dependabot 依赖更新
- CodeQL 静态分析

### 9.3 实施步骤

#### 步骤 1：golangci-lint 配置（0.5 人天）
- [ ] 创建 [.golangci.yml](.golangci.yml)（参考 [fscan 配置](D:/Go/fscan-main/.golangci.yml)）
- [ ] 启用 linters：errcheck, govet, staticcheck, ineffassign, unused, gosimple, gocritic, misspell, gosec, prealloc
- [ ] 复杂度门禁（与 fscan 一致）：revive `cognitive-complexity` > 80 失败
- [ ] [.github/workflows/ci.yml](.github/workflows/ci.yml) 加 `lint` 任务

#### 步骤 2：GoReleaser 配置（1 人天）
- [ ] 创建 [.github/workflows/release.yml](.github/workflows/release.yml)
- [ ] 创建 [.goreleaser.yml](.goreleaser.yml)：
  - 5 OS × 2 ARCH（amd64 + arm64）
  - ldflags 注入 version/commit/date
  - UPX 压缩（best）
  - sha256sums
  - SBOM 生成（syft）
- [ ] 触发条件：`tag: v*`
- [ ] 复刻 fscan 的 [build-release/action.yml](D:/Go/fscan-main/.github/actions/build-release/action.yml)

#### 步骤 3：Dependabot（0.3 人天）
- [ ] 创建 [.github/dependabot.yml](.github/dependabot.yml)
- [ ] 监控：`gomod`, `github-actions`
- [ ] 周一北京时间 8:00 推送 PR（避免撞车）

#### 步骤 4：CodeQL（0.5 人天）
- [ ] 创建 [.github/workflows/codeql.yml](.github/workflows/codeql.yml)
- [ ] 启用 security-and-quality 查询套件
- [ ] 触发：push to main, PR, 周一调度

#### 步骤 5：CHANGELOG 自动生成（0.2 人天）
- [ ] 集成 `github.com/goreleaser/chglog` 或 `git-chglog`
- [ ] 在 GoReleaser 中调用，发布时附 CHANGELOG

### 9.4 验收标准
- golangci-lint 在 CI 中失败如果违规
- `git tag v0.3.0 && git push --tags` 触发自动 release
- Dependabot PR 正常生成

---

## 10. 主题九：可扩展性（SDK + Config File）（P2 / 6 人天）

### 10.1 现状
- **无 SDK**——必须作为独立二进制运行
- **无配置文件**——所有配置通过 28 个 CLI flags
- **无插件热加载**——编译期 init() 注册

### 10.2 目标
- 公开 `pkg/fgqimen` SDK
- 支持 `.fgqimen.yaml` 配置文件（CLI flags 覆盖）
- 公开 `internal/scan` 包的稳定 API

### 10.3 实施步骤

#### 步骤 1：`pkg/fgqimen` SDK（4 人天）
- [ ] 创建 [pkg/fgqimen/scanner.go](pkg/fgqimen/scanner.go)
  - `type Scanner struct {...}` — 公开扫描器
  - `func New(opts Options) *Scanner` — 构造
  - `func (s *Scanner) Run(ctx) error` — 启动
  - `func (s *Scanner) Results() <-chan *Result` — 流式结果
  - `func (s *Scanner) Stats() <-chan *Stats` — 流式统计
- [ ] 创建 [pkg/fgqimen/types.go](pkg/fgqimen/types.go) — 公开 `Result` / `Stats` / `Options`
- [ ] 创建 [pkg/fgqimen/handler.go](pkg/fgqimen/handler.go) — `ResultHandler` 接口（`OnResult` / `OnProgress` / `OnError`）
- [ ] 创建 [pkg/fgqimen/example_test.go](pkg/fgqimen/example_test.go) — godoc 可执行示例
- [ ] 创建 [examples/sdk-basic/main.go](examples/sdk-basic/main.go) — SDK 使用示例
- [ ] 关键：`pkg/fgqimen` 是 **stable API**，`internal/*` 可随意重构

#### 步骤 2：Config File 支持（2 人天）
- [ ] 引入 `gopkg.in/yaml.v3`（轻量）
- [ ] 创建 [internal/configfile/](internal/configfile/) 包
- [ ] 解析顺序：CLI flags > `FG_QIMEN_CONFIG` env > `./.fgqimen.yaml` > `$HOME/.fgqimen.yaml` > defaults
- [ ] 示例 [examples/.fgqimen.yaml](examples/.fgqimen.yaml)：
  ```yaml
  target:
    hosts: ["10.0.0.0/24", "192.168.1.1-100"]
    exclude_ports: ["22", "3389"]
  scan:
    mode: scan
    threads: 200
    timeout: 3s
  credential:
    user_files: ["users.txt"]
    pass_files: ["passes.txt"]
  output:
    txt: results.txt
    json: results.json
  network:
    proxy: socks5://127.0.0.1:1080
  safety:
    insecure_tls: false
    insecure_ssh: false
  ```

### 10.4 验收标准
- `pkg/fgqimen` 包可通过 `go get github.com/LCUstinian/FG-QiMen/pkg/fgqimen` 引用
- SDK example 编译并运行
- 配置文件优先级正确（CLI 覆盖文件）

---

## 11. 主题十：可观察性（P3 / 4 人天）

### 11.1 现状
- **无 metrics 暴露**（Prometheus 等）
- **无 tracing**（OpenTelemetry 等）
- **无结构化日志聚合**
- **无健康检查端点**

### 11.2 目标
- 指标可导出（按需启用）
- 结构化日志（zap 或 zerolog）
- TUI 实时指标保留并增强

### 11.3 实施步骤

#### 步骤 1：结构化日志（1.5 人天）
- [ ] 引入 `github.com/rs/zerolog`（轻量）
- [ ] 替换 [internal/types/logger.go](internal/types/logger.go) 为 zerolog
- [ ] 支持双格式：人类可读（默认）/ JSON（`--log-format json`）
- [ ] 支持日志级别：debug / info / warn / error
- [ ] `--log-file <path>` flag 写入文件

#### 步骤 2：Prometheus Metrics（1.5 人天）
- [ ] 引入 `github.com/prometheus/client_golang`
- [ ] 新建 [internal/metrics/metrics.go](internal/metrics/metrics.go)
- [ ] 指标：
  - `fgqimen_targets_total` (counter)
  - `fgqimen_alive_targets` (gauge)
  - `fgqimen_open_ports_total` (counter)
  - `fgqimen_identify_duration_seconds` (histogram)
  - `fgqimen_credential_attempts_total` (counter)
  - `fgqimen_credential_hits_total` (counter)
  - `fgqimen_active_workers` (gauge)
- [ ] 添加 `--metrics-addr :9090` flag（默认禁用）
- [ ] 添加 `/metrics` HTTP 端点

#### 步骤 3：增强 TUI 统计（1 人天）
- [ ] [internal/core/scanner.go:199](internal/core/scanner.go) `pushStats()` 增加：
  - 当前速率（pps / target-per-sec）
  - 凭据命中率
  - 内存使用（`runtime.MemStats`）
  - 协程数
- [ ] TUI 增加 "DETAIL" 面板（按 `d` 切换）

### 11.4 验收标准
- `--log-format json` 输出 JSON Lines
- 启用 `--metrics-addr :9090` 后 `curl /metrics` 返回 Prometheus 格式
- TUI 详细面板显示上述指标

---

## 12. 实施路线图

### 12.1 v0.3.0（6 周，~30 人天）

| 周 | 任务 | 工时 |
|---|---|---|
| 1-3 | 主题一（测试补齐）：proxy 0%→80%、scan 45→75%、messaging 18→70% | 15 |
| 1 | 主题四-1（bbolt 加密）+ 主题四-3（运行时上限） | 4 |
| 2-3 | 主题二-1（cmd/scan.go 拆分）+ 主题二-3（main.go 改进） | 4 |
| 3-4 | 主题三-2（令牌桶限速）+ 主题三-4（benchmark） | 3 |
| 4-5 | 主题六（DNS/NetBIOS-NS/MQTT 协议识别） | 4 |
| 5-6 | 主题八-1（golangci-lint）+ 主题八-2（GoReleaser） | 2 |
| 6 | 主题五-1（CSV 输出）+ 主题五-2（flag 分组） | 2 |

**v0.3.0 验收**：
- 测试覆盖率 ≥ 65%
- bbolt 加密可用
- golangci-lint 通过
- DNS/NetBIOS-NS/MQTT 识别可用
- 限速 flag 可用

### 12.2 v0.4.0（8 周，~25 人天）

| 周 | 任务 | 工时 |
|---|---|---|
| 1-3 | 主题一收尾（adapted 插件补全到 60%） | 8 |
| 1-2 | 主题三-1（自适应线程池） | 3 |
| 3-4 | 主题二-2（合并 adapted + auth 为单包） | 3 |
| 4-5 | 主题五-3（i18n 中英文） | 3 |
| 5-6 | 主题五-4（错误码系统） | 2 |
| 6-7 | 主题七（godoc + examples + CHANGELOG + CONTRIBUTING） | 4 |
| 7-8 | 主题八-3/4/5（Dependabot + CodeQL + changelog 自动生成） | 2 |

**v0.4.0 验收**：
- adapted 插件识别路径全部 ≥ 60%
- 自适应线程池 + 限速默认开
- 中英文切换正常
- 错误码稳定
- API 文档自动生成

### 12.3 v0.5.0（8 周，~20 人天）

| 周 | 任务 | 工时 |
|---|---|---|
| 1-4 | 主题九-1（`pkg/fgqimen` SDK） | 4 |
| 1-3 | 主题九-2（Config File） | 2 |
| 4-6 | 主题十-1（结构化日志）+ 主题十-2（Prometheus metrics） | 3 |
| 5-7 | 主题六收尾（NTP/UPnP/CoAP 等低风险协议） | 2 |
| 6-8 | 主题二-4（scheduler.go 拆分）+ 主题五-3 收尾 | 2 |
| 7-8 | 主题三-3（OOM 防护） | 1 |

**v0.5.0 验收**：
- SDK 可被外部项目引用
- 配置文件加载正确
- Prometheus 指标可导出
- 协议总数 ≥ 35

### 12.4 v1.0.0（12 周，~15 人天）

- 主题十-3（TUI 详细面板）
- 全面集成测试（E2E）
- API 稳定性承诺（`pkg/fgqimen` 语义化版本）
- 安全审计 + 第三方渗透测试
- 完整文档站（基于 [Docusaurus](https://docusaurus.io/) 或类似）

---

## 13. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| bbolt 加密升级路径复杂 | 用户项目文件不可读 | 自动检测 + `--migrate-encrypt` 命令 |
| 内部包重构（adapted + auth 合并） | 大量 import 更新 | 提供 `gofmt -r 'a.b.c.X -> a.d.X' -w .` 脚本辅助 |
| i18n 增加二进制体积 | 5 OS 矩阵 × UPX 仍可接受 | 默认仅嵌入 en，zh 按需下载 |
| 自适应线程池波动影响稳定性 | 用户感知卡顿 | 默认保守恢复（10% 而非 25%） |
| 配置文件格式变更 | 用户配置不可用 | YAML 顶层加 `version: 1` 字段，CI 检测兼容性 |
| SDK 公开导致 API 承诺 | 后续重构困难 | `pkg/fgqimen` 与 `internal/` 严格分离，semver 承诺 |
| CodeQL 误报多 | 维护成本 | 调低默认 query 套件，仅 security-extended |

---

## 14. 成功指标

### 14.1 质量指标

| 指标 | v0.2.0 当前 | v0.3.0 目标 | v0.5.0 目标 | v1.0.0 目标 |
|---|---|---|---|---|
| 测试覆盖率（总） | ~35% | ≥ 65% | ≥ 75% | ≥ 85% |
| adapted 插件识别路径覆盖 | 8% | ≥ 50% | ≥ 70% | ≥ 85% |
| golangci-lint 警告 | 0（未启用） | 0 | 0 | 0 |
| 圈复杂度 > 80 函数 | 0 | 0 | 0 | 0 |
| `gofmt -s -l` 不合规 | 0 | 0 | 0 | 0 |

### 14.2 性能指标

| 指标 | v0.2.0 当前 | v0.3.0 目标 | v0.5.0 目标 |
|---|---|---|---|
| 100K 端口扫描耗时（局域网） | 未测 | < 60s | < 45s |
| 内存峰值（100K 端口） | 未测 | < 500MB | < 350MB |
| 1K 凭据 × 100 目标爆破 | 未测 | < 120s | < 90s |

### 14.3 安全指标

| 指标 | v0.2.0 | v0.3.0 目标 | v0.5.0 目标 |
|---|---|---|---|
| bbolt 凭据加密 | ❌ | ✅ | ✅ |
| 凭据文件权限 0o600 | ❌ | ✅ | ✅ |
| 运行时上限 | ❌ | ✅ | ✅ |
| 默认 redact | ✅ | ✅ | ✅ |
| CodeQL 高危告警 | 0 | 0 | 0 |
| gosec 高危告警 | 0（未启用） | 0 | 0 |

### 14.4 UX 指标

| 指标 | v0.2.0 | v0.3.0 目标 | v0.5.0 目标 |
|---|---|---|---|
| 输出格式数 | 4（TXT/NDJSON/creds/RDP） | 5（+CSV） | 5 |
| 语言数 | 1（en） | 1 | 2（+zh） |
| 错误码稳定数 | 0 | ≥ 10 | ≥ 30 |
| Prometheus 指标数 | 0 | 0 | ≥ 7 |

### 14.5 生态指标

| 指标 | v0.2.0 | v0.3.0 目标 | v0.5.0 目标 |
|---|---|---|---|
| 协议识别插件数 | 26 | 29 | 35+ |
| 公开 SDK | ❌ | ❌ | ✅（`pkg/fgqimen`） |
| Config File | ❌ | ❌ | ✅ |
| godoc 完整度 | 60% | 80% | 95% |
| examples/ 数 | 0 | 0 | 3+ |
| CHANGELOG.md | ❌ | ✅ | ✅ |

---

## 15. 附录 A：依赖新增清单

| 依赖 | 用途 | 引入主题 | 是否已存在 |
|---|---|---|---|
| `github.com/nicksnyder/go-i18n/v2` | i18n 中英文 | 五-3 | 否（fscan 有） |
| `gopkg.in/yaml.v3` | 配置文件解析 | 九-2 | 否 |
| `github.com/rs/zerolog` | 结构化日志 | 十-1 | 否 |
| `github.com/prometheus/client_golang` | 指标导出 | 十-2 | 否 |
| `golang.org/x/time` | 令牌桶限速 | 三-2 | 是（间接） |

**总新增直接依赖**：4 个
**间接依赖增加**：~15-20 个（i18n、yaml、zerolog、prometheus 的传递依赖）

**Go 模块大小预估变化**：~2-3 MB（go.sum 增量）

---

## 16. 附录 B：不变更的决策

| 项 | 不变更原因 |
|---|---|
| 漏洞利用方向 | 用户明确排除 |
| 内部包分层（`internal/core/credential/auth`） | 重构 ROI 低，价值在合并 adapted 后消失 |
| Bubbletea TUI 主交互 | 是产品差异化，不替换 |
| bbolt 作为存储后端 | 性能足够，加密层即可解决安全顾虑 |
| garble + UPX 硬化 | 是反静态分析最佳实践，保留 |
| pipeline 4 阶段架构 | 已经是行业最佳实践，不动 |
| 插件 init() 注册模式 | 比 build tag 灵活，不切换 |

---

## 17. 附录 C：参考实现链接

| 主题 | 参考 |
|---|---|
| 自适应线程池 | [fscan core/adaptive_pool.go](D:/Go/fscan-main/core/adaptive_pool.go) |
| 限速（令牌桶） | [fscan common/globals.go:115-141](D:/Go/fscan-main/common/globals.go) |
| SDK API | [fscan pkg/fscan/scanner.go](D:/Go/fscan-main/pkg/fscan/scanner.go) |
| i18n | [fscan common/i18n/](D:/Go/fscan-main/common/i18n/) |
| 输出 CSV | [fscan common/output/writers.go](D:/Go/fscan-main/common/output/writers.go) |
| GoReleaser 配置 | [fscan .github/conf/.goreleaser.yml](D:/Go/fscan-main/.github/conf/.goreleaser.yml) |
| golangci-lint 配置 | [fscan .golangci.yml](D:/Go/fscan-main/.golangci.yml) |
| Build Tag 选择性裁剪 | [fscan plugins/init.go](D:/Go/fscan-main/plugins/init.go) |
| 凭据调度 | [fscan plugins/services/credential_tester.go](D:/Go/fscan-main/plugins/services/credential_tester.go) |
| 配置管理 | [fscan common/initialize.go](D:/Go/fscan-main/common/initialize.go) |

---

**方案结束**。本方案约 17,000 字，覆盖 10 大优化主题，分 4 个版本（v0.3/v0.4/v0.5/v1.0）实施，总估时 ~62 人天。所有改动均可独立 PR、可灰度发布。
