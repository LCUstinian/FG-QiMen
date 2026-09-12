# v0.7.0 Spec B+C — Verification Report

Date: 2026-09-12
Branch: `feat/tui-v2-spec-bc`
Spec: [docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md](../../specs/2026-09-12-tui-v2-spec-bc-design.md)
Plan: [docs/superpowers/plans/2026-09-12-tui-v2-spec-bc.md](../../../docs/superpowers/plans/2026-09-12-tui-v2-spec-bc.md)

v0.7.0 Spec B+C is the **TUI v2 layout + visual polish release**. Spec B
reships the dashboard as a 6-region composition (header, live events,
stage, top plugins, errors, footer) driven by a 3-breakpoint responsive
layout (narrow <80 / medium 80–119 / wide ≥120); wide terminals place
STAGE + TOP PLUGINS side-by-side. Spec C layers the visual polish on
top: severity colours, `▓/░` progress bars, status symbols, a 10fps
spinner rotation and a 200ms hit flash. The unbounded `pending`/`events`
slice is gone — replaced by a fixed ring buffer (cap 20) with a
documented "lose history rather than OOM" pause contract.

/ v0.7.0 Spec B+C 是 **TUI v2 布局 + 视觉打磨发布**。Spec B 把
dashboard 重塑为 6 区域组合（header、实时事件、stage、top plugins、
errors、footer），由 3 断点响应式布局驱动（narrow <80 / medium 80–119
/ wide ≥120）；宽终端把 STAGE + TOP PLUGINS 并排放置。Spec C 在其上
叠加视觉打磨：severity 颜色、`▓/░` 进度条、状态符号、10fps spinner
旋转和 200ms 命中闪高。无界的 `pending`/`events` slice 已删除——换成
固定 ring buffer（cap 20），暂停契约文档化为"丢历史好过 OOM"。

## What shipped

8 features across the `internal/tui/` package:

### Spec B (layout)

- [x] 3-breakpoint responsive layout — `pickBreakpoint` + `regions`
      (narrow <80, medium 80–119, wide ≥120); `View()` composes the
      6 regions per breakpoint, wide puts STAGE | TOP PLUGINS
      side-by-side via `lipgloss.JoinHorizontal`.
- [x] LIVE EVENTS panel — fixed ring buffer (cap 20) of the last
      events, severity-coloured, chronological through wrap.
- [x] Rate sparkline — 60-sample hits/sec ring buffer rendered as
      Unicode block glyphs (`▁▂▃▅▇`) in the header.
- [x] Collapsible ERRORS panel — 1 summary row collapsed, up to 4
      category rows expanded (`e` toggle / `E` collapse).

### Spec C (visual polish)

- [x] Single dark theme — Sliver C2-inspired palette (v0.5.2 base)
      with severity colours (cyan/amber/red dim layering); `NO_COLOR`
      honoured.
- [x] Progress bars — `renderBar` `▓/░` bars replace plain counters
      for alive/ports (10-wide on narrow, 20 elsewhere).
- [x] Status symbols + animations — `✓`/`✗`/`⚠`/`▶`/`·` glyph mapping
      (`symFor`), 10fps spinner rotation driven by `tickMsg` +
      `frameIdx` (no bubbles dependency), 200ms hit flash
      (`flashUntil` pruned by `flashTickMsg`).
- [x] Layout breakpoint polish — region sizing/panel placement per
      breakpoint; narrow hides events by default with an `L`-key
      overlay revealing the last 5.

