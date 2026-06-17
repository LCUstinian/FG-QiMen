#!/usr/bin/env bash
# harden.sh — Full binary hardening pipeline for FG-QiMen.
#
# Three-stage pipeline:
#   1. Garble obfuscation (-literals, -seed=random)
#   2. UPX extreme compression (--best --lzma)
#   3. UPX signature stripping (scripts/strip_upx.py)
#
# Optional stage 4:
#   4. Certificate cloning via osslsigncode (Windows PE only)
#
# Usage:
#   ./scripts/harden.sh [binary_path] [version]
#
# Examples:
#   ./scripts/harden.sh                          # defaults: release/fg-qimen 0.2.0
#   ./scripts/harden.sh release/fg-qimen 0.3.0   # custom path + version
#   CLONE_SOURCE=/path/to/legit.exe ./scripts/harden.sh  # with cert cloning
#
# Environment variables:
#   GARBLE         — path to garble binary (default: $GOPATH/bin/garble)
#   GARBLE_SEED    — fixed seed for reproducible builds (default: random)
#   CLONE_SOURCE   — path to legitimate PE for signature cloning (optional)
#
# FG-QiMen 二进制加固全链路脚本。
#
# 三阶段管线：
#   1. Garble 混淆（-literals, -seed=random）
#   2. UPX 极限压缩（--best --lzma）
#   3. UPX 特征消除（scripts/strip_upx.py）
#
# 可选第四阶段：
#   4. 通过 osslsigncode 克隆证书（仅 Windows PE）
#
# 用法：
#   ./scripts/harden.sh [二进制路径] [版本号]
#
# 示例：
#   ./scripts/harden.sh                              # 默认：release/fg-qimen 0.2.0
#   ./scripts/harden.sh release/fg-qimen 0.3.0       # 自定义路径 + 版本
#   CLONE_SOURCE=/path/to/legit.exe ./scripts/harden.sh  # 带证书克隆
#
# 环境变量：
#   GARBLE         — garble 二进制路径（默认：$GOPATH/bin/garble）
#   GARBLE_SEED    — 固定 seed 用于可复现构建（默认：random）
#   CLONE_SOURCE   — 合法 PE 文件路径用于签名克隆（可选）

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────

BINARY="${1:-release/fg-qimen}"
VERSION="${2:-0.2.0}"
SKIP_BUILD="${3:-}"
GARBLE_BIN="${GARBLE:-$(go env GOPATH)/bin/garble}"
GARBLE_SEED="${GARBLE_SEED:-random}"
MODULE_PATH="github.com/LCUstinian/FG-QiMen/internal/version.Value"
LD_FLAGS="-s -w -buildid= -X ${MODULE_PATH}=${VERSION}"
UPX_ARGS="--best --lzma -q"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STRIP_SCRIPT="${SCRIPT_DIR}/strip_upx.py"
CLONE_SOURCE="${CLONE_SOURCE:-}"

# ── Helpers / 工具函数 ─────────────────────────────────────────────────

log() {
    echo "[*] $*"
}

warn() {
    echo "[!] $*" >&2
}

err() {
    echo "[ERROR] $*" >&2
    exit 1
}

check_tool() {
    local tool="$1"
    local install_hint="$2"
    if ! command -v "$tool" &>/dev/null; then
        err "$tool not found. Install: $install_hint"
    fi
}

show_size() {
    local file="$1"
    local label="${2:-}"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        local size
        size=$(stat -f%z "$file")
    else
        local size
        size=$(stat -c%s "$file")
    fi
    if [[ -n "$label" ]]; then
        echo "    $label: $(numfmt --to=iec-i --suffix=B "$size" 2>/dev/null || echo "${size} bytes")"
    else
        echo "$(numfmt --to=iec-i --suffix=B "$size" 2>/dev/null || echo "${size} bytes")"
    fi
}

# ── Pre-flight checks / 前置检查 ───────────────────────────────────────

log "FG-QiMen Binary Hardening Pipeline"
log "===================================="
log "Binary:  $BINARY"
log "Version: $VERSION"
log "Seed:    $GARBLE_SEED"
echo

# Check Python (required for strip_upx.py)
if ! command -v python3 &>/dev/null; then
    if command -v python &>/dev/null; then
        PYTHON=python
    else
        err "python3/python not found. Install Python 3.6+."
    fi
else
    PYTHON=python3
fi

log "Python:  $PYTHON ($($PYTHON --version 2>&1))"

# ── Stage 1: Garble Obfuscation ──────────────────────────────────────────

