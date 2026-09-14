# A3 AIMD Parameter Sensitivity Report

> Measurement report (append-only, point-in-time: 2026-09-14). English
> body with a Chinese verdict summary — this file has no zh-CN mirror
> pair; it is a one-off A3 deliverable, not maintained reference
> documentation.
>
> / A3 AIMD 参数敏感度报告。测量报告（只追加、时点性：2026-09-14）。
> 英文正文 + 中文判定摘要——本文件不设 zh-CN 镜像对；它是 A3 的一次
> 性交付物，不是维护型参考文档。

## 1. Question / 问题

Does any AIMD controller parameter (or the pool target/cap it serves)
move the needle on scan wall time, identification quality, or probe
economics — enough to justify a conservative retune of the shipped
defaults? Per the v8 ruling: no chasing synthetic-topology optima;
retune only on a clear knee.

/ 任何 AIMD 控制器参数（及其服务的池 target/cap）能否在扫描墙钟、
识别质量或 probe 经济账上拉开差距——大到值得对出厂默认做保守调参
？按 v8 裁定：不追合成拓扑最优值，只在明确拐点上动手。

## 2. Method / 方法

- Judge: the A1 bench (`tools/bench`) drives the real `core.RunScan`
  with a fully-explicit Config (no adaptive-tuning variance).
- Topology: `farm` = 9 services (3× ssh/http/memcached) + 150
  blackholes + 50 refusals = 209 loopback ports.
- Injection: new `AIMDTuning` (zero-value = shipped default) plus
  `LoadDelay` — per-conn service delay that scales with concurrency,
  the loopback stand-in for "the service slows down as the scanner
  pushes harder" (verified by
  `TestServerLoadDelayScalesWithConcurrency`).
- Grid: one axis at a time, everything else frozen; 1 run per point
  (run-to-run wall noise on this topology is <1%, far below any
  effect size of interest). 33 points across 8 axes.

/ 裁判：A1 bench（`tools/bench`）以全显式 Config 驱动真实
`core.RunScan`（无自适应调优方差）。拓扑：`farm` = 9 服务 + 150 黑
洞 + 50 拒绝 = 209 回环端口。注入：新增 `AIMDTuning`（零值 = 出厂
默认）与 `LoadDelay`——随并发增长的单连接服务延迟，回环上"扫得越
狠服务越慢"的替身（`TestServerLoadDelayScalesWithConcurrency` 验证
）。网格：单轴变动、其余冻结；每点 1 轮（本拓扑轮间 wall 噪声
<1%，远低于关注效应量）。8 轴共 33 点。

## 3. Curves / 曲线（farm, runs=1, wall per run）

| axis | points (wall) | spread |
|---|---|---|
| threads | 16: 45.97s, 32: 45.83s, 64: 45.74s, 128: 45.73s, 256: 45.73s, 512: 45.75s | 0.5% |
| timeout | 500ms: 45.96s, 1s: 45.74s, 2s: 45.65s, 3s: 45.81s | 0.7% |
| adjust | 100ms: 46.01s, 250ms: 45.73s, 500ms: 45.77s, 1s: 45.72s | 0.6% |
| slowstart (/div) | 2: 45.92s, 4: 45.91s, 8: 45.80s, 16: 45.81s | 0.3% |
| aistep (/div) | 5: 45.83s, 10: 45.74s, 20: 45.80s, 40: 45.73s | 0.2% |
| mdstress | 0.70: 45.88s, 0.85: 45.80s, 0.95: 45.82s | 0.2% |
| mdcongest | 0.30: 45.96s, 0.50: 45.79s, 0.70: 45.75s | 0.5% |
| ratchet | 1.5: 45.90s, 3.0: 45.77s, 6.0: 45.66s, off: 45.70s | 0.5% |

Identification ratio (5.7%) and probe hit rate (3.8%) are invariant
across all 33 points. Identification-latency P95 is the only
non-invariant signal: 0.9s @16 threads → 9.8s @128 → 32.2s @512
(plugin-stage queue contention; single-run, 9 service samples —
directional, not calibrated).

/ 识别率（5.7%）与 probe 命中率（3.8%）在全部 33 点上不变。识别时
延 P95 是唯一非不变信号：16 线程 0.9s → 128 线程 9.8s → 512 线程
32.2s（插件阶段排队竞争；单轮 9 个服务样本——方向性，未标定）。

## 4. Findings / 发现

**F1 — Wall time is plugin-stage-bound on the judge topology.** The
connect phase (where the AIMD controller lives) costs ≤5s at 64
threads; the plugin stage (209 open-port items through the worker
pool) costs ~41s and is flat across every axis — even `timeout=500ms`
leaves wall unchanged. All 33 points fall within a 0.8% wall band.

