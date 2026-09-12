# test/ — Test data for FG-QiMen

This directory holds committed test data used for end-to-end smoke tests
and manual integration verification.

本目录存放提交进仓库的测试数据，用于端到端烟测和手动集成验证。

## Files / 文件

| File | Purpose |
|---|---|
| `targets.txt` | Sample target list (loopback + commented private ranges) |
| `users.txt` | Sample username dictionary (20 common accounts) |
| `passes.txt` | Sample password dictionary (40+ common passwords) |

## End-to-end smoke test / 端到端烟测

```bash
# 1. Build
just build

# 2. Start a local HTTP service in another terminal
python -m http.server 8080 --bind 127.0.0.1

# 3. Ephemeral scan (writes to ./fgqm_workspace/default/)
./release/fg-qimen -f test/targets.txt --ports 22,80,8080,3306 -t 5 --shutdown-timeout 2s --no-tui

# 4. Inspect ephemeral output
cat fgqm_workspace/default/*/fgqm_result_*.txt | head
cat fgqm_workspace/default/*/fgqm_result_*.json | head

# 5. Project-mode scan (writes to ./fgqm_workspace/projects/<name>/)
./release/fg-qimen projects create smoke
./release/fg-qimen -p smoke -f test/targets.txt --ports 22,80,8080,3306 -t 5 --shutdown-timeout 2s --no-tui

# 6. Project info
./release/fg-qimen projects info smoke

# 7. Credential test (SSH only; loopback won't have SSH by default)
#    This is a no-op against loopback; just shows the flag wiring.
./release/fg-qimen -p smoke -f test/targets.txt --ports 22 \
    -u-file test/users.txt -P-file test/passes.txt \
    -mode linked --no-tui

# 8. Cleanup
just clean-runs
```

## Automated commands / 自动化命令

```bash
# Live TUI smoke probe (build tag `smoke`, env-gated) — the executable
# form of the manual /24 TUI verification. Renders to an in-memory
# buffer, injects a scripted operator timeline, asserts on per-step
# deltas, dumps captures to the temp dir for human review.
# / 实机 TUI 冒烟探针（build tag `smoke`，环境变量门控）——人工 /24
# TUI 验证的可执行版。渲染到内存缓冲，注入脚本化操作员时间线，逐步
# 增量断言，捕获导出到临时目录供人工复核。
just smoke 192.168.204.0/24        # default 150 s scan budget / 默认 150 秒扫描预算
just smoke 192.168.204.0/24 90s    # custom budget / 自定义预算

# Fast test pass — skips the network-bound plugin smoke probes (UDP
# plugins wait out their ~3 s internal deadline on a closed port);
# fake-server protocol tests still run. Per-package iteration drops
# from ≥3 s to sub-second.
# / 快速测试——跳过依赖网络的插件冒烟探测（UDP 插件在关闭端口上要
# 等满自身约 3 秒的内部 deadline）；fake-server 协议测试照常运行。
# 单包迭代从 ≥3 秒降到亚秒级。
just test-short
```

## Hard rule reminder / 硬性原则提醒

The data in `passes.txt` is for **legitimate credential testing only**.
On a successful hit, FG-QiMen writes `(user, pass)` to `creds.txt` and
**stops** — no post-authentication action is ever taken.

`passes.txt` 仅用于**合法凭据测试**。命中时 FG-QiMen 把 `(user, pass)` 写入
`creds.txt` 然后**停止**——绝不做任何认证后动作。
