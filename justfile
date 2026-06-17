# FG-QiMen build recipe (just / justfile)
# FG-QiMen 构建配方
#
# just is a command runner (https://github.com/casey/just).
# Install: cargo install just  /  winget install just
#
# Run `just` to list available recipes.
# 运行 `just` 查看所有可用 recipe。

set shell := ["bash", "-uc"]
set dotenv-load

# Binary name / 二进制名
binary := "fg-qimen"

# Version (override with `just version=v0.3.0 build`) / 版本
#
# Keep in sync with internal/version/version.go const. The v0.2
# audit (doc-3) flagged the prior 0.1.0-dev default as silently
# diverging from the source-of-truth const — a `just build` then
# produced a binary that reported 0.1.0-dev while `go run .` from a
# clean checkout reported 0.2.0. We now default to 0.2.0 to match
# the const; CI / release workflows override this via
# `just version=v0.x.y build`.
#
# 与 internal/version/version.go 常量保持一致。v0.2 审计（doc-3）
# 把旧的 0.1.0-dev 默认标为与真源常量静默漂移——`just build` 出的
# 二进制报 0.1.0-dev，而干净 checkout 下的 `go run .` 报 0.2.0。
# 现默认 0.2.0 以匹配常量；CI / release 工作流通过
# `just version=v0.x.y build` 覆盖。
version := "0.2.0"

# Code obfuscation via garble (https://github.com/burrowers/garble).
# 仅作用于 release 构建；`go run .` / `go test` / `go build -gcflags=...
# -N -l`（调试）等场景保持纯净，避免混淆栈跟踪与 dlv 断点受影响。
#
# use_garble=1: 默认开启混淆。set to "0" to disable (CI sanity build).
# garble_seed: 默认 "random" 每次构建产出不同 Hash；可填 base64
#   字符串固定 seed 做可复现构建。
# garble_bin: garble 可执行文件路径；默认从 $(go env GOPATH)/bin/garble 推导。
#
# / 仅作用于 release 构建；调试/测试链不混淆。
# / use_garble=0 可关闭（CI 静态扫描等场景）。
# / garble_seed=random 每次产出不同 Hash，对抗文件 Hash 识别。
#
# 注意：garble 标志必须放在 `build` 子命令前，例如：
#   garble -seed=random build -ldflags=... -o out.exe .
# 而不是 build 命令的参数；justfile 模板会自动处理。
use_garble := "1"
garble_seed := "random"
garble_bin := if env_var_or_default("GARBLE", "") != "" {
    env_var("GARBLE")
} else {
    "$(go env GOPATH)/bin/garble"
}

# Build ldflags (strip + clear build-id + version injection) / 构建 ldflags
# -s: omit symbol table
# -w: omit DWARF debug info
# -buildid=: clear build ID (smaller, reproducible)
# / -s：去符号表；-w：去 DWARF 调试信息；-buildid=：清构建 ID（小一点、可复现）
ldflags := "-s -w -buildid= -X github.com/LCUstinian/FG-QiMen/internal/version.Value=" + version

# CGO disabled — all our deps are pure Go (bubbletea, lipgloss, bbolt, go-mssqldb,
# hirochachacha/go-smb2, jlaffaye/ftp, x/crypto/ssh, go-sql-driver/mysql).
# Pure-Go build = statically linked, no libc dependency, easier distribution.
# / 关闭 CGO——所有依赖都是纯 Go。纯 Go 编译 = 静态链接、无 libc 依赖、便于分发。
export CGO_ENABLED := "0"

# Release output directory / 发布产物目录
# Contains:
#   - release/fg-qimen[.exe]         (current platform, from `just build`)
#   - release/fg-qimen-{os}-{arch}   (cross-compiled, from `just all`)
release_dir := "release"

# Scan run outputs (gitignored) / 扫描运行产物（gitignored）
#   - runs/default/         ephemeral mode default
#   - runs/projects/<name>  project mode default
runs_dir := "runs"

# Test data directory / 测试数据目录
test_dir := "test"

# Source files of interest / 相关源文件
go_files := shell("find . -name \"*.go\" -not -path \"./release/*\" | wc -l")

# ─────────────────────────────────────────────────────────────────────
# Default recipe / 默认 recipe
# ─────────────────────────────────────────────────────────────────────

# Show this help / 显示帮助
default:
    @just --list

# ─────────────────────────────────────────────────────────────────────
# Build / 构建
# ─────────────────────────────────────────────────────────────────────

