> [English version](CHANGELOG.md)


# Changelog
## [0.8.2] - 2026-09-13

### Fixed

- **43 个插件子包从未在生产二进制中注册** —— 类目包（`database/`、
  `email/`、`filestorage/`、`messaging/`、`network/`、`remote/`、
  `cloud/`，以及 `web/` 的 jenkins/kibana/weblogic）是空占位文件，
  注释声称上层 `adapted` 包直接 import 了各插件子目录——实际没有。
  postgresql、mysql、mssql、oracle、mongodb、elasticsearch、redis、
  memcached、ssh、telnet、vnc、winrm、ipmi、rdp、rdpnla、smb、nfs、
  rsync、ftp、smtp、pop3、imap、rabbitmq、kafka、mqtt、activemq、
  rocketmq、snmp、snmpv3、ldap、docker、socks5、modbus、bacnet、
  dns、ntp、tftp、aws、azure 及 web 三件只在单测里运行过——真实
  扫描的服务识别完全由 nmap 指纹层、UDP 指纹与凭据测试器承担。
  现在每个类目 doc.go blank-import 全部子包，adapted 根 import
  cloud，两个闭包守卫测试（`TestAggregationImportsAllSubpackages`、
  `TestRootBinaryImportsAdapted`）在任何插件包被漏出聚合链时让
  CI 变红。
- **每个帮助页渲染错误** —— 自定义 usage 模板在 cobra 自带 help
  模板已打印 `{{.Long}}` 的情况下又打了一遍，所有描述出现两次；
  模板还丢了 cobra 的 Available Commands 段，
  scan/projects/resume/schedules/version 在 `--help` 里隐身；Flag
  Groups 摘要为手写，早已与 flags.go 脱节（缺 Schedule 行、缺
  `http-form-*`、缺 `rotate-*`、缺 `web-fingerprint`、缺
  `no-batch`/`no-prescreen`）。帮助模板已修复；分组摘要改为
  Execute() 时从 flag 分组注解生成——新增一个 flag 加一行
  `annotate()` 即可出现在摘要里。

### Added

- **每条身份断言带证据链：`fp_probe` / `fp_pattern` NDJSON 字段 +
  `schema` 版本戳** —— nmap 风格 banner 指纹现在记录每条
  service/product/version 断言的来源：哪个探针（如 `GetRequest`）、
  哪条规则文本，硬匹配与 softmatch 路径、TCP 与 UDP 一视同仁。
  NDJSON 记录携带 `schema: 1`，下游消费方可钉住输出契约随演进
  不兼容。未知服务的输出保持逐字节一致（新字段全部 `omitempty`）。
- **识别覆盖率汇总：`identified=X/Y (hard=.. soft=.. unknown=..)`**
  —— 扫描结束行现在量化 Y 个开放端口中有多少带身份断言、断言有
  多强（nmap 硬匹配或插件协议握手 vs softmatch 提示）。banner 为空
  的端口被插件握手认领后即从 unknown 桶搬出；三桶划分按构造精确，
  Y−X 的未知尾部就是下次扫描的可行动目标清单（"identify, not just
  connect"——度量交付，而非承诺）。
- **指纹质量度量 harness（`just golden`）+ 复活 645 条 lookaround
  死规则** —— 以合成/RFC 公开样本构建 golden 数据集，度量识别质量
  （hard 命中率、识别率、零误报），并以垃圾字节扫描防守 regex
  意外（256 条噪声输入，零硬命中）。上游 nmap-service-probes HTTP
  规则中 RE2 不兼容的 PCRE 前瞻写法
  `(?:[^\r\n]*\r\n(?!\r\n))*?` 在编译期被机械翻译为 RE2 安全的
  header 循环，12,155 条规则的存活率从 94.3% 升至 99.6%。

### Docs

- **README 定位重写**（双语，结构镜像）—— tagline 先行的引言、
  为何不做攻击的理由（告警噪声、打坏目标风险、低价值 exploit、
  决策级交付物）、双姿态框架、三承诺结构。统计条带改为渲染实时
  注册表计数（44 插件 / 27 支持凭据 / 3333 条 web 指纹规则 /
  84 条 UDP 探针），由 gendocs 生成，CI 守卫测试
  （`TestREADMEStatsUpToDate`）把两份 README 钉在注册表上——
  一条规则，计数零漂移。
- **双语文档审计** —— gendocs 确立为生成式 flag/插件表的唯一
  真相源，修复断裂交叉链接，历史材料移入 `docs/archive/`；
  找回 v0.5.0–v0.5.1 丢失的 CHANGELOG 条目，拆分归档错位的
  v0.6.0 条目。

## [0.8.1] - 2026-09-13

### Removed

- **删除 `homebrew-tap.yml` workflow** —— 其一次性前置（同级
  `homebrew-tap` 仓库与 `HOMEBREW_TAP_TOKEN` secret）从未配置，
  该 workflow 每次 release 都必然失败；渠道从未工作过，也无人
  问津。与 scoop-bucket 同一 YAGNI 结论。若需要 Homebrew 分发，
  从 git 历史找回再加回。

### Fixed

- **webtitle 插件从未在生产二进制中注册** ——
  `internal/plugins/adapted/web/http.go`（聚合注册点）只 import 了
  基础 `http` 插件；`webtitle` 深度 HTTP 指纹插件的 `init()` 从未
  在单测之外运行，所以尽管插件本体、3139 条 FingerprintHub 规则库
  与 favicon 匹配器全都随包发布，任何扫描都不可能产出 webtitle
  命中。blank import 已补上，实机冒烟确认 webtitle 正常产出。
- `internal/version` 的默认值钉死测试未随 v0.8.0 发版 bump 同步
  更新（发版清单遗漏）。

### Added

- **结构化 Web 指纹输出：`fgqm_web.json` / `fgqm_web.txt`** ——
  webtitle 每次命中现在都在 `Result.Extra` 携带
  `types.WebFingerprint` payload（URL、状态码、标题、Server、命中
  指纹），由管线 sink 经 `output.WriteWeb` 双写（与 RDP 相同的
  `Extra` 旁路模式）。为 SIEM / 自动化消费方提供机器可读的 Web
  侦察数据。
- **https 目标的 TLS 叶子证书身份** —— subject、SAN DNS 名、
  issuer、有效期与协议版本，从 `resp.TLS` 零额外连接收割，进入
  Web 指纹 payload。SAN/CN 字段常能暴露 banner 匹配看不到的内网
  机器名与域名。无 CN（SAN-only）证书回退到序列化 RDN 串。

