# 发布流程

> [English version](RELEASE.md)

当前状态——多平台自动发布流水线。
> current — multi-platform automated release pipeline.

本文档描述如何发布 FG-QiMen 新版本。流水线完全自动化；tag push
是唯一的触发点。

The document describes how to cut a release of FG-QiMen. The pipeline
is fully automated; a tag push is the only manual trigger.

## 发布前检查清单

1. 发布 milestone 的所有 PR 已合并。
2. `main` 分支全绿：`ci.yml` 的 lint + test + coverage 全部通过。
3. `CHANGELOG.md` 已有新版本的 `[VERSION]` 小节，写明用户可见的
   变更（可参考 docs/verification/ 下的验证报告模板）。
4. `internal/version/version.go` 的 `Value` 已提升到发布版本。
   release.yml 的构建步骤会在构建时通过 `-ldflags` 覆盖它，所以
   这一步只是信息性的。

## 发布步骤

```bash
# 1. 确保 main 最新且工作区干净
git checkout main
git pull --ff-only
git status

# 2. 确认 CHANGELOG.md 有新版本小节
head -30 CHANGELOG.md

# 3. 打 tag。release.yml 的 `on.push.tags: - 'v*'` 匹配任意
#    v 前缀的 semver tag。任一符合的 tag 都会触发发布，
#    无需额外配置。
git tag -a v0.3.1 -m "v0.3.1 — one-line description"

# 4. 推送 tag。这会触发 release 流水线。
#    流水线完成后在 Actions 页确认全绿。
git push origin v0.3.1
```

tag push 触发四个 GitHub Actions 工作流：

| 工作流              | 触发条件                 | 动作 |
|---------------------|--------------------------|------|
| `release.yml`       | tag push (`v*`)          | 13 个产物构建（11 个标准平台 + 2 个加固版）+ cosign + SBOM + SLSA L2 + GitHub Release |
| `container.yml`     | tag push (`v*`)          | ghcr.io 多架构 OCI 镜像 + cosign |
| `ci.yml`            | 每次 push + PR           | 对该 tag 跑常规测试矩阵 |
| `workflow-lint.yml` | 触碰 .github/ 的 PR      | actionlint + shellcheck |

## 两个版本

每次发布同时包含**标准版**和**加固版**二进制：

- **标准版**（`fg-qimen-<platform>`，11 个平台）：纯净 `go build`
  加 `-trimpath`，通过 `SOURCE_DATE_EPOCH` 可复现。除非有特殊理由，
  一律用标准版——它可以从 tag 逐字节重建。
- **加固版**（`fg-qimen-<platform>-hardened`，仅 linux-amd64 +
  windows-amd64）：garble `-seed=random` 混淆、UPX `--best --lzma`
  压缩、UPX 特征消除（`scripts/strip_upx.py`）。刻意不可复现——
  每次构建 SHA256 都不同，对抗基于文件哈希的识别。预期有 50-200 ms
  启动延迟和更高的杀软误报率；cosign 签名仍然可以证明真实性。加固版
  跑同样的 smoke test，并获得与标准版相同的 cosign 签名、逐二进制
  SBOM 和 SLSA 证明。

## 验证发布

工作流完成后（总计约 25-40 分钟——13 个构建并行跑），GitHub
Release 页面会展示：

- 13 个二进制（11 个标准版 + 2 个加固版）及各自的 `.sig` / `.pem` /
  `.sbom.json`
- 一个覆盖全部 13 个二进制的 `SHA256SUMS`
- 一份覆盖全部发布产物的全量 SPDX SBOM
  （`FG-QiMen-release.spdx.json`）
- 一个推送到 `ghcr.io/<owner>/fg-qimen:<version>` 的 OCI 镜像
  （`latest` tag 同步刷新）

在工作站上验证产物：

