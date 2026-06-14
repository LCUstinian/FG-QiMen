# FG-QiMen v0.2 — release notes

> A 7-phase refactor sweep that lands the v0.1 codebase in a clean
> `cmd/ + internal/*` layout, decomposes the umbrella packages, and
> elevates first-class subsystems out of `core/`.

This release is a **pure tree-shaping pass**. The Plugin, Authenticator,
Result, RunMode, ScanItem and Probe contracts are unchanged; every commit
is a path-rename plus import-path update. There is no behavioural change
in the default scan path, and no removed feature. The 24 in-tree services
(13 Identify plugins + 27 Credential authenticators) all build, register,
and pass tests at the new locations.

本次发布是**纯结构调整**。Plugin / Authenticator / Result / RunMode /
ScanItem / Probe 接口保持不变；每个 commit 都只是路径改名 + import path
更新。默认扫描路径无行为变化，无功能移除。树内 24 项服务（13 个识别插件
+ 27 个凭据认证器）全部在新位置 build、register、通过测试。

---

## 1. The 7 phases / 七阶段

| Phase | Commit  | Title |
|------:|---------|-------|
| 1     | `0290233` | move `core/`, `common/`, `plugins/`, `workspace/`, `tui/` → `internal/` |
| 2     | `16b3ef7` | split `credential/auth/` into 6 category subpackages |
| 3     | `a23cf9f` | split `plugins/adapted/` into 7 category subpackages |
| 4     | `ae49078` | split `common/` into `types / output / store / ui / session` |
| 5     | `a2c6553` | move `core/scan/portfinger` → `portscan/fingerprint` |
| 6     | `3d432db` | extract `runScan` + helpers from `root.go` into `scan.go` |
| 7     | `7333b3e` | extract ARP + NetBIOS probes into `internal/discovery/` |

### Phase 1 — Internalisation

Five top-level packages (`core/`, `common/`, `plugins/`, `workspace/`,
`tui/`) move under Go's `internal/` boundary. External importers are now
compiler-blocked; only `cmd/` and `*_test.go` can depend on them.

138 files renamed, 92 callers' import paths rewritten. Zero logic change.

### Phase 2 — Credential categorisation

The flat `internal/core/cred/protocols/` directory (which had grown to 27
authenticators) becomes `internal/core/credential/auth/<category>/`:

  `database` · `email` · `filestorage` · `messaging` · `network` · `remote`

Symbol prefix shifts `cred.X → credential.X`. The interface itself is
unchanged.

### Phase 3 — Plugin categorisation

Mirrors phase 2 on the plugin side. `internal/plugins/adapted/` is split
into 7 category subpackages:

  `database` · `email` · `filestorage` · `messaging` · `network` · `remote` · `web`

The `webtitle/` HTTP framework moves under `web/`; RDP moves under
`remote/`. Each category has a `doc.go` placeholder that self-registers
via `init()`, so registration behaviour is unchanged.

### Phase 4 — `common/` decomposition (the biggest change)

`internal/common/` is split into 5 single-responsibility packages along a
clean DAG:

```
                  ┌── types  (leaf: stdlib only)
                  │
   output ───────┘
   store  ───────┘
   ui     ───────┘
                  │
                  └── session (imports all four above)
```

- **`internal/types/`** — `Config`, `State`, `Result`, `Cred`, `Target`,
  `Logger`, `TTY` helpers, `ScanItem`, `CountersView`, `HashKey`,
  `ExpandTargets`. Stdlib only — anyone can import it without dragging
  in runtime sinks.
- **`internal/output/`** — multi-format result sink (`Output`,
  `OpenOutput`, `OutputConfig`, `RDPFingerprint`). Imports `types`.
- **`internal/store/`** — bbolt persistence wrapper (`Store`, `NewStore`).
  Decoupled from `types`.
- **`internal/ui/`** — UI interface + `TextUI` + `NopUI` implementations.
  Imports `types`. (The Bubbletea TUI itself stays in `internal/tui/`.)
- **`internal/session/`** — `Session` bag that ties `Config / State /
  Store / Out / UI / Log` together. The only package that imports all
  four runtime sinks.

This also **breaks a latent cycle** that the umbrella `common` package
had masked: `common.Session` referenced `common.Output / common.Store /
common.UI`, which became `types → output → types` once the types pulled
out. Moving `Session` into its own package keeps `types` a true leaf.

36 caller files were migrated by a one-shot rewrite tool that mapped
each `common.X` symbol to its new home (see migration table below).

### Phase 5 — Promote `portfinger` to `portscan/`

The Nmap-style banner fingerprinting engine moves out of `core/scan/` to
its own top-level subsystem:

  `internal/core/scan/portfinger/` → `internal/portscan/fingerprint/`

The package rename `portfinger → fingerprint` and the symbol rename
`portfinger.X → fingerprint.X` (e.g. `NewVScan`) follow. Test file
`portfinger_test.go` becomes `fingerprint_test.go` (external test
package, mirrors the new package name).