## [0.8.0] - 2026-09-13

### Added

- **UDP 服务探针（`--udp`）** —— TCP 扫描之后的可选 UDP 阶段：常见
  UDP 端口（DNS、NetBIOS、SNMP、NTP、memcached……）用
  nmap-service-probes 的 UDP payload 探测（转义解码；同一端口的全部
  payload 在一条 connected socket 上写完再单次读），任何响应字节走
  UDP 规则集指纹识别，产出与 TCP 一致的
  `product`/`version`/`confidence` 结构化身份。端口集合：显式
  `--ports` ∩ probe 提示端口，否则用全部提示端口（约 70 个）；
  `--exclude-ports` 照常生效。UDP 池硬上限（128/200 线程、2s 探测
  超时），且在 TCP 扫描之后串行跑，不会扰动 TCP 自适应池。UDP item
  跳过 TCP 握手类插件循环；banner 显示把二进制字节收敛为 `.`（nmap
  惯例）。

- **RTT 采样的自适应探测超时** —— 池现在会在扫描过程中持续精修超时
  （借鉴 fscan 的 mean+4σ）：每个真正到达主机的探测（握手成功、被拒
  RST）把 RTT 记入 64 样本环形缓冲，单 probe 超时变为 `mean + 4σ`，
  clamp 到 `[max(500ms, base/5), base]`。快速局域网上 3s 的 filtered
  端口等待收缩到约 600ms（扫速 ≈5×）；慢路径的上限仍保持操作员的
  （可能经环境画像调优的）超时。静默 UDP 探测报 `RTT=0`，永不喂入
  采样器。操作员显式设置 `--timeout` 时整体禁用（与环境画像同一
  isExplicit 约定），且无论开关都 clamp 在 base 之内——显式超时
  始终是硬上限。TCP 与 UDP 两个池均已接线。

- **`--udp-strict`** —— 改变 UDP 静默裁决：预算内没有任何应答的
  端口报 filtered 并被插件消费方丢弃，而非 open|filtered 的 Open
  约定（在防火墙网段上每个 host×port 吐一条噪声结果）。用"漏掉
  空闲但开放的服务"换干净的结果流；默认（非 strict）语义不变。

- **借鉴 fscan 的扫描智能化** —— 主机排除（`--exclude-hosts` /
  `--exclude-hosts-file`：精确 IP、CIDR、范围、主机名、RFC1918 快捷
  `192`/`172`/`10`，在任何探测流量之前生效）、网络环境画像（扫描前
  对目标集采样 RTT/丢包率，自动调优超时与线程数——仅对操作员**未**
  显式设置的值生效）、/24 网段预筛（两阶段网关探测 + 针对防火墙
  网关的有界逐主机兜底；单网段输入永不过滤）。资源耗尽类拨号错误
  （EMFILE 等）现在按指数退避重试，重试失败率 >20% 时告警提示
  扫描大概率处于 FD/套接字配额不足状态。
- **每条结果携带结构化服务身份** —— `Result` 新增 `product`、
  `version`、`confidence` 字段。nmap 风格 banner 指纹现在解析
  `p/product/ v/version/` 模板分段（含 `$1`-`$9` 子匹配展开），不再
  原样吐出整串；硬匹配标 `high`、softmatch 兜底标 `low`；插件
  Identify（真协议握手）标 `high`。NDJSON 自动带出新字段（未知时
  省略，未知服务的记录与既往逐字节一致）；results.csv 在**末尾**
  追加 `product`/`version`/`confidence` 三列，原 9 列位置不变；txt
  banner 行现在显示 `ssh | OpenSSH 8.9p1 | banner=...`，替代原始
  `p/.../ v/.../` 模板。
- **每次扫描的日志归档** —— 每次扫描现在会把日志行写入结果文件
  旁的 `fgqm_log_HH-MM-SS.txt`（同日分桶 + 同时间戳）。文本模式
  （`--no-tui`）控制台与文件同步输出（tee）；TUI 模式与 `--silent`
  仅写文件——dashboard 保持干净，日志不再被丢弃。文件无缓冲
  （每条日志都能在硬退出下存活），并以 `0600` 权限创建（凭据
  命中行含明文口令）。日志文件打开失败降级为原行为并 stderr
  警告——绝不因此中止扫描。
- **`just smoke <CIDR>`** —— 人工实机 TUI /24 冒烟测试固化为可执行
  recipe（包装 `smoke` 标签的 `TestSmokeLiveTUIOnTarget` 探针，自动
  设置颜色/终端环境；第二个可选参数经 `FGQI_SMOKE_MAX` 限定扫描
  预算）。捕获流导出到临时目录供人工复核。
- **`just test-short`** —— 快速测试通道。`go test -short` 跳过依赖
  网络的插件冒烟探测（UDP 插件连关闭端口时没有 RST 可快速失败，
  只能等满自身约 3 秒的内部 deadline，全量运行时每个 UDP 插件包
  至少 3 秒）。fake-server 协议测试照常运行。单包迭代从 ≥3 秒降到
  亚秒级；全量 wall-clock 收益随核数伸缩（包是并行的）。

### Changed

- **分层：RDPFingerprint 从 output 下沉到 types** —— rdp 插件此前
  import internal/output 只为把 `*output.RDPFingerprint` 塞进
  `Result.Extra` 供管线 sink downcast（plugins → output 倒置）。
  结构体现在移至 `internal/types`（`types.RDPFingerprint`）：插件
  生产、sink 分发、output 只渲染。字段与 JSON tag 不变——rdp.json /
  rdp.txt 输出逐字节一致。
- **`types.ProtocolTCP` / `types.ProtocolUDP` 常量** —— 跨越
  scan→plugin 边界的 `"tcp"` / `"udp"` 魔法字符串（`ScanItem.
  Protocol` 的生产方与分派方）改用共享常量。
- **AIMD 自适应并发池（借鉴 fscan 的 AdaptivePool）** —— 扫描池原本
  "只增不减"的 open 比例启发式替换为两阶段控制器，由无锁扫描度量
  驱动：慢启动（以 `--threads` 目标的 1/4 出生，健康周期内逐周期
  翻倍）→ 稳态 AIMD——健康时加性增（+target/20），有压力 ×0.85，
  拥塞 ×0.5。健康每 500ms 评估一次，信号为资源耗尽率（穿透
  RetryableProbe 的 EMFILE 类拨号错误）与 fast/slow 双 EMA RTT 趋势
  （fast α=1/10、slow α=1/50；只有真正到达主机的探测才喂入），并按
  环境画像接入 LAN/WAN/Internet 三档阈值。雪崩教训由测试钉死：
  filtered 占比高的窗口绝不读作过载（timeout 只稀释耗尽率分母），
  纯静默不携带网络真值——超出环境调优目标的增长必须有真实响应。
  RTT 比值持续 >3 时经单向棘轮压低 AIMD target（下限
  MaxThreads/5）。
