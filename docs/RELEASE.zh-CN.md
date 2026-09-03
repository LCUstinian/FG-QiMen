# Release Procedure
## Pre-release checklist
## Cutting a release
# 1. Make sure main is up to date and clean
# 2. Confirm CHANGELOG.md has the new version block
# 3. Tag. The release.yml `on.push.tags: - 'v*'` matches any v-prefixed
#    semver tag. /打 tag。release.yml 的 `on.push.tags: - 'v*'` 匹配任
#    意 v 前缀的 semver tag。
# 4. Push the tag. This triggers the release pipeline. /推送 tag。
#    这触发 release 流水线。
## Verifying a release
# Download SHA256SUMS and verify each binary.
# Verify one binary's cosign keyless signature. The OIDC identity
# pinned here must match the GitHub Actions workflow that built the
# release.
# Verify the SLSA provenance attestation (downloadable as
# `provenance/*.intoto.jsonl` artifacts).
# Pull the container image.
## Prerelease tags
## Manual dry-run
## Local build verification
## Pinning the version (post-release)
## Troubleshooting
### A platform build fails
### Cosign fails
### ghcr.io push fails
### Tag was pushed but no release appeared
## Cut a release checklist (TL;DR)
# 1. Pre-flight
# Confirm: CHANGELOG updated, version bumped, tests green
# 2. Tag
# 3. Watch
# https://github.com/LCUstinian/FG-QiMen/actions
# 4. Verify on a workstation
## Cross-references
