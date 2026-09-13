# Configuration

> [中文版本](CONFIGURATION.zh-CN.md)

How to configure FG-QiMen — flag areas, environment variables,
dictionary files, and per-workflow examples.

## Flag discovery

```bash
fg-qimen --help                # all flags grouped by category
fg-qimen scan --help           # subcommand-specific
```

The **complete flag table** — every flag with its short alias, default,
and description — lives in [FLAGS.md](FLAGS.md). It is generated from
the same pflag registry `fg-qimen --help` serves, so it can never
drift; a CI guard test fails when it does. This page documents the
areas, the environment variables, and worked examples.

Short-form flags follow the v0.5.1 convention (see
[CHANGELOG](../CHANGELOG.md)): all lowercase, mnemonic, 1-letter for
unique concepts (`-H`, `-f`, `-a`, `-r`, `-t`, `-u`, `-p`, `-v`) and
2-letter for namespaced flags (`-ot`, `-oj`, `-oc`, `-uf`, `-pf`); the
sole uppercase is `-H` (avoids the `-h`/`--help` collision that
cobra reserves).

## Flag areas

| Group | Covers |
|-------|--------|
| Target | hosts: inline / file / exclusion (CIDR, range, RFC1918 shortcuts) |
| Workspace | project name, DB key, run mode, resume, `--workspace` root |
| Ports | port spec, exclusions, opt-in UDP probing (`--udp` / `--udp-strict`), alive-only |
| Network | HTTP proxy, SOCKS5, interface, per-protocol timeouts, web fingerprint ruleset |
| Concurrency | thread cap, timeout policy, shutdown budget, plugin worker cap |
| Credentials | inline user/pass, dictionary files, HTTP form brute |
| Output | txt / NDJSON / CSV / SARIF sinks, rotation, alive-list format |
| Schedule | `--at` / `--in` / `--cron` / `--tz` / `--daemon` / dry-run |
| Behavior | silent, TUI, batch mode, ICMP, prescreen, verbosity, plugin filter |
| Safety | credential redaction, insecure-TLS/SSH opt-outs, known_hosts |

## Environment variables

| Variable | Effect |
|----------|--------|
| `FG_QIMEN_PROJECT_KEY` | Enables AES-256-GCM encryption of bbolt values. Generated key material: `openssl rand -hex 32`. |
| `FGQI_WORKSPACE` | Default workspace root (`./fgqm_workspace` when unset); the `--workspace` flag wins. |
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
fg-qimen --project corp -H 10.0.0.0/24 --mode linked \
    -u root,admin -p 123456,admin

# Resume after interruption
fg-qimen resume --project corp

# Use SOCKS5 proxy
fg-qimen -H 10.0.0.0/24 --socks5 socks5://127.0.0.1:1080

# Per-protocol timeouts
fg-qimen -H 10.0.0.0/24 --timeout 5s --web-timeout 10s

# Opt-in UDP service probing (silent segments: add --udp-strict)
fg-qimen -H 10.0.0.0/24 --udp

# Output to a different directory (opt-in path)
FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1 fg-qimen -H 10.0.0.0/24 \
    -ot /tmp/scan.txt -oj /tmp/scan.json

# Schedule a scan to start at a specific instant in another zone
# (timestamp's offset is the time zone — no --tz needed).
fg-qimen --project corp -H 10.0.0.0/24 --at "2026-12-25T09:00:00+08:00"

# Run a cron-scheduled scan every day at 9am Shanghai time.
# Ctrl-C to exit; --schedule-dry-run previews without waiting.
fg-qimen --project corp -H 10.0.0.0/24 --cron "0 9 * * *" \
    --tz Asia/Shanghai --daemon

# Persist a schedule in the project DB (survives restart) and
# inspect what's queued.
fg-qimen schedules add    --project corp morning --cron "0 9 * * *"
fg-qimen schedules list  --project corp
fg-qimen schedules remove --project corp morning
```

## See also

- `README.md` — user-facing overview
- [FLAGS.md](FLAGS.md) — generated full flag table (single source of truth)
- [PLUGINS.md](PLUGINS.md) — generated plugin roster
- `docs/ARCHITECTURE.md` — design rationale
- `docs/SECURITY.md` — threat model + encryption
- `docs/PLUGIN_GUIDE.md` — adding a new plugin