/ 8 项特性，全部落在 `internal/tui/` 包。Spec B：3 断点响应式布局
（`pickBreakpoint` + `regions`），`View()` 按断点组合 6 区域，宽屏
STAGE | TOP PLUGINS 并排；LIVE EVENTS 面板（cap 20 ring buffer，回绕
保持时间顺序、severity 着色）；header 的 60 样本 hits/sec sparkline
（Unicode 块字符）；可折叠 ERRORS 面板（折叠 1 行汇总 / 展开最多 4
行类别，`e` 切换 / `E` 折叠）。Spec C：Sliver C2 风格暗色主题 +
severity 颜色（遵循 `NO_COLOR`）；alive/ports 换 `▓/░` 进度条
（narrow 10 宽、其余 20 宽）；状态符号 `symFor` 映射 + `tickMsg` 驱动
的 10fps spinner 旋转（未引 bubbles 依赖）+ 200ms 命中闪高（
`flashTickMsg` 剪枝 `flashUntil`）；断点打磨以区域尺寸/面板放置落地，
narrow 默认隐藏 events、`L` 键 overlay 显示最近 5 条。

## Deviations from the plan

- **No `runner.EventStream()` subscription** (Task 11): the codebase's
  event path is push-based via the `ui.UI` interface —
  `Program.Event`/`CredFound` send `eventMsg` straight into the
  Bubbletea loop. No channel subscription exists to wire; `Init()`
  already batches `tickCmd()` + `flashTick()` + `rateTick()`. Task 12's
  integration tests pin the seam that actually exists.
- **No `spinner.Model`** (spec §4): `bubbles/spinner` is not a
  dependency and the plan forbids new dependencies. The existing
  `frameIdx` + `spinnerFrames` rotation covers the glyph; the model
  carries a NOTE where the field was planned.
- **No `ThickBorder`**: Spec C's border polish shipped as region
  sizing/panel placement with the existing `NormalBorder`; a
  thick-border variant had no concrete use case (YAGNI).
- **`-race` not exercised on Windows**: the race runtime fails to load
  on this machine (`exit status 0xc0000139`, DLL entry-point error) —
  pre-existing and environmental, not caused by this change; CI already
  disables `-race` on Windows (commit `f83fe88`). All green without it.

/ 与计划的偏差：Task 11 无 `runner.EventStream()` 可接——本代码库的
事件路径是 `ui.UI` 接口推送式（`Program.Event`/`CredFound` 直接把
`eventMsg` 发进 Bubbletea 循环），`Init()` 已批量启动三个 tick；
Task 12 的集成测试钉住实际存在的接缝。spec §4 的 `spinner.Model` 未
引入（bubbles 非依赖、"无新增依赖"约束），`frameIdx` + `spinnerFrames`
轮转覆盖字形需求，model 留有 NOTE。Spec C 的边框打磨以区域尺寸/面板
放置 + 现有 `NormalBorder` 落地，未用 `ThickBorder`（无具体用例，
YAGNI）。Windows 上未跑 `-race`：race runtime 加载失败
（`exit status 0xc0000139`，DLL 入口点错误）——既有环境问题、非本次
改动引入；CI 已在 Windows 禁用 `-race`（commit `f83fe88`）。去掉
`-race` 全绿。

## Test coverage

51 tests across 4 test files, all PASS. Coverage:
**`internal/tui/` 76.2%** (floor 60% — PER_PLUGIN_FLOOR, met).

| Test file | Tests | Status |
|---|---|---|
| `internal/tui/layout_test.go` | 6 — TestPickBreakpoint, TestRegions_AllBreakpoints, TestTwoColumn_NarrowWidthCollapses, TestWindowSizeMsg_ReValidatesLayout, TestUpdate_PromotesRunStateIdleToScanning, TestUpdate_PromotesScanningToDone | PASS |
| `internal/tui/view_test.go` | 19 — TestRenderBar, TestSparkline, TestTruncate, TestSymFor, TestSeverityColor, TestViewHeader_{Narrow,Medium,Wide,ZeroHeight}, TestStageBadge, TestCountersLine, TestEtaLine, TestUptimeLine, TestViewLiveEvents_{Empty,RendersRecentEvents,NarrowHides}, TestViewErrors_{Collapsed,Expanded}, TestViewStage_ProgressBars | PASS |
| `internal/tui/model_test.go` | 6 — TestEventRingBuffer_{PushAndOrder,LessThanCap}, TestRateRingBuffer, TestPushEvent_SetsFlash, TestPruneExpiredFlashes, TestClearErrors | PASS |
| `internal/tui/program_test.go` | 20 — TestDispatcherEventMsg (ring-cap contract), TestDispatcherEventMsg_SetsFlash, TestDispatcherPausedDropsEvents, TestDispatcherStatsMsg/DoneMsg/Fallthrough/ViewDelegates, TestModelLingerExits, TestModelTickAdvancesSpinner, TestNewProgram*, TestProgram{Done,Banner,Stats,Event,CredFound}*, TestDispatcher_{RendersRateAndPlugins,RateEmptyState,E2E_StateWiring} | PASS |

