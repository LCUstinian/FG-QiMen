# Configuration

> How to configure FG-QiMen — flags, environment variables, dictionary
> files.

## Flag discovery

```bash
fg-qimen --help                # all flags grouped by category
fg-qimen scan --help           # subcommand-specific
```

Flags are grouped into 9 categories in the help output:

| Group | Examples |
|-------|----------|
| Target | `--host`, `--hosts-file` |
| Workspace | `--project`, `--project-key`, `--mode`, `--resume`, `--no-state` |
| Ports | `--ports`, `--exclude-ports`, `--alive-only` |
| Network | `--proxy`, `--socks5`, `--iface`, `--port-timeout`, `--web-timeout` |
| Concurrency | `--threads`, `--timeout`, `--shutdown-timeout` |
| Credentials | `--user`, `--pass`, `--user-file`, `--pass-file` |
| Output | `--output-txt`, `--output-json`, `--output-csv` |
| Behavior | `--silent`, `--no-tui`, `--no-batch`, `--no-icmp`, `--verbose`, `--plugins` |
| Safety | `--show-creds`, `--insecure-tls`, `--insecure-ssh`, `--known-hosts` |

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
```

## See also

- `README.md` — user-facing overview
- `docs/ARCHITECTURE.md` — design rationale
- `docs/SECURITY.md` — threat model + encryption
- `docs/PLUGIN_GUIDE.md` — adding a new plugin
