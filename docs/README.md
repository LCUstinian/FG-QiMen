# FG-QiMen documentation

> [中文版本](README.zh-CN.md)

This is the canonical index for FG-QiMen's user-facing and developer-facing
documentation. The repository layout is:

```
docs/
├── README.md                    ← you are here
│
├── ARCHITECTURE.md              ← Pipeline design, plugin contract, session bag
├── ARCHITECTURE.zh-CN.md
├── CONFIGURATION.md             ← CLI flag areas + usage templates (full table: FLAGS.md)
├── CONFIGURATION.zh-CN.md
├── FLAGS.md                     ← GENERATED: full flag table from the live registry
├── FLAGS.zh-CN.md               ← GENERATED: Chinese twin of FLAGS.md
├── PLUGINS.md                   ← GENERATED: full plugin roster from the live registry
├── PLUGINS.zh-CN.md             ← GENERATED: Chinese twin of PLUGINS.md
├── PLUGIN_GUIDE.md              ← How to write a new plugin / authenticator
├── PLUGIN_GUIDE.zh-CN.md
├── SECURITY.md                  ← HARD rule + the no-exploit contract
├── SECURITY.zh-CN.md
├── RELEASE.md                   ← How to cut a release (tag → multi-platform)
└── RELEASE.zh-CN.md
```

Every doc exists as an English/Simplified-Chinese pair (`*.md` /
`*.zh-CN.md`) kept structurally in sync. The two `GENERATED` files are
build products of `just docs-gen` — never hand-edited; a CI guard test
(internal/docgen) fails the moment they drift from the live flag/plugin
registries.

## User-facing docs (top level)

These are linked from the project root [README.md](../README.md). They
describe what the tool is and how to use it.

| File | Purpose |
|---|---|
| [FLAGS.md](FLAGS.md) / [FLAGS.zh-CN.md](FLAGS.zh-CN.md) | **Generated** full flag table — one section per group, rendered from the same pflag registry `fg-qimen --help` serves. Regenerate with `just docs-gen`; a CI guard test fails when this file drifts, and every flag must carry a Chinese usage translation. |
| [PLUGINS.md](PLUGINS.md) / [PLUGINS.zh-CN.md](PLUGINS.zh-CN.md) | **Generated** plugin roster — name, default ports, Identify/Credential capabilities, from the live plugin + authenticator registries. Same guard as FLAGS.md. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Pipeline design (4 stages: alive → scan → identify → spray), plugin contract (Identify + Credential dual-mode interface), session bag wiring, bbolt project workspace |
| [CONFIGURATION.md](CONFIGURATION.md) | Flag areas (Target / Workspace / Ports / Network / Concurrency / Credentials / Output / Schedule / Behavior / Safety) + per-workflow usage templates |
| [PLUGIN_GUIDE.md](PLUGIN_GUIDE.md) | How to write a new plugin or authenticator; the `Plugin` interface contract; how to register for Identify / Credential modes |
| [SECURITY.md](SECURITY.md) | The HARD no-exploit rule; list of forbidden capabilities; the audit-driven security posture; SSH host-key handling; in-memory-only credential storage |
| [RELEASE.md](RELEASE.md) | Pre-release checklist; the tag-triggered release pipeline; the 11-platform matrix; how SHA256SUMS, cosign, and SBOMs are produced |

## Internal working docs (local-only)

A few directories under `docs/` are **not committed to the repository**
(gitignored) and are therefore not linked here: `verification/`
(per-version verification and benchmark reports), `design/` (date-stamped
design specs), `superpowers/` (iteration plans from agent sessions), and
`archive/` (historical audit reports and progress journals). They stay on
contributor machines and in the CHANGELOG's plain-text path references;
if you need one, ask a maintainer or reconstruct it from the CHANGELOG
entries, which summarize each report's findings.
