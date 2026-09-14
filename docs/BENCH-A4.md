# A4 UDP Pool Throughput Report

> Measurement report (append-only, point-in-time: 2026-09-14). English
> body with a Chinese verdict summary — no zh-CN mirror pair; one-off
> A4 deliverable, same status as BENCH-A3.md.
>
> / A4 UDP 池吞吐报告。测量报告（只追加、时点性：2026-09-14）。英文
> 正文 + 中文判定摘要——不设 zh-CN 镜像对；与 BENCH-A3.md 同地位的一
> 次性 A4 交付物。

## 1. Question / 问题

Does the UDP phase's pool sizing (initial target / hard cap / probe
timeout) move the UDP wall clock enough to justify a conservative
retune of the shipped defaults — and does the raised-concurrency
regime surface resource-exhaustion errors that used to be swallowed?

/ UDP 阶段的池尺寸（初始 target / 硬上限 / probe 超时）能否把 UDP
墙钟拉开到值得保守重调出厂默认的程度——并且提高并发后是否会浮出
过去被吞掉的资源耗尽错误？

## 2. Method / 方法

- Judge: `tools/bench -proto udp` drives the real `core.RunScan`
  pipeline with `--udp` on — same substrate as the A1/A3 benches, no
  mock phases.
- Topology: `udp-farm` = loopback `127.66.0.0/28`, 14 expanded hosts ×
  35 ports picked from the live `UDPHintPorts()` set: 3 responsive
  fake services (bind the specific loopback IP; answer any datagram),
  30 silent ports (ONE wildcard `0.0.0.0` bind per port — every
  host's that port is open-but-idle and each probe pays the full read
  deadline), 2 closed (unbound; kernel answers ICMP port-unreachable
  → fast `StateClosed`). Ports the OS already owns are dropped from
  the farm entirely (bind-verified at plan time).
- Injection: `Config.UDPThreads` / `Config.UDPMaxThreads` (zero =
  shipped default) feed the UDP pool's `Threads`/`MaxThreads`;
  `UDPOptions.Timeout` is the explicit per-probe timeout.
- Grid: one axis at a time, everything else frozen; 3 runs per point.
  Run-to-run wall spread on this topology is <2%.
- Pre-A4 shipped defaults: target 128 / cap 200, timeout 2s.

/ 裁判：`tools/bench -proto udp` 以 `--udp` 开启驱动真实
`core.RunScan` 管线——与 A1/A3 基准同基底，无 mock 阶段。拓扑：
`udp-farm` = 回环 `127.66.0.0/28`，14 台展开主机 × 35 端口（取自实
时 `UDPHintPorts()` 集）：3 个响应式假服务（绑定具体回环 IP，任何
datagram 都应答）、30 个静默端口（每端口一条 wildcard `0.0.0.0` 绑
定——所有主机的该端口都 open-but-idle，每个探测付满读超时）、2 个
closed（不绑定；内核回 ICMP port-unreachable → 快速 `StateClosed`）
。OS 已占用的端口在计划期经绑定验证整体剔除。注入：
`Config.UDPThreads` / `Config.UDPMaxThreads`（零 = 出厂默认）接入
UDP 池 `Threads`/`MaxThreads`；`UDPOptions.Timeout` 为显式单 probe
超时。网格：单轴变动、其余冻结；每点 3 轮，轮间 wall 散布 <2%。A4
前出厂默认：target 128 / cap 200，超时 2s。

## 3. Curves / 曲线（udp-farm, runs=3, wall mean per point）

| udpthreads (timeout=2s) | wall mean | min | max |
|---|---|---|---|
| 128 (pre-A4 target) | 10.714s | 10.697s | 10.748s |
| 200 (pre-A4 cap) | 8.706s | 8.626s | 8.792s |
| 400 | 6.641s | 6.544s | 6.744s |
| **800 (new default)** | **4.589s** | 4.548s | 4.623s |
| 1600 | 4.435s | 4.410s | 4.458s |

| udptimeout (pool=128/200, pre-A4) | wall mean | min | max |
|---|---|---|---|
| 500ms | 3.227s | 3.017s | 3.570s |
| 1s | 5.671s | 5.605s | 5.781s |
| 2s (shipped) | 10.807s | 10.737s | 10.935s |

Records column was constant across every point and run (476 under the
pre-fix accounting — see F4), and no probe errors surfaced at any
concurrency up to 1600 (8× the pre-A4 cap).

/ 记录数在所有点与所有轮恒定（旧记账下 476——见 F4），并发推到
1600（A4 前 cap 的 8 倍）无任何 probe 错误浮出。

## 4. Findings / 发现