- **`--threads` 显式指定时成为硬上限** —— 此前显式值在响应良好的
  网络上仍会被涨到内置的 500 线程上限；AIMD 控制器现在把显式
  `--threads` 视为天花板，绝不超出。环境画像只在未显式指定时自动
  调优。
- **fingerprint：softmatch 降级为低置信度猜测** —— 当没有任何硬
  规则命中 banner 时，首个 soft 命中现按 nmap 惯例报为 `service?`
  且不带版本信息，而非看似权威的匹配。soft 正则是宽松的协议提示，
  不是指纹。

### Removed

- **container.yml + Dockerfile** —— OCI 镜像通道删除。该工作流自
  v0.3.3 起每个 tag 都失败，从未产出过一个镜像（根因：ghcr.io 拒绝
  含大写字母的镜像引用，而本仓库 owner 是 `LCUstinian`）；没人发现
  是因为没人用它。网络扫描器进容器本来就是残废——Docker NAT 破坏
  ARP/主机网段扫描和 UDP/组播插件。与 scoop-bucket 删除同一 YAGNI
  判决。

### Fixed

- **scan：UDP 响应不再过 `trimASCII`** —— DNS/SNMP/NBTStat 响应是
  二进制的，空格替换会毁掉全部不可打印字节，二进制锚定的 UDP 指纹
  规则永远无法匹配。匹配保留原始字节；显示路径自行收敛（控制/高位
  字节折叠为 `.`）。
- **scan：banner 抓取从未接入生产扫描** —— `core.NewScanner` 构造
  `TCPConnectProbe` 时没挂 `BannerReader`，每个开放端口的 banner 恒
  为空，Stage-0 的 nmap 风格指纹层在真实扫描中是死代码（只在单元
  测试里跑过）。生产路径现在用 `scan.FirstBanner` 拨号（每个开放
  端口 256 字节 / 200ms 预算）。
- **scan：banner 去尾破坏所有尾部锚定的指纹规则** —— `readBanner`
  把 CR/LF 替换成空格再去尾，锚定在行尾换行的规则（OpenSSH
  `...\r?\n` 家族等几十条）永远无法硬匹配：真实 sshd 被报成
  `ssh?` 而非 `ssh`。banner 现在为匹配保留真实 CR/LF；显示路径自行
  折叠内部换行与尾部空白（TXT/CSV/NDJSON 保持单行）。
- **fingerprint：垃圾 banner 上的幽灵服务（dps-shell 类）** ——
  pattern 编译器此前把字节转义（`\x7c` 等）解码成裸字节，且仅对
  不可打印字节重新转义，于是解码出的 0x7c 变成了活的 `|` 正则
  "或"分支。每个含 `\x7c` 协议字段分隔符的 pattern 都被静默切成
  多分支——如 jrpgt 的 `^<<jrpgt!>>\x7c$` 变成 `^<<jrpgt!>>` | `$`，
  裸 `$` 分支匹配任意 banner（垃圾字节实验：3000 个伪随机噪声命中
  2972 次）。现在 pattern 以转义保真的形式（`\x{7c}`）交给 Go
  regexp，整类误报消除。另外，空洞的 `nagios-nsca` 规则
  （`^.{128}[\x52-\x7F]...$`——任何 ≥132 字节且第 129 字节落在
  0x52..0x7F 的 banner 都命中）经证据拉黑在解析期丢弃；两项修复后
  噪声 banner 实验的硬匹配误报为零。
- **CI：homebrew-tap 不再与 release 构建赛跑** —— tap 工作流在
  11 平台 release 还在传产物时就去 fetch SHA256SUMS（v0.7.1 上 404）。
  现改为 `workflow_run: [release]` 触发 + 成功门槛，SHA256SUMS 获取
  重试 5 次、间隔 30 秒。
- **CI：main 上 golangci-lint 报红** —— 五处 gofmt（flags、
  modbus_test、snmpv3）加一处 errorlint（modbus 改用 `errors.Is`
  判断 `io.ErrUnexpectedEOF`）。问题自 v0.6.1 起就存在；release
  工作流不跑 lint，所以 v0.7.1 是带着红 CI 发出去的。

## [0.7.1] - 2026-09-12

安全加固与审计闭环版本。全项目审计（P0/P1/P2）的所有发现在本版全部
处理；唯一延期项是插件接口演进（A-1/A-4，按设计留待接口下次变更时
一并处理）。

### Security

- **webtitle：远程响应体限界读取** —— web 指纹路径对响应体加 1 MB
  上限（`io.LimitReader`），主识别与重定向两条路径均覆盖。此前恶意
  服务器流式返回无限大响应，可在数百并发探测下把扫描器 OOM。
- **output：CSV 公式注入中和** —— 以 `=` `+` `-` `@` `\t` `\r` 开头
  的 banner 单元格加 `'` 前缀（OWASP CSV Injection）。`user`/`pass`
  列保持原样，操作员可直接复制凭据。
- **CI：全部第三方 action pin 到完整 commit SHA** —— 32 处 `uses:`
  经 GitHub API 解析（含可变 ref `ludeeus/action-shellcheck@master`）。
  tag 劫持不再能触及 release workflow 的 secrets。
- **CI：ci.yml 补 `permissions: {contents: read}`** —— 此前唯一缺
  最小权限块的 workflow；checkout token 不再默认带写权限。
- **fingerprint：自定义规则集加载护栏** —— 16 MiB 字节上限现同样
  适用于本地文件，另加 1 万条规则数上限（两种格式均生效）。（说明：
  Go 的 RE2 语义正则不存在灾难性回溯，护栏针对的是 O(规则数 × body)
  的匹配开销。）

### Added

- **`fg-qimen projects prune <name> --before <date> [--compact] [--yes]`**
  —— 长期项目的保留策略：删除早于截止时间的 seen-hash 条目（results/
  creds 永不触碰），先预览条数，非交互无 `--yes` 拒绝执行，
  `--compact` 经临时文件换盘重写 `fgqm.db` 回收磁盘。

### Removed

- **`credential.Scheduler`** —— 并行凭据派发实现（节流 + HitSink）
  生产零调用；`core.dispatchCred` 是唯一派发路径。删除同时关闭审计
  中的超时耦合发现（M-4）——它只存在于死代码中。

