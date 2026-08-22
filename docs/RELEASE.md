# Release Procedure

> v0.3.1+ — multi-platform automated release pipeline.
> v0.3.1+ — 多平台自动发布流水线。

This document describes how to cut a release of FG-QiMen. The pipeline
is fully automated; a tag push is the only manual trigger.

本文档描述如何发布 FG-QiMen 新版本。流水线完全自动化；tag push
是唯一的触发点。

## Pre-release checklist

1. All PRs targeting the release milestone are merged.
2. `main` branch is green: `ci.yml` lint + test + coverage all pass.
3. `CHANGELOG.md` has a `[VERSION]` section with the user-visible
   deltas (use the second-batch verification report as a template).
4. `internal/version/version.go` `Value` is bumped to the release
   version. The release.yml's `derive version` step overrides it at
   build time via `-ldflags`, so this is informational only.

## Cutting a release

```bash
# 1. Make sure main is up to date and clean
git checkout main
git pull --ff-only
git status

# 2. Confirm CHANGELOG.md has the new version block
head -30 CHANGELOG.md

# 3. Tag. The release.yml `on.push.tags: - 'v*'` matches any v-prefixed
#    semver tag. /打 tag。release.yml 的 `on.push.tags: - 'v*'` 匹配任
#    意 v 前缀的 semver tag。
git tag -a v0.3.1 -m "v0.3.1 — one-line description"

# 4. Push the tag. This triggers the release pipeline. /推送 tag。
#    这触发 release 流水线。
git push origin v0.3.1
```

The push triggers four GitHub Actions workflows:

| Workflow            | Trigger                  | Action |
|---------------------|--------------------------|--------|
| `release.yml`       | tag push (`v*`)          | 11-platform build + cosign + SBOM + SLSA L2 + GitHub Release |
| `container.yml`     | tag push (`v*`)          | ghcr.io multi-arch OCI image + cosign |
| `ci.yml`            | every push + PR          | regular test matrix against the tag |
| `workflow-lint.yml` | PRs touching .github/    | actionlint + shellcheck |

## Verifying a release

After the workflows complete (~25-40 minutes total — 11 platform
builds run in parallel), the GitHub Release page shows:

- 11 binaries + their `.sig` / `.pem` / `.sbom.json` pairs
- One `SHA256SUMS` covering all 11 binaries
- One OCI image pushed to `ghcr.io/<owner>/fg-qimen:<version>`
  (with `latest` tag also refreshed)

To verify the artifacts on your workstation:

```bash
# Download SHA256SUMS and verify each binary.
sha256sum -c SHA256SUMS

# Verify one binary's cosign keyless signature. The OIDC identity
# pinned here must match the GitHub Actions workflow that built the
# release.
COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --certificate fg-qimen-linux-amd64.pem \
  --signature    fg-qimen-linux-amd64.sig \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-linux-amd64

# Verify the SLSA provenance attestation (downloadable as
# `provenance/*.intoto.jsonl` artifacts).
cosign verify-attestation \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  fg-qimen-linux-amd64.intoto.jsonl

# Pull the container image.
docker pull ghcr.io/<owner>/fg-qimen:v0.3.1
docker run --rm ghcr.io/<owner>/fg-qimen:v0.3.1 --help
```

## Prerelease tags

Tags containing a `-` (e.g. `v0.3.1-rc1`, `v0.3.1-beta2`) are
published as **prerelease** GitHub Releases. Use them for
release-candidate smoke-testing before the final cut.

## Manual dry-run

The `workflow_dispatch` trigger on `release.yml` and `container.yml`
lets you run the pipeline without a tag. Use this to verify the
workflow still works after a refactor.

## Local build verification

To reproduce one target locally (Linux/amd64):

```bash
SOURCE_DATE_EPOCH=1700000000 \
go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X github.com/LCUstinian/FG-QiMen/internal/version.Value=v0.3.1" \
  -o fg-qimen-linux-amd64 .
```

The binary's `version` subcommand should print `v0.3.1`.

## Pinning the version (post-release)

After the tag is pushed and the release is live:

- Update `internal/version/version.go` `Value` to the next development
  version (e.g. `0.3.2-dev`).
- Open a CHANGELOG `[Unreleased]` section in `CHANGELOG.md`.
- Continue normal development on `main`.

This keeps `git log v0.3.1..main` clean for the next release.

## Troubleshooting

### A platform build fails

Re-run the matrix entry individually via the GitHub Actions UI
("Re-run jobs → re-run failed jobs"). Most common cause: a new Go
target needs a build flag (e.g. GOARM) that the matrix entry doesn't
provide. Update the matrix in `release.yml`.

### Cosign fails

Check `cosign sign-blob` output. Common cause: the OIDC `id-token`
permission isn't granted in the workflow — verify the `permissions:`
block. If the OIDC issuer URL changed (Sigstore policy change),
update `release.yml` to match.

### ghcr.io push fails

The workflow uses the auto-generated `GITHUB_TOKEN` for ghcr.io. If
the org's "Allow GitHub Actions to create and approve pull requests"
setting is disabled, this fails. Fix at the org settings page.

### Tag was pushed but no release appeared

Check the Actions tab on the tag. If the `release` job failed,
inspect the failure. If the workflow never ran, verify the tag name
matches `v*` and that `release.yml`'s `on.push.tags` is correctly
configured.

## Cut a release checklist (TL;DR)

```bash
# 1. Pre-flight
git checkout main && git pull --ff-only
# Confirm: CHANGELOG updated, version bumped, tests green

# 2. Tag
git tag -a vX.Y.Z -m "vX.Y.Z — description"
git push origin vX.Y.Z

# 3. Watch
# https://github.com/LCUstinian/FG-QiMen/actions

# 4. Verify on a workstation
sha256sum -c SHA256SUMS
cosign verify-blob --certificate ... --signature ...
docker pull ghcr.io/<owner>/fg-qimen:vX.Y.Z
```

## Cross-references

- Workflows: `.github/workflows/release.yml`, `container.yml`,
  `workflow-lint.yml`, `ci.yml`, `homebrew-tap.yml`, `scoop-bucket.yml`
- Per-batch verification: `docs/FIRST_BATCH_VERIFICATION.md`,
  `SECOND_BATCH_VERIFICATION.md`, `V0.4_VERIFICATION.md`
- Release notes: `CHANGELOG.md`
- Dockerfile: `.github/docker/Dockerfile`
- Benchmark baselines: `docs/BENCHMARKS_v0.4.md`