**F1 — The throughput knee is at 800.** 200 → 800 buys −47% wall;
800 → 1600 buys only −3%. Below the knee, wall scales with
concurrency (the silent fleet's read deadlines are paid in parallel);
above it, the deadline waves dominate and extra concurrency is
waste. 1600 shows no degradation (stable records, sub-2% spread), so
800 ships with demonstrated headroom above it.

/ **F1 —— 吞吐拐点在 800。** 200 → 800 买到 −47% 墙钟；800 → 1600
只剩 −3%。拐点以下 wall 随并发近似线性改善（静默大部队的读超时被
并行支付）；拐点以上由 deadline 波次主导，加并发纯浪费。1600 无退
化（记录恒定、散布 <2%）——800 出厂时上方有实测余量。

**F2 — Timeout is the dominant but off-limits lever.** Wall is
linear in the probe timeout (each silent wave costs one full
deadline). It stays at 2s: real-network silence budgets are a
correctness knob served by the adaptive sampler, not a throughput
dial to overfit on loopback.

/ **F2 —— 超时是主导杠杆但不在授权范围内。** wall 对 probe 超时线
性（每波静默付一个完整 deadline）。保持 2s：真实网络的静默预算是
正确性旋钮（由自适应采样器服务），不是可以在回环上过拟合的吞吐旋
钮。

**F3 — Resource-exhaustion errors now surface instead of vanishing.**
Raising the cap made dial/send/recv starvation reachable (EMFILE /
ENOBUFS class). Pre-A4, a starved UDP dial was silently laundered
into a Filtered result — a lost probe with no evidence. A4 maps it
via `udpDialVerdict` + explicit checks on all three paths (dial,
write, read) to: RetryableProbe absorbs transients with backoff;
persistent starvation reaches the pool, `RecordExhausted()` feeds the
AIMD controller (shrink), and `OnProbeError` gives the operator the
log line. Refused → closed and unreachable/timeout verdicts are
unchanged (pattern set reviewed for misclassification: no overlap
with refused/timeout message shapes).

/ **F3 —— 资源耗尽错误现在浮出而不是消失。** 提高上限使 dial/send/
recv 饥饿路径可达（EMFILE / ENOBUFS 类）。A4 前饥饿的 UDP dial 被
悄悄洗成 Filtered——探测无证据丢失。A4 经 `udpDialVerdict` + 三条
路径（dial、write、read）的显式检查映射为：RetryableProbe 以退避吸
收瞬时耗尽；持续性饥饿到达池，`RecordExhausted()` 喂 AIMD 控制器
（缩容），`OnProbeError` 给操作员日志。refused → closed 与
unreachable/timeout 裁决不变（已审查模式集与 refused/timeout 消息
形状无重叠，无误分类）。

**F4 — Record accounting forensics (the 476 mystery).** Forensic run:
exactly one UDP record per (host, farm-port) = 14 × 35 = 490, plus 14
records on port 135 with an empty shape — Windows itself serves
TCP/135 (RpcSs) on every loopback address, so the farm's "TCP phase
is all-refusals" premise leaks one OS-open record per host. The old
counter excluded the whole closed bucket (and with it all 135
records): 504 − 28 = 476. Fixed: `countUDPRecords` now counts all
farm ports; the expected Windows signature is 504 (= 490 + 14). The
metric's role stays a drift indicator — cross-point stability is what
the sweep reads.

/ **F4 —— 记录记账取证（476 之谜）。** 取证跑：每个（主机，
farm 端口）恰好一条 UDP 记录 = 14 × 35 = 490，外加 14 条端口 135
的空 shape 记录——Windows 自身在所有回环地址上服务 TCP/135
（RpcSs），farm"TCP 阶段纯拒绝"的前提每主机漏一条 OS 开放记录。
旧计数器把 closed 桶整个排除（连带 135 的全部记录）：504 − 28 =
476。已修：`countUDPRecords` 现统计全部 farm 端口；Windows 特征值
为 504（= 490 + 14）。该指标的定位仍是漂移指示器——扫描读的是跨点
稳定性。

## 5. Verdict / 判定

**One conservative change: UDP pool defaults 128/200 → 800/800**
(`DefaultUDPThreads` / `DefaultUDPMaxThreads`, target and cap both at
the knee; slow start still ramps target/4 → 2× → knee, AIMD still
shrinks under stress, and the F3 guard covers FD starvation).
Timeout stays 2s. This is not an overfit: the knee is a
first-order concurrency effect of the silent-fleet model itself,
confirmed stable at 2× with zero error-rate change.

/ **一处保守变更：UDP 池默认 128/200 → 800/800**（
`DefaultUDPThreads` / `DefaultUDPMaxThreads`，target 与 cap 都取拐
点；慢启动仍按 target/4 → 2× → 拐点爬坡，AIMD 仍会在压力下缩容，
F3 守卫覆盖 FD 饥饿）。超时保持 2s。这不是过拟合：拐点是静默大部
队模型自身的一阶并发效应，且在 2× 处验证稳定、错误率零变化。

## 6. What would change the picture / 什么会改变结论

1. A real-WAN substrate (the A5 plan): real RTTs change both the
   knee and the timeout trade-off; re-run both axes there before any
   further bump.
2. A low-FD Linux regime (`ulimit -n` small): the F3 guard makes
   starvation survivable, but the knee may move; sweep before bumping
   again.
3. Per-probe payload multiplication (more payloads per hint port)
   would shift the wall model from deadline-waves to send-rate —
   re-measure.

/ 1. 真实 WAN 基底（A5 方案）：真实 RTT 会同时改写拐点与超时权衡；
   进一步调参前先在该域重跑两轴。2. 低 FD Linux 域（`ulimit -n`
   小）：F3 守卫使饥饿可存活，但拐点可能移动；上调前先扫描。3.
   单 probe payload 翻倍（每 hint 端口更多 payload）会把 wall 模型
   从 deadline 波次转向发送速率——需重测。

## 7. Reproduce / 复现

```
go run ./tools/bench -proto udp -runs 3
go run ./tools/bench -proto udp -sweep udpthreads -runs 3
go run ./tools/bench -proto udp -sweep udptimeout -runs 3
```

Raw NDJSON per axis: `<workspace>/bench/sweep_<axis>_<time>.ndjson`
(gitignored, machine-local).
/ 每轴原始 NDJSON：`<workspace>/bench/sweep_<axis>_<time>.ndjson`
（gitignored，仅本机）。