```bash
# 下载 SHA256SUMS 并校验每个二进制（13 条：11 个标准版 +
# 2 个加固版）。加固版产物名带 -hardened 后缀，校验方式
# 与标准版完全一致。
sha256sum -c SHA256SUMS

# 校验一个标准版二进制的 cosign keyless 签名。这里固定的 OIDC
# 身份必须与构建发布的 GitHub Actions 工作流一致。
COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --certificate fg-qimen-linux-amd64.pem \
  --signature    fg-qimen-linux-amd64.sig \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-linux-amd64

# 加固版的校验方式完全相同——同一流水线、同一 OIDC 身份，只有产物名
# 带 -hardened 后缀的区别。
COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --certificate fg-qimen-windows-amd64-hardened.exe.pem \
  --signature    fg-qimen-windows-amd64-hardened.exe.sig \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-windows-amd64-hardened.exe

# 校验 SLSA provenance 证明（可在 `provenance/*.intoto.jsonl`
# artifact 中下载）。
cosign verify-attestation \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  fg-qimen-linux-amd64.intoto.jsonl

# 拉取容器镜像。
docker pull ghcr.io/<owner>/fg-qimen:v0.3.1
docker run --rm ghcr.io/<owner>/fg-qimen:v0.3.1 --help
```

## 预发布标签

带 `-` 的 tag（如 `v0.3.1-rc1`、`v0.3.1-beta2`）会发布为
**prerelease** GitHub Release。用于正式发布前的候选版本冒烟测试。

## 手动 dry-run

`release.yml` 和 `container.yml` 的 `workflow_dispatch` 触发器让你
不打 tag 也能跑流水线。重构工作流之后用它验证流水线仍然可用。
dispatch 运行产出与正式 tag 发布相同的 13 个产物（11 个标准版 +
2 个加固版）——只有 Release 发布步骤跳过。

## 本地构建验证

在本地复现一个标准版目标（Linux/amd64）：

```bash
SOURCE_DATE_EPOCH=1700000000 \
go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X github.com/LCUstinian/FG-QiMen/internal/version.Value=v0.3.1" \
  -o fg-qimen-linux-amd64 .
```

二进制的 `version` 子命令应输出 `v0.3.1`。

本地构建加固版请用加固脚本——它们跑与 CI 相同的 garble + UPX +
strip 管线（garble 固定在 v0.17.0，版本号从
`internal/version/version.go` 自动推导）：

```bash
# Linux/macOS：构建后原地加固 release/fg-qimen。
scripts/harden.sh [binary_path] [version]

# Windows（PowerShell；scripts/harden.bat 是薄包装）。
scripts\harden.ps1 [binary_path] [version]
```

可选的第 4 阶段通过 `osslsigncode` 从合法 PE 克隆代码签名证书——
调用前设置 `CLONE_SOURCE=<legit.exe>`。加固产物不可复现（garble
随机 seed）；这是刻意设计。

## 发布后固定版本号

tag 推送、发布上线之后：

- 把 `internal/version/version.go` 的 `Value` 提升到下一个开发
  版本（如 `0.3.2-dev`）。
- 在 `CHANGELOG.md` 开一个新的 `[Unreleased]` 小节。
- 回到 `main` 继续正常开发。

这样下一次发布时 `git log v0.3.1..main` 保持干净。

## 故障排查

### 某个平台构建失败

在 GitHub Actions UI 里单独重跑该矩阵项（"Re-run jobs → re-run
failed jobs"）。最常见原因：新 Go 目标需要矩阵项没提供的构建标志
（如 GOARM）。更新 `release.yml` 的矩阵即可。

### Cosign 失败

检查 `cosign sign-blob` 输出。常见原因：工作流没有授予 OIDC
`id-token` 权限——检查 `permissions:` 块。如果 OIDC issuer URL
变了（Sigstore 策略变更），同步更新 `release.yml`。

### ghcr.io 推送失败

工作流使用自动生成的 `GITHUB_TOKEN` 推 ghcr.io。如果组织的
"Allow GitHub Actions to create and approve pull requests" 设置
被禁用，会失败。到组织设置页修复。

### UPX 或 garble 失败（加固版构建）

UPX 失败只会降级加固产物——工作流告警后发布仅 garble 混淆的
二进制（与 `scripts/harden.sh` / `harden.ps1` 策略一致），黄色
告警步骤不是发布阻塞。garble 失败才是阻塞。如果 garble 报
`go: overlay contains a replacement for ...`，说明工作流选中了
下载的工具链——确保设置 `GOTOOLCHAIN=local`（garble 无法 patch
GOMODCACHE 里的工具链）。

### 杀软报毒（加固版二进制）

这是预期行为，不是 bug：随机 seed 混淆加 UPX 压缩在启发式引擎
眼里就是 packer 恶意软件。对精确 SHA256 的 cosign 签名是真实性
证明——排查前先验签。受管终端可以考虑按哈希加白，或者直接分发
标准版。

### tag 已推送但没有出现 Release

检查该 tag 的 Actions 页。如果 `release` job 失败，查失败日志。
如果工作流根本没跑，确认 tag 名匹配 `v*` 且 `release.yml` 的
`on.push.tags` 配置正确。

## 发布检查清单（TL;DR）

```bash
# 1. 起飞前检查
git checkout main && git pull --ff-only
# 确认：CHANGELOG 已更新、版本号已提升、测试全绿

# 2. 打 tag
git tag -a vX.Y.Z -m "vX.Y.Z — description"
git push origin vX.Y.Z

# 3. 盯进度
# https://github.com/LCUstinian/FG-QiMen/actions

# 4. 工作站验证
sha256sum -c SHA256SUMS          # 13 条：11 个标准版 + 2 个加固版
cosign verify-blob --certificate ... --signature ...
docker pull ghcr.io/<owner>/fg-qimen:vX.Y.Z
```

## 交叉引用

- 工作流：`.github/workflows/release.yml`、`container.yml`、
  `workflow-lint.yml`、`ci.yml`、`homebrew-tap.yml`、`scoop-bucket.yml`
- 分批验证报告：`docs/verification/v0.3/first-batch-verification.md`、
  `docs/verification/v0.3/second-batch-verification.md`、
  `docs/verification/v0.4/verification.md`、
  `docs/verification/v0.5/verification.md`
- 发布说明：`CHANGELOG.md`
- Dockerfile：`.github/docker/Dockerfile`
- 基准测试：`docs/verification/v0.4/benchmarks.md`
