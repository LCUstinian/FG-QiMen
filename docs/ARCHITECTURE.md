# Architecture

> [中文版本](ARCHITECTURE.zh-CN.md)

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
  scanner's channel layout. The TUI's progress ledger ticks
  off this.

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
    │   │   └── adapted/            44 built-in plugins (8 category packages)
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
fgqm_workspace/projects/<name>/
├── fgqm.db                # bbolt state
├── targets.txt            # hand-editable target list (no fgqm_ prefix — operators edit it directly)
└── <YYYY-MM-DD>/
    ├── fgqm_result_HH-MM-SS.txt / .ndjson / .csv / .sarif
    ├── fgqm_creds.txt     # always cleartext
    └── fgqm_rdp.ndjson / .txt   # RDP deep fingerprint
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

The scan path is self-tuning end to end. Four mechanisms stack:

### AIMD thread pool (`internal/core/scan/pool.go`)

- **Slow start**: the pool is born at `max(floor, target/4)` and
  doubles per healthy adjustment interval until it reaches the
  AIMD target (`InitialThreads`).
- **Steady-state AIMD**: additive increase `+target/20` while
  healthy; multiplicative decrease `×0.85` under stress, `×0.5`
  under congestion. Health signals are the resource-exhaustion
  rate (EMFILE-style dial errors) first and the fast/slow EMA
  RTT trend second — a high filtered/closed ratio is *network
  truth*, not scanner overload, and never triggers a shrink
  (the v0.3-era heuristic that avalanched concurrency to 1).
- **Silent segments**: a window with no open/refused responses
  demotes Good to OK so a firewalled /24 can't be read as
  headroom.
- **Floor**: concurrency never drops below
  `max(MinThreads, MaxThreads/20)`; `DefaultMinThreads=50`
  comes from the avalanche post-mortem. `--threads` is a hard
  cap — the pool never scales past it.
- A 50 ms wake ticker drives the controller loop; the producer
  uses capped exponential backoff (1 ms → 50 ms) so saturation
  doesn't peg a core.
- `RetryableProbe` (`internal/core/scan/retry.go`) absorbs
  transient resource-exhaustion errors with exponential backoff
  before they can reach the congestion controller; retry
  failure >20% logs a warning that the process is likely FD /
  socket-quota bound.

### Adaptive per-probe timeout (`internal/core/scan/adaptive_timeout.go`)

Every probe that actually reached the host (handshake completed
or refused-RST) records its RTT into a 64-sample ring buffer.
The single-probe timeout becomes `mean + 4σ`, clamped to
`[max(500ms, base/5), base]`: on a fast LAN a 3 s filtered-port
wait shrinks to ~600 ms (≈5× faster sweeps), while the ceiling
stays at the operator's (possibly profile-tuned) base. Warmup
(10 samples) returns base. Silent UDP probes report `RTT=0`
and never feed the sampler. An explicit `--timeout` disables
the whole mechanism — an explicit value is always the hard
ceiling.

### Network environment profiling (`internal/core/envprobe.go`)

Before the pipeline opens the scan phase, up to 10 hosts are
sampled evenly across the target list with cheap TCP connects
(refused counts as a valid RTT) and the path is classified
LAN (<20 ms median) / WAN (20-200 ms) / Internet (>200 ms) /
Slow. The profile auto-tunes `--timeout` and `--threads` — but
only for values the operator did **not** set explicitly. Zero
samples (e.g. every sampled port is closed) default to WAN.

### Segment pre-screening (`internal/core/scan/prescreen.go`)

Inputs larger than `PrescreenThreshold` (256) addresses are
pre-screened before the full scan: phase 1 probes the gateway
(.1/.254) of each /24 with rotating ports; segments whose
gateways all miss get a bounded phase-2 fallback (first
`Phase2SampleCap=64` hosts, one rotating port each) before
being skipped. Single-subnet inputs are never filtered. The
residual false-negative window (a live host beyond position 64
in an otherwise-silent segment) is documented; the kill switch
is `PrescreenOptions.Phase2=false`.

### Misc

- `LoadSeenHashes` pre-allocates using `bk.Stats().KeyN` so 100k
  resumes avoid 17 log-2 reallocs.
- Output sink uses 6 per-sink mutexes so a slow sink doesn't
  head-of-line block the others.
- The UDP phase is serial after TCP with its own fixed pool
  (800/800 threads, 2 s probe timeout), so UDP's long silent
  waits can't perturb the TCP controller.

## Tradeoffs

- **Pool dedup key is HMAC-hashed, but cleartext is still in the
  heap** — process memory dumps can recover pre-GC strings.
  Documented in `docs/SECURITY.md`.
- **TUI is on by default on TTY stdout.** Non-TTY stdout (CI,
  scripts) gets the text logger; `--no-tui` forces it anywhere.
- **`RawTCPIdentify` is a thin wrapper** — doesn't abstract every
  protocol. UDP fallback (SNMP) and TLS probe (HTTPS) still write
  their own dial loop.
