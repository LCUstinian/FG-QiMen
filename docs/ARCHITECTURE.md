# Architecture

> [中文版本](../ARCHITECTURE.zh-CN.md)

## Project scope

**In scope:** pure scanner + credential tester. Authorized internal recon,
asset discovery, weak-password detection, port inventory.

**Out of scope by design** (the project will not ship these, full stop):

- Vulnerability exploitation (CVE-based RCE, deserialization, auth bypass).
- Persistence / backdooring / lateral movement.
- Post-credential automation (running commands on a hit, dropping webshells,
  writing SSH keys).
- Exploit-framework features that duplicate other tools (Metasploit, Sliver,
  Cobalt Strike — all do this better; FG-QiMen deliberately doesn't compete).

The rationale lives in [`docs/SECURITY.md`](../SECURITY.md) (threat model +
HARD rules). The README's tagline ("More features ≠ better") is the one-line
version.

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

## Stage lifecycle + view projection

The scanner emits progress through a small state machine on
`internal/types.State` so the UI never has to peek at scanner
internals:

- **`Stage` enum** (`internal/types/state.go`): an `int32`
  with named constants `StageNone → StageAlive → StagePortScan
  → StageIdentify → StageCred → StageDone`. The scanner
  advances `State.Stage` as each phase starts/finishes. The TUI
  header's `[ ▶ STAGE ]` badge renders directly off this field.
- **`PluginHits` map** (`State.PluginHits[pluginID]int64`):
  per-plugin hit counters populated by the plugin workers as
  they fire. Used by the TUI's "top plugins" bar chart.
- **`ErrorCategories` map** (`State.ErrorCategories[category]int64`):
  per-category error counts (`timeout`, `refused`, `dns`, …)
  populated by `core.ClassifyError`.
- **`CountersView` projection** (`State.CountersView()`):
  returns a read-only snapshot struct so the view layer
  reads a stable contract instead of mutating shared maps.
  The TUI always reads through this — it does not touch
  `PluginHits` or `ErrorCategories` directly.
- **`core.ClassifyError(err)`** (`internal/core/errors.go`):
  classifies a scan error into a named bucket. The lookup
  order is `errors.Is`/`errors.As` against the typed sentinel
  set first, substring fallback last; the substring step is a
  safety net for raw `*net.OpError`/`syscall.ECONNREFUSED`
  strings that don't bubble up typed sentinels.
- **`alive.Progress()`** (`internal/core/alive/cmd.go`):
  public API for external callers to observe the
  mid-alive-sweep probe count without coupling to the
  scanner's channel layout. The TUI's "alive N/M" counter
  ticks off this.

Why this matters: the channel-decoupled pipeline above is
great for throughput but terrible for "what is the scan
doing right now?" visibility. The `Stage` enum +
`CountersView` projection is the bridge — it lets the UI
report concrete progress without re-deriving state from
noisy per-worker channels.

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
    │   ├── alive/                  host discovery (exposes
    │   │                          Progress() for mid-sweep counters)
    │   ├── scan/                   port scanner (drives Stage lifecycle,
    │   │                          populates PluginHits/ErrorCategories)
    │   ├── credential/             spray scheduler
    │   ├── errors/                 ClassifyError(err) -> category bucket
    │   ├── plugins/                Plugin interface + registry
    │   │   └── adapted/            30 built-in plugins
    │   ├── portscan/fingerprint/   Nmap PSL service fingerprint
    │   ├── discovery/              LAN-only ARP + NetBIOS
    │   ├── fakeserver/             shared in-process test doubles for
    │   │                          adapted-plugin tests
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
├── fg.db                  # bbolt state
├── targets.txt            # hand-editable target list (no fgqm_ prefix — operators edit it directly)
└── <YYYY-MM-DD>/
    ├── fgqm_result_HH-MM-SS.txt / .json / .csv / .sarif
    ├── fgqm_creds.txt     # always cleartext
    └── fgqm_rdp.json / .txt   # RDP deep fingerprint
```

The `HH-MM-SS` suffix (added in v0.5.1) is the local-time start
stamp, captured once at scan start so two same-day runs don't
clobber each other. All result files in a single scan share the
same suffix. Explicit paths via `-ot` / `-oj` / `-oc` bypass the
suffix.

The `fgqm_` prefix marks every result artifact as fg-qimen's,
so they stand out in mixed directories (`ls runs/` →
`fgqm_*.txt` is obviously the scanner's, not the app's). The
prefix is also a stable grep anchor: `grep -l 'fgqm_' runs/`
finds every result file across all projects and all dates.
`targets.txt` stays unprefixed because it's the one file
operators are expected to read and edit by hand.

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