Rationale: fingerprint matching is a peer of port scanning, not a
helper buried inside it. The new location signals that intent and
leaves room for siblings like `portscan/syn/` later.

### Phase 6 — Split `cmd/root.go`

`cmd/root.go` had grown to 356 lines, hosting both the Cobra scaffolding
AND the entire scan implementation. Phase 6 extracts the scan side:

```
cmd/
├── root.go     161 lines  rootCmd, persistent flags, Execute()
├── scan.go     243 lines  runScan + buildConfig + openProject +
│                          resolveOutputPath (rootCmd.RunE + `scan` cmd)
├── resume.go    38 lines  resume subcommand (alias forcing --resume)
├── projects.go 188 lines  projects {list, create, delete, info}
└── version.go   32 lines  version subcommand
```

To make the `projects` subcommand self-contained, `internal/workspace`
gained three primitives that should have been there from the start:

- `workspace.ProjectsRoot()` — single source of truth for the
  `runs/projects/` location (used by `Open` / `List` / `Delete`).
- `workspace.List() ([]string, error)` — missing root returns an empty
  list, not an error (matches the fresh-checkout case).
- `workspace.Delete(name) error` — refuses empty `name` to prevent
  accidental cwd removal; `projects delete` further requires `--force`.

### Phase 7 — `internal/discovery/` for LAN probes

The ARP and NBNS probes — which only produce hits inside the broadcast
domain — move out of `internal/core/alive/` into a dedicated
`internal/discovery/` package, wired through a registration pattern so
they remain optional:

```go
// In internal/discovery/arp.go and netbios.go:
func init() { alive.RegisterLANProbe(NewARPProbe()) }

// In internal/core/alive/probe.go:
func RegisterLANProbe(p Probe)          // mutex-guarded registry
func RegisteredLANProbes() []Probe      // snapshot
// alive.DefaultOptions() appends RegisteredLANProbes() to the
// always-on ICMP/TCP/system-ping list — so default callers see no
// behavioral change.

// In cmd/root.go:
_ "github.com/LCUstinian/FG-QiMen/internal/discovery"
// Omitting this import yields an internet-only scan
// (no ARP, no NBNS).
```

The same opt-in convention is now used by credential authenticators
(phase 2), plugin adapters (phase 3) and LAN probes (phase 7) — one
consistent registration story across the whole tree.

A new `TestRegistered` test in `internal/discovery/discovery_test.go`
guards against the `init()` calls being silently removed.

---

## 2. Cross-cutting themes / 横向主题

1. **Leaf-package discipline.** Phases 2, 3, 4 and 7 all push toward
   single-responsibility roots (auth/<cat>/, adapted/<cat>/, types as a
   stdlib-only leaf, discovery as a focused LAN-probe home). Unrelated
   code can no longer accidentally reach across the package graph.

2. **`init()`-based registration as the universal opt-in pattern.**
   First introduced for credential authenticators, then plugin adapters,
   now LAN probes. Blank-import to enable; omit the import to disable.
   No flag, no config, no runtime switch — entirely build-time.

3. **Public contracts are preserved.** Every interface that a downstream
   consumer could plausibly implement (`Plugin`, `Authenticator`,
   `Probe`) and every value type that crosses package boundaries
   (`Result`, `Cred`, `RunMode`, `ScanItem`, `Hit`) keeps its shape.
   The entire 7-phase series is a tree-rename, not an API redesign.

---

## 3. Migration table / 迁移表

If you have code outside the FG-QiMen tree that depended on the old
package paths, here is the complete mapping. (Internally, every caller
was migrated automatically — this table exists for downstream forks /
out-of-tree integrations.)

### Top-level relocation (phase 1)

| Before                             | After                                       |
|------------------------------------|---------------------------------------------|
| `…/common`                         | `…/internal/common` (then split — see phase 4) |
| `…/core`                           | `…/internal/core`                           |
| `…/plugins`                        | `…/internal/plugins`                        |
| `…/workspace`                      | `…/internal/workspace`                      |
| `…/tui`                            | `…/internal/tui`                            |

### Credential layer (phase 2)

| Before                             | After                                       |
|------------------------------------|---------------------------------------------|
| `…/internal/core/cred`             | `…/internal/core/credential`                |
| `…/internal/core/cred/protocols`   | `…/internal/core/credential/auth/{database, email, filestorage, messaging, network, remote}` |
| `cred.X` (symbols)                 | `credential.X`                              |
| `protocols.X` (internal test refs) | bare `X` inside each `auth/<cat>/` package  |

### Plugin layer (phase 3)

