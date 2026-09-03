# Changelog
## [Unreleased]
### Added
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
- 覆盖率门槛维持 60%（scripts/ci-coverage-check.py）。
A2 原目标 65%，但 30+ adapted plugin 0% 覆盖把总覆盖率拖
到 60.5%——没 fake-server fixture（v0.6 工作）到不了 65%。
门槛维持 60%，docstring 详细说明 65% 推迟原因。cmd/ 单
元可测代码已达 64.4%。
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
### Changed
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