/ **F1 —— 裁判拓扑上墙钟由插件阶段主导。** 连接阶段（AIMD 控制器
的辖区）在 64 线程下 ≤5s；插件阶段（209 个开放端口 item 过 worker
池）约 41s 且对所有轴全平——连 `timeout=500ms` 都不动墙钟。33 点
全部落在 0.8% 的 wall 带内。

**F2 — AIMD knobs are invisible in wall time here.** Farm's
blackholes are open-but-silent: every one becomes a plugin item. In
the regime AIMD exists for — mostly-filtered segments where the SYN
sweep dominates — the controller's pacing is first-order. The judge
cannot synthesize that regime on loopback (pre-handshake drop is not
simulatable; the A5 plan already accepts this).

/ **F2 —— AIMD 旋钮在墙钟上不可见。** farm 的黑洞是"开放但沉默"：
每个都成为插件 item。而 AIMD 存在的意义域——SYN 扫描主导的
mostly-filtered 网段——里控制器的节流是一阶效应。裁判在回环上无法
合成该域（握手前丢包不可模拟；A5 方案已接受此限制）。

**F3 — Concurrency buys no wall time here and costs identification
latency.** 16 threads do as well as 512 on wall; the tail (P95)
grows with queue burst at high concurrency.

/ **F3 —— 并发在本拓扑买不到墙钟，还赔上识别时延。** 16 线程与 512
线程 wall 持平；高并发的排队突发拉长尾部（P95）。

**F4 — Judge sanity holds.** ident/probe-hit invariance across all
grid points confirms the measurement substrate does not drift with
the parameters under test.

/ **F4 —— 裁判健全性成立。** 识别率/probe 命中率在全部网格点不变，
证明测量基底不随被测参数漂移。

## 5. Verdict / 判定

**No default changes.** Every curve is flat within noise; there is no
knee to conservatively retune toward, and any change would be
overfitting to a synthetic topology — exactly what the v8 ruling
forbids. The shipped AIMD defaults (target/4 birth, target/20 AI
step, 0.85/0.5 MD, 3.0 ratchet, 500ms adjust interval) stay. The
A-line hard gates (golden zero-regression, CI race) cover the
injection plumbing.

/ **不改任何默认值。** 所有曲线在噪声内持平；没有可保守调向的拐点
，任何改动都是对合成拓扑的过拟合——正是 v8 裁定禁止的。出厂 AIMD
默认（target/4 出生、target/20 AI 步长、0.85/0.5 MD、3.0 棘轮、
500ms 评估周期）保持。A 线硬门禁（golden 零回归、CI race）覆盖注
入管道。

## 6. What would change the picture / 什么会改变结论

1. A filtered-port regime (SYN-drop) in the judge — needs a real
   network or OS-level packet filtering (e.g. firewall drop rules on
   chosen loopback ports); re-run the axes there before ever touching
   defaults.
2. A workload where the connect phase dominates wall (huge
   port-space, mostly non-responding) — the A5 127/8 substrate.
3. A plugin-stage bottleneck fix (if any) would re-balance the wall
   budget and could make connect-phase parameters second-order
   visible.

/ 1. 裁判具备 filtered 端口域（SYN 丢弃）——需要真实网络或 OS 级
   包过滤（如对选定回环端口加防火墙丢弃规则）；动默认值之前先在该
   域重跑各轴。2. 连接阶段主导墙钟的负载（巨大端口空间、多数不应答
   ）——A5 的 127/8 基底。3. 插件阶段瓶颈若有修复会重配 wall 预算
   ，或使连接阶段参数以二阶可见。

## 7. Reproduce / 复现

```
just bench -sweep threads  -topo farm -runs 1
just bench -sweep timeout  -topo farm -runs 1
just bench -sweep adjust   -topo farm -runs 1 -threads 64
just bench -sweep slowstart -topo farm -runs 1 -threads 64
just bench -sweep aistep   -topo farm -runs 1 -threads 64
just bench -sweep mdstress -topo farm -runs 1 -threads 64
just bench -sweep mdcongest -topo farm -runs 1 -threads 64
just bench -sweep ratchet  -topo farm -runs 1 -threads 64
```

Raw NDJSON per axis: `<workspace>/bench/sweep_<axis>_<time>.ndjson`
(gitignored, machine-local).
/ 每轴原始 NDJSON：`<workspace>/bench/sweep_<axis>_<time>.ndjson`
（gitignored，仅本机）。
