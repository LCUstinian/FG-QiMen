> [English version](CHANGELOG.md)


# Changelog
## [Unreleased]

### BREAKING — 工作区目录改名

on-disk 工作区布局从 `runs/` 改名为 `fgqm_workspace/`，与所有 fg-qimen
结果文件（`fgqm_result.txt`、`fgqm_creds.txt`、`fgqm_alive.txt`、
`fgqm_rdp.*`）的 `fgqm_` 前缀统一。bbolt 状态文件相应从 `fg.db`
改名为 `fgqm.db`，理由相同。

| 旧（≤ v0.6.0） | 新（v0.6.x） |
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

### Changed

- TUI v2 Spec B (panel layout) + Spec C (visual polish): 3-breakpoint responsive layout (narrow/medium/wide); LIVE EVENTS panel with severity-coloured ring buffer; rate sparkline in header; collapsible ERRORS panel (e/E); single dark theme with severity colours; progress bars for alive/ports; status symbols + 200ms hit flash. See docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md.

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
- applySchedule 单元测试（cmd/schedule_test.go，含
daemon-loops 12 个 case）。v0.5 时该函数 0% 覆盖，现
**100%**。覆盖 ModeNone 早返、9 个 Resolve 错误路径（at
格式错 / 过去、in 格式错 / 0 / 负、cron 格式错、at+in
互斥、daemon 无 cron、tz 错）、3 种 mode 的 dry-run、等
未来时间、等 in 时长、daemon ctx 取消、cron 无 daemon、
并发调用 sanity、daemon 循环跑 ≥2 次（验证 post-Wait
代码）。互斥 case 用 errors.Is(ErrInvalidCombination)
钉死。
- 更多 cmd/ 测试（cmd/cmd_test.go、cmd/schedule_test.go）。
加 applyTransport（nil + flag 传递 + 空 KnownHosts 保护）、
applyHTTPForm（空 + 填）、detectScheduleMode（4 种 mode +
优先级）、loadScheduleTZ（空 / UTC / 非法 IANA 不 panic）
测试。cmd/ 单元可测代码 59.6% → 64.4%。总覆盖率仍 ~60.5%
因 30+ adapted plugin 0% 覆盖——需要 fake-server 基础
设施（v0.6 目标）。

### Changed

- 覆盖率门槛从 60% 抬到 70%（scripts/ci-coverage-check.py）。
  v0.5.1 设的目标 80% 通过 fake-server 覆盖推进后达到
  70.8%。80% 全局目标仍是 v0.6.x 的方向——per-plugin 70%
  walk 同时引入，让 CI 能抓住任何单 plugin 包的回归（之前
  30 个 plugin 全 0% 拖累 60% 全局地板，现在每个 plugin 都
  要单独 ≥ 60%）。modbus（plugin 端 readFullMBP bug）放
  FLOOR_EXEMPT，跟踪为 v0.6.1 follow-up。