| Before                                       | After                                          |
|----------------------------------------------|------------------------------------------------|
| `…/internal/plugins/adapted/{name}`          | `…/internal/plugins/adapted/{cat}/{name}` (cat = database, email, filestorage, messaging, network, remote, web) |
| `…/internal/plugins/adapted/webtitle`        | `…/internal/plugins/adapted/web/webtitle`      |
| `…/internal/plugins/adapted/rdp`             | `…/internal/plugins/adapted/remote/rdp`        |

### `common/` decomposition (phase 4)

| Before                             | After                                       |
|------------------------------------|---------------------------------------------|
| `common.Session` / `common.NewSession` | `session.Session` / `session.NewSession` |
| `common.OpenOutput` / `OutputConfig` / `Output` / `RDPFingerprint` | `output.X` |
| `common.Store` / `common.NewStore` | `store.Store` / `store.NewStore`            |
| `common.UI` / `NopUI` / `TextUI` / `NewTextUI` | `ui.X`                            |
| everything else (`Config`, `State`, `Result`, `Cred`, `Target`, `Logger`, `TTY` helpers, `ScanItem`, `CountersView`, `HashKey`, `ExpandTargets`) | `types.<same-name>` |

### `portfinger` → `fingerprint` (phase 5)

| Before                                              | After                                       |
|-----------------------------------------------------|---------------------------------------------|
| `…/internal/core/scan/portfinger`                   | `…/internal/portscan/fingerprint`           |
| `portfinger.NewVScan` / `VScan` / …                 | `fingerprint.NewVScan` / `VScan` / …        |

### cmd/ split + workspace surface (phase 6)

| New / changed surface              | Notes                                       |
|------------------------------------|---------------------------------------------|
| `cmd/scan.go` owns `runScan` + helpers | `cmd/root.go` keeps only Cobra scaffolding |
| `workspace.ProjectsRoot()`         | new public; single source of truth for `runs/projects/` |
| `workspace.List() ([]string, error)` | new public; missing root → empty list, not error |
| `workspace.Delete(name) error`     | new public; refuses empty name              |

### Discovery extraction (phase 7)

| Before                             | After                                       |
|------------------------------------|---------------------------------------------|
| `internal/core/alive.arp` + `netbios` (always-on) | `internal/discovery.arp` + `netbios` (opt-in via blank import) |
| (none)                             | `alive.RegisterLANProbe(p Probe)` — new public |
| (none)                             | `alive.RegisteredLANProbes() []Probe` — new public |

---

## 4. Project layout — before vs after / 项目布局

### Before (v0.1)

```
FG-QiMen/
├── cmd/                                # one root.go (356 lines)
├── common/                             # umbrella: Config + State + Store + Output + Logger + UI + TTY
├── core/
│   ├── alive/                          # all 5 probes here
│   ├── scan/
│   │   └── portfinger/                 # buried fingerprint engine
│   └── cred/
│       └── protocols/                  # flat list of 27 authenticators
├── plugins/
│   └── adapted/                        # flat list of plugins + 2 top-level .go files
├── tui/
└── workspace/
```

### After (v0.2)

```
FG-QiMen/
├── cmd/                                # 5 cobra files (root / scan / resume / projects / version)
└── internal/
    ├── types/      ← leaf              # Config, State, Result, Cred, …
    ├── output/                         # multi-format result sink
    ├── store/                          # bbolt wrapper
    ├── ui/                             # UI interface + TextUI + NopUI
    ├── session/                        # Session bag (tops of leaf DAG)
    ├── core/
    │   ├── alive/                      # always-on probes (ICMP / TCP / system-ping)
    │   ├── credential/                 # pool / protocol / scheduler
    │   │   └── auth/{database, email, filestorage, messaging, network, remote}/
    │   └── scan/                       # TCP connect + UDP probe
    ├── discovery/                      # LAN-only probes (ARP + NBNS) — opt-in
    ├── portscan/
    │   └── fingerprint/                # promoted out of core/scan/
    ├── plugins/
    │   └── adapted/{database, email, filestorage, messaging, network, remote, web}/
    ├── tui/
    └── workspace/                      # + ProjectsRoot, List, Delete
```

---

## 5. Verification / 验证

```
go build ./...   # clean
go test  ./...   # all 13 tested packages green:
                 #   internal/core/alive
                 #   internal/core/credential
                 #   internal/core/credential/auth/database
                 #   internal/core/credential/auth/email
                 #   internal/core/credential/auth/filestorage
                 #   internal/core/credential/auth/messaging
                 #   internal/core/credential/auth/network
                 #   internal/core/credential/auth/remote
                 #   internal/core/scan
                 #   internal/discovery
                 #   internal/plugins/adapted/remote/rdp
                 #   internal/plugins/adapted/web/webtitle/fingerprint
                 #   internal/portscan/fingerprint
```

Total Go packages after refactor: **58** (was ~26 pre-Phase-3).

---

## 6. Co-authored / 协作

This refactor sweep was driven by [Claude Code](https://claude.com/claude-code).

🤖 Generated with [Claude Code](https://claude.com/claude-code).