# Build for current platform / 当前平台构建
# -trimpath: strip filesystem paths from binary (smaller, reproducible)
# -buildvcs=false: omit VCS info embedded by default in a git checkout
# / -trimpath：去文件路径；-buildvcs=false：去 git 信息
#
# Default behaviour:
#   - use_garble=1: invoke `garble -seed=random build ...` for code-name
#     obfuscation. The garble layer rewrites function/variable/package
#     names; the resulting binary has a different SHA256 on every build.
#     No literal obfuscation (-literals) and no -tiny, so stack traces
#     remain readable and the toolchain footprint stays small.
#   - use_garble=0: fall back to plain `go build` (useful for CI
#     sanity checks that compare the unobfuscated binary against a
#     known-good fingerprint, or for `delve` symbol resolution).
#
# 默认行为: use_garble=1 时通过 garble 走混淆构建;产物的 SHA256
# 每次构建都不同。仅混淆名称，不混淆字面量、不开 -tiny,栈跟踪
# 仍可读（garble reverse 还原）。
build:
    @mkdir -p {{release_dir}}
    @if [ "{{use_garble}}" = "1" ]; then \
        if ! command -v {{garble_bin}} >/dev/null 2>&1; then \
            echo "==> garble not found at {{garble_bin}}; installing mvdan.cc/garble@latest" >&2; \
            go install mvdan.cc/garble@latest; \
        fi; \
        echo "==> Building {{binary}} {{version}} (cgo=off, garble obfuscated, seed={{garble_seed}})"; \
        {{garble_bin}} -seed={{garble_seed}} build \
            -ldflags="{{ldflags}}" -trimpath -buildvcs=false \
            -o {{release_dir}}/{{binary}}{{exe_suffix}} . 2>&1 \
            | { grep -v "^warning: -seed only uses the first 8 bytes" || true; }; \
    else \
        echo "==> Building {{binary}} {{version}} (cgo=off, plain go build)"; \
        go build -ldflags="{{ldflags}}" -trimpath -buildvcs=false \
            -o {{release_dir}}/{{binary}}{{exe_suffix}} .; \
    fi

# Cross-compile to all platforms / 交叉编译到所有平台
#
# 同样遵守 use_garble — 默认混淆构建。
# / Same use_garble gate as `build`.
all: clean-build
    @mkdir -p {{release_dir}}
    @if [ "{{use_garble}}" = "1" ] && ! command -v {{garble_bin}} >/dev/null 2>&1; then \
        echo "==> garble not found at {{garble_bin}}; installing mvdan.cc/garble@latest" >&2; \
        go install mvdan.cc/garble@latest; \
    fi
    @echo "==> Cross-compiling {{binary}} {{version}} for all platforms (cgo=off, garble={{use_garble}})"
    @os_archs="windows/amd64/.exe linux/amd64/ darwin/amd64/ linux/arm64/ darwin/arm64/"; \
    for entry in $os_archs; do \
        goos="${entry%%/*}"; \
        rest="${entry#*/}"; \
        goarch="${rest%%/*}"; \
        ext="${rest#*/}"; \
        if [ -z "$goos" ] || [ -z "$goarch" ]; then continue; fi; \
        out="{{release_dir}}/{{binary}}-$goos-$goarch$ext"; \
        echo "  -> $goos/$goarch"; \
        if [ "{{use_garble}}" = "1" ]; then \
            GOOS=$goos GOARCH=$goarch {{garble_bin}} -seed={{garble_seed}} build \
                -ldflags="{{ldflags}}" -trimpath -buildvcs=false \
                -o "$out" . 2>&1 \
                | { grep -v "^warning: -seed only uses the first 8 bytes" || true; } || exit 1; \
        else \
            GOOS=$goos GOARCH=$goarch go build \
                -ldflags="{{ldflags}}" -trimpath -buildvcs=false \
                -o "$out" . || exit 1; \
        fi; \
    done
    @ls -lh {{release_dir}}/

# ─────────────────────────────────────────────────────────────────────
# Obfuscation (garble) / 混淆构建
# ─────────────────────────────────────────────────────────────────────
#
# `build` / `all` 默认已经走 garble(use_garble=1)。
# 下面这些配方是显式别名，方便脚本与文档引用。
# / `build` / `all` already go through garble by default; the recipes
# / below are explicit aliases for documentation / scripting clarity.

# Force-on obfuscated build (alias of `use_garble=1 build`) / 强制混淆构建当前平台
obfuscate:
    @just use_garble=1 build

# Force-on obfuscated cross-compile (alias of `use_garble=1 all`) / 强制混淆交叉编译
obfuscate-all:
    @just use_garble=1 all