- 6 字段 cron 表达式（internal/scheduler/cron.go）。解析器
从 cron.ParseStandard（5 字段）改为 cron.NewParser
(SecondOptional | ...)，5 或 6 字段都支持。文档化的 5
字段形式（`0 9 * * *` 等）仍可用；6 字段（`* * * * * *`
= 每秒）现在合法，用于快速测试和短间隔 daemon 任务。
- 二进制内嵌 time/tzdata（main.go）。二进制 +~400 KB（压
缩后）让 --tz 在精简容器镜像（没 /usr/share/zoneinfo）
上也能工作。少了这个，系统 tz DB 缺失会静默回退
time.Local（很多最小容器是 UTC 偏移 0）→ cron 触发时
间静默错。v0.5.1 改为默认开启，消除"我机器行 CI 挂"
的尴尬。
- 短参全面重构（cmd/flags.go、cmd/multishort.go、cmd/multishort_test.go、
cmd/{root,resume,scan,schedules}.go、internal/core/credential/pool.go、
README*）。单字母短参全部小写 + mnemonic；2 字母短参用于命名空
间 / 配对（output-* 和 user/pass-file，nmap `-oN/-oX/-oG/-oA`
先例）；无语义的大写短参（`-M`、`-X`、`-U`、`-W`、`-P`）删
除。**迁移表**（v0.5.0 → v0.5.1）：见上。无 deprecated
alias 保留——硬切。实现备注：pflag v1.0.9 在注册时拒绝多字
母 shorthand 会 panic，所以 `-ot` / `-oj` / `-oc` / `-uf` /
`-pf` 走 cmd/multishort.go 的 50 行预解析 hook，在 cobra 看
到 args 前改写为 `--output-txt` 等。flag-value 启发式（上
一个 arg 是 flag 形态则跳过重写）确保字面密码如 `-p "-ot"`
通过长形式能正确往返。
- **CI 卫生**：.gitattributes 锁 `*.go text eol=lf`，Windows checkout（core.autocrlf=true）不会再把源文件翻 CRLF 触发 gofmt -l。顺手解掉 internal/tui/ 里 4 个已有 golangci-lint 阻塞（Stage 比较的 truncateCmp、top-N slice 的 prealloc、renderErrorCategoriesRow 里多余的 ineffectual width、ETA docstring 注释续行对齐）。TestApplySchedule_WaitCronNoDaemon 的 minute-boundary flake 也修了：测试 cron 从 `*/1 * * * *`（每分钟）换成 `0 0 1 1 *`（每年），保证 1.2s ctx 超时永远先赢。
- **TUI 信息密度面板**（internal/tui/render.go、internal/tui/tui.go、internal/tui/styles.go、internal/types/state.go）。Header 行新增按阶段的 `[ ▶ STAGE ]` 徽章（ETA 右对齐）、扫描速率（hits/s 和 ports/s，EWMA 平滑）、每次渲染的预算。types.State 加 CountersView 投影，让视图层读稳定契约而不是改共享 map。ClassifyError（internal/core/errors.go）把扫描错误按 errors.Is/As 优先、子串 fallback 的方式路由到命名桶（timeout / refused / dns 等），TUI 底部以压缩汇总行展示 top categories。scanner 配套改造驱动新 Stage 枚举转移并填充 PluginHits / ErrorCategories。8 个单元测试 + 1 个契约测试钉住 rate EWMA、top-N 抽取、ETA 估算和 bar 尺寸。
- 全部结果文件加 `fgqm_` 前缀（cmd/scan.go、cmd/projects.go、
cmd/flags.go、internal/output/*_test.go、README*、docs/ARCHITECTURE.md、
docs/SECURITY.md）。七个默认结果文件名都带 `fgqm_` 前缀，混
合目录里一眼能认出是 fg-qimen 的产物。`targets.txt` 不加前
缀，因为操作员手编。-o / -j / --output-csv / --output-sarif 仍
可覆盖文件名（和路径），现有脚本管线传显式文件名继续可用。
- 同日多次 run 文件名加 HH-MM-SS 时间戳（cmd/scan.go、
cmd/cmd_test.go）。同日两次 run 现在产出不同文件名，不再
互相覆盖。目录仍按 YYYY-MM-DD 分桶，时间戳打在文件名上
（fgqm_result_14-30-22.txt 而非 fgqm_result.txt）。格式
HH-MM-SS 本地时间（连字符分隔，兼容 Windows 文件名，且
与 YYYY-MM-DD 风格一致）。-o / -j / --output-csv /
--output-sarif 仍可跳过时间戳——操作员传显式路径就是要精
确路径，不自动加缀。时间戳在 scan 开始时一次性抓取，单
次 run 的所有 sink 共享同一后缀。
- 结果文件按日分桶（cmd/scan.go、cmd/cmd_test.go、cmd/flags.go）。
默认结果路径现在带本地日期 YYYY-MM-DD 段，跨日扫描不会互相
覆盖。即扫即走模式新布局（项目模式同形，在 runs/projects/<name>/
下）：fg.db（持久化状态 / 去重 DB）保持在项目根，跨日共享；
只有结果产物分桶。-o / -j / --output-csv / --output-sarif 仍
接显式路径，跳过分桶（操作员传这些就是要精确路径）。桶名在
scan 开始时一次性抓取，跨午夜扫描落到单一日桶，不会拆分结果。

### Fixed

- **TUI mid-alive-sweep 计数实时更新**（internal/core/alive/cmd.go、internal/core/alive/probe.go、internal/tui/tui.go、internal/types/state.go）。原来 header 的 "alive N/M" 在 alive 阶段完成前一直停在 0/M；现在随 probe 完成即时增长。alive.Progress() 是供外部调用方观察中途探测数的公共 API。
- **mssql plugin 真正修了一个 bug**（internal/plugins/adapted/database/mssql/mssql.go）。原 DSN `server=127.0.0.1:12345;port=...` 把端口塞进 server= 字段，go-mssqldb 的 tcpParser 不会剥离端口后缀，ParseIP 返 nil，dial 永远失败。改成 `server=127.0.0.1;port=12345;...`（host 和 port 拆成两个独立 DSN key）后驱动正确解析。Fake-server 测试在修前发现这个 bug——它不需要连真 mssql server 就暴露了"plugin 写错了"这个事实。
## [0.5.0] - 2026-09-01
### Added
### Changed
### Test coverage
### Compatibility
## [0.4.0] - 2026-08-30
### Added
### Changed
### CI
## [0.4.0] - 2026-08-30
### Added
### Coverage
### CI
## [0.3.1] - 2026-08-19 (original entry)
### Security
### Fixed
### Docs
## [0.3.0] - 2026-07-15
### Security
### Performance
### Added
### Tests
## [0.2.0] - 2026-06-15
### Highlights