### Changed

- **cmd：`scan.go` 拆分** 为 `scan_lifecycle.go`（session 装配、TUI
  拆除、硬退出 sink flush）与 `scan_outputs.go`（日桶、时间戳、cwd
  沙箱）。纯代码移动。
- **perf：NDJSON `json.Encoder` 提升为字段**，与既有 `csvWriter`
  模式一致——每结果行少一次分配。
- **docs：`main.go` 包注释** 现指向真正的双语 `docs/ARCHITECTURE.md`
  （此前声称架构文档在 THIRD_PARTY_LICENSES.md，该文件从未有过）。

### Fixed

- **tui：标题栏裁剪与冒烟探测修复**（Spec B+C /24 冒烟后续）。
- **version 测试** 期望值与发版版本同步。

## [0.7.0] - 2026-09-12

### BREAKING — 工作区目录改名

on-disk 工作区布局从 `runs/` 改名为 `fgqm_workspace/`，与所有 fg-qimen
结果文件（`fgqm_result.txt`、`fgqm_creds.txt`、`fgqm_alive.txt`、
`fgqm_rdp.*`）的 `fgqm_` 前缀统一。bbolt 状态文件相应从 `fg.db`
改名为 `fgqm.db`，理由相同。

| 旧（≤ v0.6.0） | 新（v0.7.0） |
|---|---|
| `runs/default/<YYYY-MM-DD>/fgqm_*` | `fgqm_workspace/default/<YYYY-MM-DD>/fgqm_*` |
| `runs/projects/<name>/fg.db` | `fgqm_workspace/projects/<name>/fgqm.db` |
| `runs/projects/<name>/<YYYY-MM-DD>/fgqm_*` | `fgqm_workspace/projects/<name>/<YYYY-MM-DD>/fgqm_*` |

**迁移：**

```bash
# 一次性重命名现有工作区树。安全——树内文件无需改，仅父目录名变。
mv runs fgqm_workspace

# 项目目录内 fg.db → fgqm.db（bbolt 允许重命名，只要内容不变）。
find fgqm_workspace/projects -name 'fg.db' -exec mv {} {}.tmp \; -exec mv {}.tmp "$(dirname {})/fgqm.db" \;
```

迁移完成后，原 `fg-qimen resume --project <name>` 会从
`fgqm_workspace/projects/<name>/fgqm.db` 恢复，无需重扫或重建状态。

**不**提供 `--workspace-root` 兼容 flag——硬切。在单台主机上想保留
旧路径可以建符号链接：`ln -s fgqm_workspace runs`（仅向下兼容）。

### Added

- **双版本发布流水线**（.github/workflows/release.yml、scripts/harden.sh、
  scripts/harden.ps1、scripts/strip_upx.py）。每个 GitHub Release 同时
  发布两个版本：标准版（可复现，SOURCE_DATE_EPOCH 固定，11 个平台）和
  加固版（仅 linux-amd64 + windows-amd64；garble `-seed=random` 混淆、
  UPX `--best --lzma` 压缩、经 scripts/strip_upx.py 消除 UPX 特征）。
  加固版产物带 `-hardened` 后缀（fg-qimen-linux-amd64-hardened、
  fg-qimen-windows-amd64-hardened.exe），刻意不可复现（每次构建
  SHA256 都不同），但与标准版同样获得 cosign 签名 / 逐二进制 SBOM /
  SLSA 证明。本地用 scripts/harden.sh（Linux/macOS）与 scripts/harden.ps1
  （Windows；harden.bat 是薄包装）复现同一管线，版本号从
  internal/version/version.go 自动推导；garble 固定在 v0.17.0 与
  CI 一致。

### Changed

- TUI v2 Spec B (panel layout) + Spec C (visual polish): 3-breakpoint responsive layout (narrow/medium/wide); LIVE EVENTS panel with severity-coloured ring buffer; rate sparkline in header; collapsible ERRORS panel (e/E); single dark theme with severity colours; progress bars for alive/ports; status symbols + 200ms hit flash. See docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md.

### Fixed

- TUI layout hardening (probe-driven): the footer hint line was never
  truncated to the terminal width, so `JoinVertical` padded every
  region to its 89-col width and the whole frame wrapped on ≤89-col
  terminals (18 of 20 lines overflowed at 80×24). Footer / collapsed
  ERRORS / events rows are now width-clamped and the keymap descs
  shortened (`toggle errors panel` → `errors panel`).
- TUI frame height: `regions()` under-counted the chrome rows (title
  bar 2, header rate line), so a full events panel pushed the frame
  past the terminal on 80×24. Regions now reserve the real chrome;
  `View()` clamps the events budget to the measured remainder and
  reconciles height (pads short frames, truncates overframes as a
  last resort). Pinned by `TestViewFrameFitsTerminal` across
  breakpoints, paused included.
- The `e` toggle flipped `errorsExpanded` but the dashboard never
  rendered the expanded panel (`regions()` always budgeted 1 row;
  expansion needs ≥4). `View()` now widens the budget to 4 rows when
  expanded. The `E` keymap desc matches its collapse-only behavior
  ("collapse errors", was "clear errors").
- Help overlay lists the new `e`/`E`/`L` keys; the collapsed ERRORS
  line is indented + dim like the other regions; the TOP PLUGINS
  panel no longer injects stray blank lines into the composition.

## [0.6.0] - 2026-09-10

Fake-server 覆盖推进。35 个 adapted plugin 中的 35 个拿到了 in-process
fake-server 测试（通过新 `internal/fakeserver/` 共享包）；两个 plugin
（modbus、snmpv3）以较低 per-plugin 覆盖率 ship，文档化 v0.6.1 follow-up
（按 plan §11.2 的"复杂协议"例外）。项目总覆盖率 60.5% → 70.8%（+10.3 pp）。
Fake-server 开发过程中揭出 mssql plugin 真 bug（`server=<addr>;port=<port>`
DSN 永远不能被 go-mssqldb 的 `tcpParser` 解析），commit `45a19b3` 已修。
CI 门槛提升：全局覆盖率地板 60% → 70%，`scripts/ci-coverage-check.py`
新增 per-plugin 60% walk。

### Added

