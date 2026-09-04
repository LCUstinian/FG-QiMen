# Configuration

> [中文版本](../CONFIGURATION.zh-CN.md)

How to configure FG-QiMen — flags, environment variables, dictionary
> files.

## Flag discovery

```bash
fg-qimen --help                # all flags grouped by category
fg-qimen scan --help           # subcommand-specific
```

Flags are grouped into 9 categories in the help output. Short-form
flags follow the v0.5.1 convention (see [CHANGELOG](../CHANGELOG.md)):
all lowercase, mnemonic, 1-letter for unique concepts
(`-H`, `-f`, `-a`, `-r`, `-t`, `-u`, `-p`, `-v`) and 2-letter
for namespaced flags (`-ot`, `-oj`, `-oc`, `-uf`, `-pf`); the
sole uppercase is `-H` (avoids the `-h`/`--help` collision that
cobra reserves). Run `fg-qimen --help` for the authoritative list.

| Group | Short | Long |
|-------|-------|------|
| Target | `-H`, `-f` | `--host`, `--hosts-file` |
| Workspace | — | `--project`, `--project-key`, `--mode`, `-r`/`--resume`, `--no-state` |
| Ports | `-a` | `--ports`, `--exclude-ports`, `-a`/`--alive-only` |
| Network | — | `--proxy`, `--socks5`, `--iface`, `--port-timeout`, `--web-timeout` |
| Concurrency | `-t` | `-t`/`--threads`, `--timeout`, `--shutdown-timeout` |
| Credentials | `-u`, `-p`, `-uf`, `-pf` | `--user`, `--pass`, `--user-file`, `--pass-file` |
| Output | `-ot`, `-oj`, `-oc` | `--output-txt`, `--output-json`, `--output-csv`, `--rotate-bytes`, `--rotate-files` |
| Schedule | — | `--at`, `--in`, `--cron`, `--tz`, `--daemon`, `--schedule-dry-run` |
| Behavior | `-v` | `--silent`, `--no-tui`, `--no-batch`, `--no-icmp`, `-v`/`--verbose`, `--plugins` |
| Safety | — | `--show-creds`, `--insecure-tls`, `--insecure-ssh`, `--known-hosts` |

## Environment variables

| Variable | Effect |
|----------|--------|
| `FG_QIMEN_PROJECT_KEY` | Enables AES-256-GCM encryption of bbolt values. Generated key material: `openssl rand -hex 32`. |
| `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1` | Opt-out of the project-root path containment check for `--output-txt`/`--output-json`/`--output-csv`. |
| `FG_QIMEN_SOCKS5_USER` / `FG_QIMEN_SOCKS5_PASS` | SOCKS5 proxy auth (CLI flag values take precedence). |
| `NO_COLOR` | Disable ANSI color (per https://no-color.org/). Honoured by the text banner and TUI. |
| `TERM=dumb` | Skip the TUI. |
| `CI` family | Skip the TUI. |

## Dictionary files

`--user-file` (one username per line):

```
admin
root
test
oracle
postgres
```

`--pass-file` (one password per line; `#` lines are skipped):

```
# top-10 worst passwords
123456
password
admin
root
qwerty
```

Empty lines are ignored. Files are read once at scan start.

## Examples

```bash
# Ephemeral scan of a /24 with default ports
fg-qimen -H 192.168.1.0/24

# Project mode with encryption
export FG_QIMEN_PROJECT_KEY=$(openssl rand -hex 32)
fg-qimen -p corp -H 10.0.0.0/24 -mode linked \
    -u admin root -P 123456 admin

# Resume after interruption
fg-qimen resume -p corp

# Use SOCKS5 proxy
fg-qimen -H 10.0.0.0/24 --socks5 socks://127.0.0.1:1080

# Per-protocol timeouts
fg-qimen -H 10.0.0.0/24 --timeout 5s --web-timeout 10s

# Output to a different directory (opt-in path)
FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1 fg-qimen -H 10.0.0.0/24 \
    -o /tmp/scan.txt -j /tmp/scan.json

# Schedule a scan to start at a specific instant in another zone
# (timestamp's offset is the time zone — no --tz needed).
# / 排一个跨时区扫描在指定瞬时启动（时间戳的 offset 就是
# 时区——不用 --tz）。
fg-qimen -p corp -H 10.0.0.0/24 --at "2026-12-25T09:00:00+08:00"

# Run a cron-scheduled scan every day at 9am Shanghai time.
# Ctrl-C to exit; --schedule-dry-run previews without waiting.
# / 每天上海时间 9am 跑 cron 调度扫描。Ctrl-C 退出；
# --schedule-dry-run 不等、只打预览。
fg-qimen -p corp -H 10.0.0.0/24 --cron "0 9 * * *" \
    --tz Asia/Shanghai --daemon

# Persist a schedule in the project DB (survives restart) and
# inspect what's queued. / 调度持久化到项目 DB（重启后保留），
# 查看挂起的调度。
fg-qimen schedules add    -p corp morning --cron "0 9 * * *"
fg-qimen schedules list  -p corp
fg-qimen schedules remove -p corp morning
```

## See also

- `README.md` — user-facing overview
- `docs/ARCHITECTURE.md` — design rationale
- `docs/SECURITY.md` — threat model + encryption
- `docs/PLUGIN_GUIDE.md` — adding a new plugin
