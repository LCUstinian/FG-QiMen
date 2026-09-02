# Architecture

## Pipeline data flow

```
hostiter ─ch(host)──> portscan ─ch("host:port")──> pluginWorker
                                                       │
                                                       ├─ Identify plugin ─┐
                                                       └─ Credential plugin┴─> sink
sink = output (TXT + NDJSON + creds) + store (bbolt dedup)
```

Each stage is a goroutine. Context cancellation propagates through
every channel.

## Why channel-decoupled?

The port scanner is the fastest stage. Plugin workers are the
slowest. Decoupling lets the scanner fill the buffer while slow
workers chew on one target.

Channel buffer `DefaultChannelBuffer = 1024` — enough for 200-
worker spikes, small enough that SIGINT drains within the
shutdown-timeout window.

## Package layering

```
cmd/                                Cobra commands
└── internal/
    ├── session/                    top of the leaf DAG
    │   ├── types/                  leaf: Config, State, Result, Cred
    │   ├── output/                 multi-format sink
    │   ├── store/                  bbolt persistence
    │   ├── ui/                     UI interface + TextUI + NopUI
    │   └── tui/                    Bubbletea dashboard
    ├── core/                       pipeline orchestrator
    │   ├── alive/                  host discovery
    │   ├── scan/                   port scanner
    │   ├── credential/             spray scheduler
    │   ├── plugins/                Plugin interface + registry
    │   │   └── adapted/            30 built-in plugins
    │   ├── portscan/fingerprint/   Nmap PSL service fingerprint
    │   ├── discovery/              LAN-only ARP + NetBIOS
    │   └── workspace/              ephemeral / project state
    ├── scheduler/                  cross-timezone schedule (--at, --in,
    │                              --cron); cron parser via robfig/cron/v3
    └── version/                    ldflag-injected version string
```

Strict downward: `core/` may import `types/`, but `types/` does
not import `core/`. Leaf packages have no `internal/` imports.

## Plugin layers

1. **Identify plugins** under `internal/plugins/adapted/`. Return
   banner / version / title.
2. **Credential authenticators** under
   `internal/core/credential/auth/<category>/`. Speak the service
   auth protocol and return Hit (or nil).

Both self-register via `init()`. Both implement the same hard
rules: no post-auth action, no exploitation.

## Modes

- `scan` — Identify only.
- `crack` — skip port scan, run Credential against configured ports.
- `linked` — run scan first, then trigger Credential on services
  that declared `ModeCredential`.

## Project workspace

```
runs/projects/<name>/
├── fg.db                # bbolt state
├── targets.txt
├── result.txt / .json / .csv
├── creds.txt            # always cleartext
└── rdp.json / rdp.txt   # RDP deep fingerprint
```

Ephemeral mode (no `-p`): workspace is CWD, no bbolt. Project mode
(`-p <name>`): per-project bbolt + optional encryption.

## Batched writes (v0.3.1+)

`store.BatchWriter` accumulates `PutOp` values and flushes in
batches of 32 ops or every 200ms. `--no-batch` falls back to
per-write semantics.

## Performance

- Adaptive worker pool (sliding window on filtered/open ratios).
  Self-tunes 64-200.
- `LoadSeenHashes` pre-allocates using `bk.Stats().KeyN` so 100k
  resumes avoid 17 log-2 reallocs.
- Output sink uses 6 per-sink mutexes so a slow sink doesn't
  head-of-line block the others.

## Tradeoffs

- **Pool dedup key is HMAC-hashed, but cleartext is still in the
  heap** — process memory dumps can recover pre-GC strings.
  Documented in `docs/SECURITY.md`.
- **TUI is opt-in by default.** Non-TTY stdout (CI, scripts) get
  the text logger.
- **`RawTCPIdentify` is a thin wrapper** — doesn't abstract every
  protocol. UDP fallback (SNMP) and TLS probe (HTTPS) still write
  their own dial loop.