- **`internal/fakeserver/`**（`fakeserver.go`、`tcp.go`、`udp.go`、
  `http.go`、`bin.go`、`doc.go`、`fakeserver_test.go`）。共享的进程内
  fake-server helper：`ListenLoop`（TCP）、`ListenUDPLoop`（UDP）、
  `StartHTTP`（HTTP via httptest）、`WriteMagic`（binary TLV builder）。
  每个 helper 绑 `127.0.0.1:0`（OS 自动分空闲端口）所以并发测试永不冲
  突；注册 `t.Cleanup` 关 listener 所以测试进程不泄漏。引入前每个 plugin
  测试要内联 ~20 行相同的 listener plumbing；集中后每个 plugin 测试只需
  "写协议 handler + 断言 Identify/Credential 返回"。

### Changed

- 覆盖率门槛 60% → 70%（scripts/ci-coverage-check.py）。
  v0.5.1 时代的 60% 地板被 30+ 0% 覆盖的 adapted plugin 卡
  住；上面的 fake-server 覆盖推进填上缺口，门槛相应抬升。
  同时引入 per-plugin 60% walk，让 CI 能抓住任何单 plugin
  包自己的回归。modbus（plugin 端 readFullMBP bug）放
  FLOOR_EXEMPT，跟踪为 v0.6.1 follow-up。
- **CI 卫生**：.gitattributes 锁 `*.go text eol=lf`，Windows checkout（core.autocrlf=true）不会再把源文件翻 CRLF 触发 gofmt -l。顺手解掉 internal/tui/ 里 4 个已有 golangci-lint 阻塞（Stage 比较的 truncateCmp、top-N slice 的 prealloc、renderErrorCategoriesRow 里多余的 ineffectual width、ETA docstring 注释续行对齐）。TestApplySchedule_WaitCronNoDaemon 的 minute-boundary flake 也修了：测试 cron 从 `*/1 * * * *`（每分钟）换成 `0 0 1 1 *`（每年），保证 1.2s ctx 超时永远先赢。
- **TUI 信息密度面板**（internal/tui/render.go、internal/tui/tui.go、internal/tui/styles.go、internal/types/state.go）。Header 行新增按阶段的 `[ ▶ STAGE ]` 徽章（ETA 右对齐）、扫描速率（hits/s 和 ports/s，EWMA 平滑）、每次渲染的预算。types.State 加 CountersView 投影，让视图层读稳定契约而不是改共享 map。ClassifyError（internal/core/errors.go）把扫描错误按 errors.Is/As 优先、子串 fallback 的方式路由到命名桶（timeout / refused / dns 等），TUI 底部以压缩汇总行展示 top categories。scanner 配套改造驱动新 Stage 枚举转移并填充 PluginHits / ErrorCategories。8 个单元测试 + 1 个契约测试钉住 rate EWMA、top-N 抽取、ETA 估算和 bar 尺寸。

### Fixed

- **TUI mid-alive-sweep 计数实时更新**（internal/core/alive/cmd.go、internal/core/alive/probe.go、internal/tui/tui.go、internal/types/state.go）。原来 header 的 "alive N/M" 在 alive 阶段完成前一直停在 0/M；现在随 probe 完成即时增长。alive.Progress() 是供外部调用方观察中途探测数的公共 API。
- **mssql plugin 真正修了一个 bug**（internal/plugins/adapted/database/mssql/mssql.go）。原 DSN `server=127.0.0.1:12345;port=...` 把端口塞进 server= 字段，go-mssqldb 的 tcpParser 不会剥离端口后缀，ParseIP 返 nil，dial 永远失败。改成 `server=127.0.0.1;port=12345;...`（host 和 port 拆成两个独立 DSN key）后驱动正确解析。Fake-server 测试在修前发现这个 bug——它不需要连真 mssql server 就暴露了"plugin 写错了"这个事实。

## [0.5.1] - 2026-09-04

v0.5.0 与 v0.6.0 之间打包发布的增量硬化：输出/文件/UX 硬化、
短参重构、`applySchedule` 测试补齐。

完整验证：`docs/verification/v0.5.1/verification.md`

### Added

- **默认 `fgqm_alive.txt` 存活主机列表 sink**（cmd/scan.go、
  internal/output/output.go、internal/output/output_test.go、
  README.md）。一行一个 IP，内存内经 `aliveMu` 去重，并发
  worker 不会重复写。路径为
  `runs/<default|projects/<name>>/<YYYY-MM-DD>/fgqm_alive_<HH-MM-SS>.txt`
  ——与其他带时间戳 sink 同一套日桶 + 时间戳方案。空 `Host`
  是 no-op（避免会产生弄坏 `nmap -iL` 的杂散空行）。6 个单测
  （2 路径 + 4 行为）钉住契约。
- **`applySchedule` 单元测试**（cmd/schedule_test.go，含
  daemon-loops 共 12 个 case）。v0.5 时该函数 0% 覆盖，现
  **100%**。覆盖 ModeNone 早返、9 个 Resolve 错误路径（--at
  格式错 / 过去、--in 格式错 / 0 / 负、--cron 格式错、
  --at+--in 互斥、--daemon 无 --cron、--tz 错）、3 种 mode
  的 dry-run、等未来时间、等 in 时长、daemon ctx 取消、
  cron 无 daemon、并发调用 sanity。互斥 case 用
  `errors.Is(..., scheduler.ErrInvalidCombination)` 钉死。
- **更多 cmd/ 测试**（cmd/cmd_test.go、cmd/schedule_test.go）。
  加 applyTransport（nil + flag 传递 + 空 KnownHosts 保护）、
  applyHTTPForm（空 + 填）、detectScheduleMode（4 种 mode +
  优先级）、loadScheduleTZ（空 / UTC / 非法 IANA 不 panic）
  测试。cmd/ 单元可测代码 59.6% → 64.4%。总覆盖率仍 ~60.5%，
  因 30+ adapted plugin 0% 覆盖——需要 fake-server 基础
  设施（v0.6 目标）。

### Changed