Plan-named tests adapted to the real architecture:
`TestEventSubscriptionWiresThrough` → `TestDispatcherEventMsg_SetsFlash`
(flash wiring through the dispatcher→pushEvent seam); the
`TestFlashDecaysAfter200ms` contract (flash expiry returns the
severity colour) is covered by `TestSeverityColor`'s expired-flash
case using a synthetic clock instead of a sleep.

/ 4 个测试文件共 51 个测试全过；`internal/tui/` 覆盖率 **76.2%**
（地板 60%——PER_PLUGIN_FLOOR，达标）。计划里的测试名按真实架构
做了适配：`TestEventSubscriptionWiresThrough` →
`TestDispatcherEventMsg_SetsFlash`（钉住 dispatcher→pushEvent 接缝的
flash 接线）；`TestFlashDecaysAfter200ms` 的契约（flash 过期回落到
severity 颜色）由 `TestSeverityColor` 的 expired-flash 用例以合成
时钟（而非 sleep）覆盖。

## Verification commands

```text
gofmt -l internal/tui/            → no diffs
go vet ./internal/tui/            → clean
golangci-lint run internal/tui/   → clean
go test ./internal/tui/           → ok, 76.2% coverage
go test -race ./internal/tui/     → N/A on this machine (see deviations)
go build ./...                    → clean
go build . && fg-qimen --help     → exits 0, no panic
```

## Smoke test (manual, on /24 real targets)

Not yet performed on this branch — pending a live run before the
v0.7.0 tag. Expected on the live run: header sparkline updates each
second; LIVE EVENTS fills as the scan progresses; severity colours
visible per kind; each hit flashes red for ~200ms; `e`/`E` collapse
and expand the ERRORS panel; on a narrow terminal `L` overlays the
last 5 events.

/ 尚未在本分支做实机 /24 冒烟——打 v0.7.0 tag 前补一次实跑。预期：
header sparkline 每秒更新；LIVE EVENTS 随扫描填充；不同 kind 的
severity 颜色可见；每次命中红色闪高 ~200ms；`e`/`E` 折叠/展开
ERRORS 面板；窄终端下 `L` overlay 显示最近 5 条事件。

## Open items

- Sparkline width tuning (currently the full 60-sample window; a
  narrower header budget may want a shorter window on narrow
  breakpoints).
- Flash colour tuning: the 200ms flash paints `colorErr` red for all
  hit-family kinds; a softer accent may read better on `cred_success`.
- `toEntry` helper in model.go remains unwired (nolint:unused) — it
  exists for a future runner→TUI direct-entry path; delete if that
  path never materialises.
- Expanded-errors row count is fixed at 4; an operator-preference
  knob is deliberately out of scope (YAGNI).

/ 遗留项：sparkline 宽度调优（目前全 60 样本窗口，narrow 断点可能
需要更短窗口）；flash 颜色调优（200ms 闪高对所有 hit 类画红
`colorErr`，`cred_success` 或许用更柔和的强调色更好读）；
model.go 的 `toEntry` 仍未接线（nolint:unused，为未来 runner→TUI
直连路径保留，若该路径不会出现则删除）；展开态 errors 行数固定 4，
不做操作员偏好开关（YAGNI）。