# Show garble version + effective seed / 显示 garble 版本与生效的 seed
obfuscate-info:
    @if command -v {{garble_bin}} >/dev/null 2>&1; then \
        echo "==> garble: $({{garble_bin}} version 2>&1 | head -1)"; \
        echo "    binary: {{garble_bin}}"; \
        echo "    seed:   {{garble_seed}} (override with: just garble_seed=...)"; \
    else \
        echo "garble not found at {{garble_bin}}; run 'just build' once to auto-install, or: go install mvdan.cc/garble@latest" >&2; \
        exit 1; \
    fi
# (or any build that produces files in release/). Emits a single
# `release/SHA256SUMS` file in the standard two-column format
# that `sha256sum -c SHA256SUMS` can verify.
#
# P1/audit: release/ had no checksums; a MITM or compromised
# mirror could substitute binaries without detection (F-07).
#
# 为所有 release 产物生成 SHA256SUMS。在 `just all`（或任何往
# release/ 写文件的 build）后跑。输出标准两列格式的 release/SHA256SUMS，
# `sha256sum -c SHA256SUMS` 可校验。
sha256sums:
    @if [ -z "$(ls -A {{release_dir}} 2>/dev/null | grep -v SHA256SUMS)" ]; then \
        echo "no release artifacts in {{release_dir}}/ — run 'just build' or 'just all' first" >&2; \
        exit 1; \
    fi
    @cd {{release_dir}} && \
        find . -maxdepth 1 -type f -name '{{binary}}*' \! -name 'SHA256SUMS' -print0 | \
        xargs -0 sha256sum > SHA256SUMS
    @echo "[*] {{release_dir}}/SHA256SUMS:"
    @cat {{release_dir}}/SHA256SUMS

# Build and run with default flags / 构建并以默认参数运行
run: build
    @./{{release_dir}}/{{binary}}{{exe_suffix}} -H 127.0.0.1

# Quick local test against 127.0.0.1:18080 (assumes a local service) / 本地快速测试
test-local: build
    @./{{release_dir}}/{{binary}}{{exe_suffix}} -H 127.0.0.1 --ports 18080,22,80,3306 -t 5 --shutdown-timeout 2s

# Clean ephemeral-mode outputs / 清理即扫即走输出
clean-out:
    @rm -rf {{runs_dir}}/default
    @echo "[*] {{runs_dir}}/default cleaned"

# Clean a project's outputs (usage: just clean-project NAME) / 清理某个项目产物
clean-project name:
    @rm -rf {{runs_dir}}/projects/{{name}}
    @echo "[*] {{runs_dir}}/projects/{{name}} cleaned"

# Clean all scan-run outputs / 清理所有扫描运行产物
clean-runs:
    @rm -rf {{runs_dir}}
    @echo "[*] {{runs_dir}}/ cleaned"

# Clean test data / 清理测试数据
clean-test:
    @rm -rf {{test_dir}}
    @echo "[*] {{test_dir}}/ cleaned"

# ─────────────────────────────────────────────────────────────────────
# Dependency / 依赖
# ─────────────────────────────────────────────────────────────────────

# Run go mod tidy / 整理依赖
tidy:
    go mod tidy

# Download and verify dependencies / 下载并验证依赖
deps:
    go mod download
    go mod verify

# ─────────────────────────────────────────────────────────────────────
# Code quality / 代码质量
# ─────────────────────────────────────────────────────────────────────

# Run go fmt / 格式化代码
fmt:
    go fmt ./...

# Run go vet / 静态检查
vet:
    go vet ./...

# Run go test / 运行测试
test:
    go test ./...

# Run go test with verbose / 详细测试输出
testv:
    go test -v ./...

# Run all quality checks (fmt + vet + test) / 运行所有质量检查
check: fmt vet test

# ─────────────────────────────────────────────────────────────────────
# Cleanup / 清理
# ─────────────────────────────────────────────────────────────────────

# Remove build artifacts and release/ / 清理构建产物
clean: clean-build
    @rm -rf {{release_dir}}
    @echo "[*] clean done"

# Remove only local binary (keep release/) / 仅清理本地二进制
clean-build:
    @rm -f {{release_dir}}/{{binary}}{{exe_suffix}}

# ─────────────────────────────────────────────────────────────────────
# Documentation / 文档
# ─────────────────────────────────────────────────────────────────────

# Show line counts / 显示行数
loc:
    @echo "==> Go source line counts"
    @find . -name "*.go" -not -path "./{{release_dir}}/*" | xargs wc -l 2>/dev/null | tail -1
    @echo "  ({{go_files}} files)"

# Show Go module info / 显示 go module 信息
modinfo:
    go list -m -json

# ─────────────────────────────────────────────────────────────────────
# Helpers / 工具
# ─────────────────────────────────────────────────────────────────────

# Detect current platform's executable suffix (.exe on Windows) / 检测当前平台的可执行后缀
[private]
exe_suffix := if os_family() == "windows" { ".exe" } else { "" }