- **短参全面重构**（cmd/flags.go、cmd/multishort.go、
  cmd/multishort_test.go、cmd/{root,resume,scan,schedules}.go、
  internal/core/credential/pool.go、README*）。单字母短参
  全部小写 + mnemonic；2 字母短参用于命名空间 / 配对
  （output-* 和 user/pass-file，nmap `-oN/-oX/-oG/-oA`
  先例）；无语义的大写短参（`-M`、`-X`、`-U`、`-W`、`-P`）
  删除。**迁移表**（v0.5.0 → v0.5.1）：

  | 旧 | 新 |
  |---|---|
  | `-p corp` | `--project corp` |
  | `-M scan` | `--mode scan` |
  | `-X http://proxy:8080` | `--proxy http://proxy:8080` |
  | `-U users.txt` | `-uf users.txt` |
  | `-W pass.txt` | `-pf pass.txt` |
  | `-P admin,root` | `-p admin,root` |
  | `-o result.txt` | `-ot result.txt` |
  | `-j result.json` | `-oj result.json` |
  | (无) | `-oc result.csv` (新增) |
  | (无) | `-r` (`--resume` 短参新增) |

  无 deprecated alias 保留——硬切。实现备注：pflag v1.0.9
  在注册时拒绝多字母 shorthand 会 panic，所以 `-ot` / `-oj` /
  `-oc` / `-uf` / `-pf` 走 cmd/multishort.go 的 50 行预解析
  hook，在 cobra 看到 args 前改写为 `--output-txt` 等。
  flag-value 启发式（上一个 arg 是 flag 形态则跳过重写）
  确保字面密码如 `-p "-ot"` 通过长形式能正确往返。

- **结果文件按日分桶**（cmd/scan.go、cmd/cmd_test.go、
  cmd/flags.go）。默认结果路径现在带本地日期 YYYY-MM-DD 段，
  跨日扫描不会互相覆盖。即扫即走模式新布局（项目模式同形，
  在 runs/projects/<name>/ 下）：
  ```
  runs/default/2026-09-02/fgqm_result.txt
  runs/default/2026-09-02/fgqm_result.json
  runs/default/2026-09-02/fgqm_creds.txt
  runs/default/2026-09-02/fgqm_rdp.json
  runs/default/2026-09-02/fgqm_rdp.txt
  ```
  fg.db（持久化状态 / 去重 DB）保持在项目根，跨日共享；
  只有结果产物分桶。`-ot` / `-oj` / `-oc` / `--output-sarif`
  仍接显式路径，跳过分桶（操作员传这些就是要精确路径）。
  桶名在 scan 开始时一次性抓取，跨午夜扫描落到单一日桶，
  不会拆分结果。

- **同日多次 run 文件名加 HH-MM-SS 时间戳**（cmd/scan.go、
  cmd/cmd_test.go）。同日两次 run 现在产出不同文件名，不再
  互相覆盖。目录仍按 YYYY-MM-DD 分桶，时间戳打在文件名上
  （fgqm_result_14-30-22.txt 而非 fgqm_result.txt）。格式
  HH-MM-SS 本地时间（连字符分隔，兼容 Windows 文件名，且
  与 YYYY-MM-DD 风格一致）。`-ot` / `-oj` / `-oc` /
  `--output-sarif` 仍可跳过时间戳——操作员传显式路径就是
  要精确路径，不自动加缀。时间戳在 scan 开始时一次性抓取，
  单次 run 的所有 sink 共享同一后缀。

