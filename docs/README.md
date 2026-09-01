# FG-QiMen documentation

This is the canonical index for FG-QiMen's user-facing and developer-facing
documentation. The structure is:

```
docs/
├── README.md                    ← you are here
│
├── ARCHITECTURE.md              ← Pipeline design, plugin contract, session bag
├── CONFIGURATION.md             ← All 28 CLI flags by area, usage templates
├── PLUGIN_GUIDE.md              ← How to write a new plugin / authenticator
├── SECURITY.md                  ← HARD rule + the no-exploit contract
├── RELEASE.md                   ← How to cut a release (tag → multi-platform)
│
├── verification/                ← Per-version reports
│   ├── v0.2/
│   │   └── release-notes.md     ← v0.2 release notes (historical)
│   ├── v0.3/
│   │   ├── first-batch-verification.md
│   │   └── second-batch-verification.md
│   ├── v0.4/
│   │   ├── verification.md
│   │   └── benchmarks.md
│   └── v0.5/
│       └── verification.md       ← scheduled scan + persistent schedules
│
├── design/                      ← Design specs (date-stamped)
│   ├── 2026-06-13-db-cred-rdp-fingerprint-design.md
│   ├── 2026-07-22-high-priority-fixes-design.md
│   └── 2026-07-22-high-priority-fixes.md
│
└── archive/                     ← Historical; do not link from README
    ├── audits/                  ← Foundational audit reports
    │   ├── comprehensive-audit-report.md
    │   ├── audit-report-vs-fscan.md
    │   ├── optimization-plan-comprehensive.md
    │   ├── optimization-plan.md
    │   └── audit-fixes-completion-report.md
    └── optimization-journal/    ← Phase-by-phase progress journal
        ├── final-delivery-report.md
        ├── final-summary.md
        ├── final-summary-v2.md
        ├── short-term-completion-report.md
        ├── mid-term-strategy-adjustment.md
        ├── overall-progress.md
        ├── p1-p2-optimization-report.md
        ├── progress-report.md
        ├── session-summary.md
        ├── stage1-completion-report.md
        ├── stage2-completion-report.md
        ├── stage2-final.md
        └── stage3-completion-report.md
```

## User-facing docs (top level)

These are linked from the project root [README.md](../README.md). They
describe what the tool is and how to use it.

| File | Purpose |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Pipeline design (4 stages: alive → scan → identify → spray), plugin contract (Identify + Credential dual-mode interface), session bag wiring, bbolt project workspace |
| [CONFIGURATION.md](CONFIGURATION.md) | All 28 CLI flags grouped by area (Target / Workspace / Ports / Network / Concurrency / Credentials / Output / Behavior / Safety) + usage templates |
| [PLUGIN_GUIDE.md](PLUGIN_GUIDE.md) | How to write a new plugin or authenticator; the `Plugin` interface contract; how to register for Identify / Credential modes |
| [SECURITY.md](SECURITY.md) | The HARD no-exploit rule; list of forbidden capabilities; the audit-driven security posture; SSH host-key handling; in-memory-only credential storage |
| [RELEASE.md](RELEASE.md) | Pre-release checklist; the tag-triggered release pipeline; the 11-platform matrix; how SHA256SUMS, cosign, and SBOMs are produced |

## Verification (per version)

Each version folder holds the verification / benchmark artifacts that
landed in that version. These are not linked from the user README — they
exist for reviewers and the audit trail.

- [v0.2/release-notes.md](verification/v0.2/release-notes.md) — v0.2 release notes
- [v0.3/first-batch-verification.md](verification/v0.3/first-batch-verification.md) — first high-priority fix batch (P0 race coverage, 24h pool cleanup, etc.)
- [v0.3/second-batch-verification.md](verification/v0.3/second-batch-verification.md) — second audit batch (P1-3 per-attempt deadlines, P2-7 fsync, P3-5 MySQL cache invalidation, etc.)
- [v0.4/verification.md](verification/v0.4/verification.md) — v0.4 deferred features
- [v0.4/benchmarks.md](verification/v0.4/benchmarks.md) — performance baseline (Ultra 9 285K, Windows 11, Go 1.26)

## Design specs (date-stamped)

| File | Date | Purpose |
|---|---|---|
| [2026-06-13-db-cred-rdp-fingerprint-design.md](design/2026-06-13-db-cred-rdp-fingerprint-design.md) | 2026-06-13 | DB credential completion + RDP deep fingerprint design |
| [2026-07-22-high-priority-fixes-design.md](design/2026-07-22-high-priority-fixes-design.md) | 2026-07-22 | Design for the first high-priority fix batch (zh) |
| [2026-07-22-high-priority-fixes.md](design/2026-07-22-high-priority-fixes.md) | 2026-07-22 | Implementation plan for the first batch (zh) |

## Archive

`archive/` holds two kinds of historical material:

- **`audits/`** — the four foundational documents from the 2026-06 audit
  pass. `comprehensive-audit-report.md` is the source of truth; the other
  three are the vs-fscan comparison, the optimization plan, and the
  fixes-completion report.
- **`optimization-journal/`** — the phase-by-phase progress journal from
  the 2026-06 optimization pass. Most of these are intermediate status
  reports; `final-delivery-report.md` and `final-summary-v2.md` are the
  closest things to a TL;DR.

These are kept for historical reference but **not** linked from anywhere
in the active docs tree. If you're trying to understand what shipped in
v0.3 or later, look in `verification/`, not `archive/`.
