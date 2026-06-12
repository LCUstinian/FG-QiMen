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

# 3. Ephemeral scan (writes to ./runs/default/)
./release/fg-qimen -f test/targets.txt --ports 22,80,8080,3306 -t 5 --shutdown-timeout 2s --no-tui

# 4. Inspect ephemeral output
cat runs/default/result.txt
cat runs/default/result.json

# 5. Project-mode scan (writes to ./runs/projects/<name>/)
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

## Hard rule reminder / 硬性原则提醒

The data in `passes.txt` is for **legitimate credential testing only**.
On a successful hit, FG-QiMen writes `(user, pass)` to `creds.txt` and
**stops** — no post-authentication action is ever taken.

`passes.txt` 仅用于**合法凭据测试**。命中时 FG-QiMen 把 `(user, pass)` 写入
`creds.txt` 然后**停止**——绝不做任何认证后动作。