- **全部结果文件加 `fgqm_` 前缀**（cmd/scan.go、cmd/projects.go、
  cmd/flags.go、internal/output/*_test.go、README*、
  docs/ARCHITECTURE.md、docs/SECURITY.md）。七个默认结果
  文件名都带 `fgqm_` 前缀，混合目录里一眼能认出是 fg-qimen
  的产物。`targets.txt` 不加前缀，因为操作员手编。`-ot` /
  `-oj` / `-oc` / `--output-sarif` 仍可覆盖文件名（和路径），
  现有脚本管线传显式文件名继续可用。

- **覆盖率地板维持 60%**（scripts/ci-coverage-check.py）。
  原 A2 目标是 65%，但 30+ adapted plugin 0% 覆盖把总量拖
  到 60.5%——不投入 plugin fake-server 夹具（v0.6 工作）
  就到不了 65%。地板维持 60%，脚本 docstring 详述推迟原因。
  cmd/ 单元可测代码已 64.4%。（v0.6.0 抬到 70% 并引入
  per-plugin walk。）

- **6 字段 cron 表达式**（internal/scheduler/cron.go）。解析器
  从 cron.ParseStandard（5 字段）改为 cron.NewParser
  (SecondOptional | ...)，5 或 6 字段都支持。文档化的 5
  字段形式（`0 9 * * *` 等）仍可用；6 字段（`* * * * * *`
  = 每秒）现在合法，用于快速测试和短间隔 daemon 任务。

- **二进制内嵌 time/tzdata**（main.go）。二进制 +~400 KB
  （压缩后）让 `--tz` 在精简容器镜像（没 /usr/share/zoneinfo）
  上也能工作。少了这个，系统 tz DB 缺失会静默回退
  time.Local（很多最小容器是 UTC 偏移 0）→ cron 触发时间
  静默错。默认开启，消除"我机器行 CI 挂"的尴尬。

### Fixed

- **硬退出丢结果**（cmd/scan.go、cmd/cmd_test.go）。硬退出
  路径（第二次 SIGINT 或 drain 超时）上 os.Exit(1) 跳过
  runScan 里 defer 的 sess.Out.Close()，导致每 sink 最多
  4 KB（默认 bufio.Writer）缓冲写入加上整个 SARIF 文档
  不落盘。preHardExit 现在在 Quit TUI 前调
  closeOutputForHardExit(sessOut)，让最后一行结果和完整
  SARIF 文档在进程死前都落到磁盘。三个回归测试覆盖 nil、
  流式（txt）、SARIF 单文档场景。

## [0.5.0] - 2026-09-01

跨时区定时扫描。用户经常从与目标不同时区发起扫描，需要不依赖宿主机
cron 的调度方式。v0.5 新增 `--at` / `--in` / `--cron` / `--tz` /
`--daemon` 做带内调度，新增 `fg-qimen schedules add | list | remove`
子命令做存项目 DB 的持久化调度。

完整验证：`docs/verification/v0.5/verification.md`

### Added

- **定时扫描 flags**（`--at`、`--in`、`--cron`、`--tz`、`--daemon`、
  `--schedule-dry-run`）：见 [CLI 参考](README.zh-CN.md#cli-reference)。
  扫描在到达目标时间前不打开任何 socket 或文件，配置错误的调度会
  快速失败。
- **`fg-qimen schedules add | list | remove` 子命令**：持久化调度存
  项目 DB（`schedules` bbolt bucket，由 `internal/scheduler/store.go`
  惰性打开）。`add` 在解析期校验 cron 表达式，坏记录不会落库。
- **`internal/scheduler` 包**：独立、无 cobra 依赖、可单测。持有
  cron 解析包装、带倒计时 + ctx 取消的等待循环、bbolt store。

### Changed

- **新外部依赖：`github.com/robfig/cron/v3`**（二进制约 +50 KB）。
  分类器第一轮拦下了这个依赖，按用户明确要求引入；备选方案是自写
  约 150 行的 cron 解析器。最终选 robfig/cron/v3，因为它处理了
  自写版本必然要重新踩坑的边界情况（DST 切换、秒字段、`@daily` /
  `@hourly` 描述符语法）。

### Test coverage

`internal/scheduler/` 与 `cmd/schedule_test.go` 新增 16 个单测：

- cron：合法 / 非法 / 描述符（`@hourly`、`@daily`、`@midnight`）/
  时区加载
- 等待：成功 / 取消 / dry-run
- bbolt store：add / get / list / remove / 幂等 remove / 覆盖时保留
  CreatedAt
- CLI：detectScheduleMode（3 模式 + 空）、loadScheduleTZ（3 变体）、
  `schedules add → list → remove` 全链路

总覆盖率 60.4%（v0.4.0 时为 60.0%）。

### Compatibility

- `--output-rotate-bytes` / `--output-rotate-files` 更名为
  `--rotate-bytes` / `--rotate-files`（v0.4.1 更名；`output-` 前缀
  冗余）。无其他破坏性变更。
- 早期 README 的"v0.4 核心改进"内容移入本 changelog；各插件的
  `(added v0.X)` 标注也移入本 changelog。README 只描述当前行为，
  不再按版本罗列。

## [0.4.0] - 2026-08-30

v0.4 周期：质量基础 + 四项核心管线改进。合并 v0.4.0-rc1 质量阶段
（CI 绿、覆盖率 60%）与下列四项 Phase 2 功能。

逐功能验证见
`docs/verification/v0.4/verification.md`
与 `docs/verification/v0.4/benchmarks.md`。

### Added

- **Phase 2.1 — Crack 模式重构**（`core.RunScan`）：RunScan 改为薄
  派发。ModeScan / ModeLinked 走 runFullPipeline（与之前相同
  alive → scan → identify → 可选 credential）。ModeCrack 走
  runCrackPipeline，完全跳过 alive + 端口扫描，直接把已知的
  host:port 列表喂给 plugin worker 池。256 主机 /24 × 6 端口的
  crack 场景省掉旧代码多发的约 1536 次冗余 TCP 连接。

- **Phase 2.2 — 代理统一**（`credential.DialTCPAddr`）：新增
  DialTCPAddr 接收预拼的 `host:port` 字符串，走与 DialTCP 同一全局
  proxy manager，让 `--proxy` / `--socks5` 在整个 auth 树上统一
  生效。telnet、vnc、ssh 已从 raw `net.Dialer` 迁到新 helper。其他
  插件（modbus、bacnet、ipmi）保留 raw `net.Dialer`——它们需要
  UDP / 自定义协议的传输路径，统一 TCP dialer 覆盖不了。

- **Phase 2.3 — 输出轮转**（`--output-rotate-bytes N`、
  `--output-rotate-files M`）：新增 `rotatingWriter`，当
  TXT / NDJSON / CSV / SARIF sink 跨过字节阈值时滚动。文件滚
  `<path>` → `<path>.1` → `<path>.2` → ... 至多保留 M 个总文件。
  `flushCloser` 重构为包装新类型。4 个单测覆盖 under-cap / at-cap /
  beyond-cap / zero-cap。

- **Phase 2.4 — `.fgq` 项目导入/导出**（`projects export` /
  `projects import`）：单文件可移植项目转储。格式：4 字节 magic
  `FGQ1` + 4 字节 LE uint32 header 长度 + JSON header（version、
  project、created_at、db_bytes）+ 原始 bbolt 数据，逐字节一致。
  CLI：`fg-qimen projects export <name> <out.fgq>` /
  `fg-qimen projects import <in.fgq> <name>`。import 拒绝覆盖已有
  项目，除非先 `delete`。`internal/workspace` 4 个单测 +
  `cmd/projects_test.go` 2 个 CLI 测试。

- **MQTT 插件**（1883 / 8883）：面向 MQTT 3.1.1 / 5.0 broker 的
  Identify 插件。

### Coverage

- 覆盖率门槛本周期 50% → 60%（实际 ~60.0%）。
- 本周期新增单测 9 个（`internal/workspace` 5 个、`internal/output`
  轮转 4 个，另加 `internal/version` 1 个防 const→var ldflag 回归）。

### CI

- v0.4.0-rc1 的 CI exit-126 / exit-127 修复延续：actionlint 走真实
  文件下载（不再 `bash <(curl)`）；覆盖率检查改用 Python 脚本
  （确定性，`tail -1` 不会再吃 SIGPIPE）。

## [0.3.1] - 2026-08-19（原始条目）

第二批审计驱动的正确性、安全与可靠性修复。全部十个 commit 记录在
`docs/verification/v0.3/second-batch-verification.md`；
此处只列用户可见变更。

### Security

- **P1-3 — 七个 TCP 类 authenticator 的 per-attempt 读超时**
  （telnet、rsync、vnc、modbus、nfs、rabbitmq、smb）。此前一个
  接受 TCP 但永不回包的慢/挂服务器能把 worker 卡死整整一个
  `cfg.Timeout`。每个 authenticator 现在在每轮凭据迭代开头调
  `conn.SetDeadline(time.Now().Add(timeout))` —— 与 `redis.go:133`、
  `mongo.go:97` 同一模式。
- **P2-7 — `Output.Close()` 中 `f.Sync()`**：`flushCloser.Close()`
  现在在 close 前 fsync 底层文件，掉电 / OOM-kill 时最后约 200ms
  的缓冲写入不再无声丢失。
- **P2-5 — `creds.txt` 去重**：`--resume` 或重试后重复派发的
  `(host, port, user, pass)` 命中不再追加重复行。闸门是管线 sink
  的 `sess.State.MarkSeen(chash)`。（bbolt 的 `PutCred` 本就幂等。）

### Fixed

- **P2-2 — `workersWG` 并入外层 `wg`**（`RunScan`）：此前生产者的
  `defer close(items)` 一旦漂移，`wg.Wait()` 可能永远挂在仍活跃的
  worker 池上。closer goroutine 现在注册到外层 `wg`，生产者挂死
  表现为 RunScan 返回变慢，而非永久 hang。
- **sink 错误传播（残留项）**：`persistResult` 现在检查每个
  Output / Store 错误并经 `sess.Log.Warn` 上报，不再无声丢弃。
- **P3-1 — BatchWriter `pendingBytes` 死代码删除**：每 op 64 KiB
  上限恒等于一个 op，字节阈值与条数阈值在同一 op 触发，实际不可达。
- **P3-3 — 自适应 goroutine join 延迟**：`Pool.adaptiveLoop` 改用
  50ms 唤醒 ticker（原 `opts.AdjustInterval` 默认 500ms），循环能
  及时看到 `stopAdj` 关闭。`adjust()` 仍经时间戳守卫节流到
  `opts.AdjustInterval`。
- **P3-5 — MySQL `sqlcache` 失效策略**：现在只在 MySQL 错误 1045
  （`ER_ACCESS_DENIED`）时失效缓存。网络错误（拒绝 / 超时 /
  server-gone）保留缓存的 `*sql.DB`，网络抖动下 Phase 1.9 热池
  不受损。
- **P3-7 — Pool 忙等 CPU 打满**：生产者退避改为有上限的指数退避
  （1ms → 50ms）。饱和下 CPU 降约 50×；空槽释放的响应仍在 50ms 内。

### Docs

- **README 供应链验证章节**：SHA256SUMS、cosign keyless 签名、
  CycloneDX SBOM、源码可复现性与差异上报的双语操作指引。
- `docs/SECURITY.md` 收编完整的"无漏洞利用"硬性规则契约（从
  README 移入）；README 保留 1 段 TL;DR。
- `docs/` 重组：18 份历史进度 / 审计报告移入 `docs/archive/`；
  顶层 docs 目录只留现行指南（ARCHITECTURE / CONFIGURATION /
  PLUGIN_GUIDE / SECURITY / FIRST_BATCH_VERIFICATION /
  SECOND_BATCH_VERIFICATION / RELEASE_NOTES_v0.2）。

## [0.3.0] - 2026-07-15

第一批审计驱动的正确性、性能与安全修复（7 个 commit）。全部七个
commit 记录在
`docs/verification/v0.3/first-batch-verification.md`；
此处只列用户可见变更。

### Security

- **P0 — 项目 DB 静态加密**：`internal/store/crypto.go` 为
  `PutResult` / `PutCred` 加值级 AES-256-GCM 加密。新 `--project-key`
  flag（及 `FG_QIMEN_PROJECT_KEY` 环境变量）可选启用。v0.2.x 明文
  bbolt 文件继续正常读取（前向兼容 magic bytes）。
- **P0 — workspace 加密接线**：`workspace.AsStoreWithKey` 把派生的
  32 字节 key 接入 `Store` 构造器；seen-set bucket 保持明文
  （只存非机密哈希）。
- **P1 — magic-byte AAD 绑定**：`store/crypto.go` 用 GCM AAD 把
  magic byte 与密文绑定。magic byte 翻位会被识别为
  `ErrDecryptFailed`，而不是把加密行静默当"明文"。
- **P1 — host flag 注入防护**：alive `cmd` 探针拒绝以 `-` 开头的
  host 值（否则会被解析成 `ping` 的 flag）。
- **P1 — 优雅 resume 降级**：`--resume` 遇到损坏 bbolt 记警告并以
  空 seen-set 继续，不再中止。
- **P1 — 输入校验**：`--ports 99999` / `--ports abc` 现在传播解析
  错误，不再静默跑 0 个端口。

### Performance

- **bbolt 批量写**：新 `store.BatchWriter` + `PutMany` 把每条结果
  一次 fsync 摊薄成每批一次（32 ops 或 200ms）。新 `--no-batch`
  flag 回退逐条写语义。
- **Output per-sink 锁拆分**：`Output` 改用 6 把 per-sink 锁
  （txt / json / creds / rdp.json / rdp.txt / csv），慢 sink 不再
  队头阻塞其他 sink。`csv.Writer` 提升为字段，避免每行分配。
- **`RawTCPIdentify` helper**：共享 TCP-dial 样板收敛到
  `internal/plugins/rawtcp.go`。5 个插件（redis、memcached、
  postgresql、mongodb、socks5）完成重构。
- **HTTP Transport 复用**：elasticsearch 与 web 插件改用进程级
  `http.Client`，不再每次 Identify 分配新 `http.Transport`。

### Added

- **CSV 输出**：新 `--output-csv` flag 写 RFC-4180 单行一结果的
  CSV，列序稳定。脱敏策略与 `result.txt` / `result.json` 一致。
- **稳定错误码**：`internal/types/errors.go` 定义带稳定 4 字符码的
  `CodedError`（E001..E999）。接入 `workspace.ValidateProjectName`
  与 `bolt.Open` 失败路径。
- **Flag 分组**：持久 flag 携带 `group` annotation，由
  `cmd/root.go` 的自定义 `usageTemplate` 渲染（Target、Workspace、
  Ports、Network、Concurrency、Credentials、Output、Behavior、
  Safety）。
- **golangci-lint 集成**：`.golangci.yml` 启用 errcheck、govet、
  staticcheck、revive、gocritic、gosec、errorlint、prealloc、
  misspell。CI workflow 增加 `lint` job 跑 `only-new-issues`。
- **CI**：govulncheck job + 覆盖率上传（仅 Linux）；`go mod tidy`
  作为 CI 门槛；release workflow tag 触发构建，带 `cosign` keyless
  签名与 CycloneDX SBOM 生成。

### Tests

本批次测试覆盖大幅上升。要点数字（完整矩阵见
`docs/verification/v0.3/first-batch-verification.md`）：

- `internal/network/proxy`：**0% → 86.3%**
- `internal/store`：**N/A → 61.3%**
- `internal/output`：**86.8% → 89.5%**
- `internal/types`：**80.2% → 80.6%**
- `cmd`：**47.4% → 50.0%**

另加约 70 个新测试用例，覆盖 FTP authenticator、BatchWriter、
magic-byte AAD 篡改检测、alive flag 注入防护、输出并发写、
`RawTCPIdentify` helper。

## [0.2.0] - 2026-06-15

首次公开发布（v0.1 审计修复）。完整清单见
`docs/verification/v0.2/release-notes.md`。

### Highlights

- 四阶段管线（alive → 端口扫描 → 插件识别 → 凭据喷洒）。
- 7 大类 26 个协议插件（database、email、file-storage、messaging、
  network、remote、web）。
- Bubbletea TUI 仪表盘，带 LIVE EVENTS 列与状态栏。
- bbolt 项目工作区，支持 resume。
- 多格式输出（TXT、NDJSON、creds、RDP JSON+TXT）。
- 多平台交叉构建（justfile 5 个 OS/arch 目标）。
- Garble 混淆 + UPX 压缩管线。
- 每目标节流、关停排水、context 取消传播到 goroutine。
