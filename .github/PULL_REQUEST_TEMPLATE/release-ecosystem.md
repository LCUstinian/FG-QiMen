# PR body: ci/release-ecosystem-fixes → main

**URL to create PR:** https://github.com/LCUstinian/FG-QiMen/pull/new/ci/release-ecosystem-fixes

---

## Background

Six open Dependabot PRs and recent main pushes are all failing CI. The root cause is not the dependency upgrades themselves — it's that remote `main` is missing 4 locally-ready CI fixes. This PR packages them together so they can be reviewed as a unit instead of pushed directly to `main`.

## What's in this PR

4 commits, all originally on local `main` (Co-Authored-By: Claude, previously reviewed by the maintainer):

### 1. `ci(release): fix 3 actionlint errors blocking all release runs`
- `release.yml`: `runner: macos-13` → `macos-14` (no `macos-13` runner label exists)
- `scoop-bucket.yml`: `${{ envwin_sha256 }}` → `${{ env.WIN_SHA256 }}` (typo — GHA expression context is `env.`, not `env`)
- `container.yml`: cosign sign step missing `id: build`; reference `${{ steps.build.outputs.digest }}` was unaddressable
- All three now caught by `actionlint` running on every PR via `workflow-lint.yml`

### 2. `ci(release): drop unnecessary QEMU + bump syft + bump attest-provenance`
- Drop `docker/setup-qemu-action` entirely: Go cross-compile writes files, never executes them, so QEMU user-mode emulation is not needed. Eliminates a 2-3 min setup cost per matrix entry and one source of BSD-target cross-arch flakiness.
- `syft` v1.0.0 → v1.51.0 (SBOM schema has evolved since 2024)
- `actions/attest-build-provenance` v1 → v4 (SLSA v1.0 fixes + GitHub-recommended output binding)

### 3. `fix: go mod tidy promotes 5 indirect deps to direct`
- `ci.yml`'s `go mod tidy check` step (`go mod tidy && git diff --exit-code go.mod go.sum`) currently produces a 21-line diff on remote main, failing the entire `vet + test` job before vet/test/coverage ever runs.
- The diff promotes 5 indirect deps to direct (no API impact, just hygiene).

### 4. `fix(ci): workflow-lint was using broken docker:// syntax`
- `workflow-lint.yml` used `uses: docker://rhysd/actionlint:latest`; ubuntu-latest runners don't have Docker daemon by default.
- Replaced with the official actionlint download script — works on every GH-hosted runner, no Docker dependency.
- Bumped `ludeeus/action-shellcheck: 2.0.0` → `@master` (v2.0.0 is from 2022 and predates the v1 schema).
- `scandot` now points at `scripts/**/*.sh`.

## Why this is a separate PR

These commits existed locally before this session and were authored by the maintainer. Splitting them from the fuzz fix lets the bug fix review land independently of CI hygiene changes. Once this merges, Dependabot PRs #1–#6 just need to be rebased (or Dependabot will auto-rebase) to turn green.

## Notes for reviewers

- The merge commit `79792d6 Merge branch 'worktree-release-ecosystem' into main` is flattened in this branch — the merge only carried `d519dfe`'s changes to main, so the linear history preserves file state exactly. Diff against `FG-QiMen/main`: +54 / -32 across 6 files, matches the accumulated diff of all 5 unpushed commits on local `main`.
- All commits preserve their original authorship and Co-Authored-By trailers.