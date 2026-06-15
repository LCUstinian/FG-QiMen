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

# Version (override with `just version=v0.2.0 build`) / 版本
version := "0.1.0-dev"

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
build:
    @echo "==> Building {{binary}} {{version}} (cgo=off)"
    @mkdir -p {{release_dir}}
    go build -ldflags="{{ldflags}}" -trimpath -buildvcs=false -o {{release_dir}}/{{binary}}{{exe_suffix}} .

# Cross-compile to all platforms / 交叉编译到所有平台
all: clean-build
    @echo "==> Cross-compiling {{binary}} {{version}} for all platforms (cgo=off)"
    @mkdir -p {{release_dir}}
    @os_archs="windows/amd64/.exe linux/amd64/ darwin/amd64/ linux/arm64/ darwin/arm64/"; \
    for entry in $os_archs; do \
        goos="${entry%%/*}"; \
        rest="${entry#*/}"; \
        goarch="${rest%%/*}"; \
        ext="${rest#*/}"; \
        if [ -z "$goos" ] || [ -z "$goarch" ]; then continue; fi; \
        out="{{release_dir}}/{{binary}}-$goos-$goarch$ext"; \
        echo "  -> $goos/$goarch"; \
        GOOS=$goos GOARCH=$goarch go build \
            -ldflags="{{ldflags}}" -trimpath -buildvcs=false \
            -o "$out" . || exit 1; \
    done
    @ls -lh {{release_dir}}/

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