if [[ "$SKIP_BUILD" == "--skip-build" ]]; then
    log ""
    log "Stage 1/4: Garble obfuscation (skipped, --skip-build)"
    log "-----------------------------------------------"
    if [[ ! -f "$BINARY" ]]; then
        err "Build skipped but binary not found: $BINARY"
    fi
    log "Using existing binary: $(show_size "$BINARY")"
else
    log ""
    log "Stage 1/4: Garble obfuscation"
    log "-----------------------------"

    # Install garble if missing
    if ! command -v "$GARBLE_BIN" &>/dev/null; then
        log "garble not found at $GARBLE_BIN, installing..."
        go install mvdan.cc/garble@latest
    fi

    log "Garble:  $GARBLE_BIN ($($GARBLE_BIN version 2>&1 | head -1))"

    # Build with garble
    log "Building with garble -seed=$GARBLE_SEED -literals ..."
    CGO_ENABLED=0 "$GARBLE_BIN" -seed="$GARBLE_SEED" -literals build \
        -ldflags="$LD_FLAGS" \
        -trimpath \
        -buildvcs=false \
        -o "$BINARY" . 2>&1 | grep -v "^warning: -seed only uses the first 8 bytes" || true

    if [[ ! -f "$BINARY" ]]; then
        err "Build failed: $BINARY not created"
    fi

    log "Build complete: $(show_size "$BINARY")"
fi

# ── Stage 2: UPX Compression ─────────────────────────────────────────────

log ""
log "Stage 2/4: UPX compression"
log "--------------------------"

check_tool upx "winget install upx (Windows) | apt install upx-ucl (Linux) | brew install upx (macOS)"

log "Compressing with UPX $UPX_ARGS ..."
upx $UPX_ARGS "$BINARY"

log "Compressed: $(show_size "$BINARY")"

warn "UPX-compressed binaries may have 50-200ms startup delay (in-memory decompression)."
warn "UPX outputs are frequently flagged by antivirus software. Consider whitelisting."

# ── Stage 3: UPX Signature Stripping ─────────────────────────────────────

log ""
log "Stage 3/4: UPX signature stripping"
log "-----------------------------------"

if [[ ! -f "$STRIP_SCRIPT" ]]; then
    err "strip_upx.py not found at $STRIP_SCRIPT"
fi

log "Stripping UPX signatures with strip_upx.py ..."
$PYTHON "$STRIP_SCRIPT" "$BINARY"

log "Signatures stripped: $(show_size "$BINARY")"

# ── Stage 4: Certificate Cloning (Optional) / 证书克隆（可选） ─────────

if [[ -n "$CLONE_SOURCE" ]]; then
    log ""
    log "Stage 4/4: Certificate cloning"
    log "------------------------------"

    if [[ ! -f "$CLONE_SOURCE" ]]; then
        err "Clone source not found: $CLONE_SOURCE"
    fi

    # Detect if binary is PE (Windows)
    if ! file "$BINARY" | grep -qi "PE32"; then
        warn "Certificate cloning only supports Windows PE binaries. Skipping."
    else
        check_tool osslsigncode "apt install osslsigncode (Linux) | brew install osslsigncode (macOS)"

        log "Cloning signature from: $CLONE_SOURCE"
        log "Target: $BINARY"

        # Clone the signature
        osslsigncode add-signature \
            -in "$CLONE_SOURCE" \
            -out "${BINARY}.signed" \
            2>&1 || {
                warn "osslsigncode failed. The binary may still work without cloned signature."
                rm -f "${BINARY}.signed"
            }

        if [[ -f "${BINARY}.signed" ]]; then
            mv "${BINARY}.signed" "$BINARY"
            log "Signature cloned: $(show_size "$BINARY")"
        fi
    fi
else
    log ""
    log "Stage 4/4: Certificate cloning (skipped)"
    log "-----------------------------------------"
    log "Set CLONE_SOURCE=<path_to_legit_pe.exe> to enable signature cloning."
    log "Example: CLONE_SOURCE=/path/to/legit.exe ./scripts/harden.sh"
fi

# ── Summary ──────────────────────────────────────────────────────────────

log ""
log "===================================="
log "Hardening complete!"
log "===================================="
log "Final binary: $BINARY"
log "Final size:   $(show_size "$BINARY")"
log ""
log "Verification commands:"
log "  ./$(basename "$BINARY") version          # Check version injection"
log "  upx -t $BINARY                           # Verify UPX integrity"
log "  file $BINARY                             # Check binary format"
log ""
warn "Remember: Test thoroughly before deployment!"
warn "  - Startup crash test"
warn "  - Functionality test (scan, TUI, etc.)"
warn "  - Antivirus scan (expect false positives)"
