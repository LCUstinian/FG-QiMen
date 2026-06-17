# harden.ps1 — Full binary hardening pipeline for FG-QiMen (Windows PowerShell)
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
#   .\scripts\harden.ps1 [binary_path] [version]
#
# Examples:
#   .\scripts\harden.ps1                              # defaults: release\fg-qimen.exe 0.2.0
#   .\scripts\harden.ps1 release\fg-qimen.exe 0.3.0   # custom path + version
#   $env:CLONE_SOURCE="C:\path\to\legit.exe"; .\scripts\harden.ps1  # with cert cloning
#
# Environment variables:
#   GARBLE         — path to garble binary (default: $env:GOPATH\bin\garble.exe)
#   GARBLE_SEED    — fixed seed for reproducible builds (default: random)
#   CLONE_SOURCE   — path to legitimate PE for signature cloning (optional)

param(
    [Parameter(Position=0)]
    [string]$Binary = "release\fg-qimen.exe",

    [Parameter(Position=1)]
    [string]$Version = "0.2.0",

    [switch]$SkipBuild
)

# ── Configuration ────────────────────────────────────────────────────────

$ErrorActionPreference = "Stop"

# Add UPX path fallback if not in PATH
$UpxFallback = "C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64"
if (-not (Get-Command upx -ErrorAction SilentlyContinue) -and (Test-Path $UpxFallback)) {
    $env:PATH = "$UpxFallback;$env:PATH"
}

$GarbleBin = if ($env:GARBLE) { $env:GARBLE } else { "$env:GOPATH\bin\garble.exe" }
$GarbleSeed = if ($env:GARBLE_SEED) { $env:GARBLE_SEED } else { "random" }
$ModulePath = "github.com/LCUstinian/FG-QiMen/internal/version.Value"
$LdFlags = "-s -w -buildid= -X ${ModulePath}=${Version}"
$UpxArgs = "--best --lzma -q"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$StripScript = Join-Path $ScriptDir "strip_upx.py"
$CloneSource = $env:CLONE_SOURCE

# ── Helpers ──────────────────────────────────────────────────────────────

function Write-Log {
    param([string]$Message)
    Write-Host "[*] $Message" -ForegroundColor Cyan
}

function Write-Warn {
    param([string]$Message)
    Write-Host "[!] $Message" -ForegroundColor Yellow
}

function Write-Err {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
    exit 1
}

function Test-Tool {
    param(
        [string]$Tool,
        [string]$InstallHint
    )
    if (-not (Get-Command $Tool -ErrorAction SilentlyContinue)) {
        Write-Err "$Tool not found. Install: $InstallHint"
    }
}

function Get-FileSize {
    param([string]$Path)
    $file = Get-Item $Path
    $size = $file.Length
    if ($size -ge 1GB) {
        return "{0:N2} GB" -f ($size / 1GB)
    } elseif ($size -ge 1MB) {
        return "{0:N2} MB" -f ($size / 1MB)
    } elseif ($size -ge 1KB) {
        return "{0:N2} KB" -f ($size / 1KB)
    } else {
        return "$size bytes"
    }
}

# ── Pre-flight checks ────────────────────────────────────────────────────

Write-Log "FG-QiMen Binary Hardening Pipeline (Windows PowerShell)"
Write-Log "========================================================="
Write-Log "Binary:  $Binary"
Write-Log "Version: $Version"
Write-Log "Seed:    $GarbleSeed"
Write-Host ""

# Check Python
$PythonCmd = $null
if (Get-Command python -ErrorAction SilentlyContinue) {
    $PythonCmd = "python"
} elseif (Get-Command python3 -ErrorAction SilentlyContinue) {
    $PythonCmd = "python3"
} else {
    Write-Err "python/python3 not found. Install Python 3.6+ from https://www.python.org/"
}

$PythonVersion = & $PythonCmd --version 2>&1
Write-Log "Python:  $PythonCmd ($PythonVersion)"

# ── Stage 1: Garble Obfuscation ──────────────────────────────────────────

if ($SkipBuild) {
    Write-Host ""
    Write-Log "Stage 1/4: Garble obfuscation (skipped, --skip-build)"
    Write-Log "-----------------------------------------------"
    if (-not (Test-Path $Binary)) {
        Write-Err "Build skipped but binary not found: $Binary"
    }
    Write-Log "Using existing binary: $(Get-FileSize $Binary)"
} else {
    Write-Host ""
    Write-Log "Stage 1/4: Garble obfuscation"
    Write-Log "-----------------------------"

    # Install garble if missing
    if (-not (Test-Path $GarbleBin)) {
        Write-Log "garble not found at $GarbleBin, installing..."
        & go install mvdan.cc/garble@latest
    }

    $GarbleVersion = & $GarbleBin version 2>&1 | Select-Object -First 1
    Write-Log "Garble:  $GarbleBin ($GarbleVersion)"

    # Build with garble via temp batch file to handle quoting correctly
    Write-Log "Building with garble -seed=$GarbleSeed -literals ..."
    $env:CGO_ENABLED = "0"
    $tmpBat = "$env:TEMP\fgqimen_harden.bat"
    $batContent = "@echo off`r`nset CGO_ENABLED=0`r`n""$GarbleBin"" -seed=$GarbleSeed -literals build ""-ldflags=$LdFlags"" -trimpath -buildvcs=false -o ""$Binary"" ."
    Set-Content -Path $tmpBat -Value $batContent -Encoding ASCII
    $proc = Start-Process -FilePath $tmpBat -NoNewWindow -Wait -PassThru -RedirectStandardOutput "$env:TEMP\fgqimen_harden_stdout.txt" -RedirectStandardError "$env:TEMP\fgqimen_harden_stderr.txt"
    $buildOutput = Get-Content "$env:TEMP\fgqimen_harden_stderr.txt" -ErrorAction SilentlyContinue | Where-Object { $_ -notmatch "^warning: -seed only uses the first 8 bytes" }
    if ($buildOutput) { $buildOutput | ForEach-Object { Write-Host $_ } }
    Remove-Item "$env:TEMP\fgqimen_harden_stdout.txt" -ErrorAction SilentlyContinue
    Remove-Item "$env:TEMP\fgqimen_harden_stderr.txt" -ErrorAction SilentlyContinue
    Remove-Item $tmpBat -ErrorAction SilentlyContinue

    if ($proc.ExitCode -ne 0) {
        Write-Err "Build failed with exit code $($proc.ExitCode)"
    }

    if (-not (Test-Path $Binary)) {
        Write-Err "Build failed: $Binary not created"
    }

    Write-Log "Build complete: $(Get-FileSize $Binary)"
}

# ── Stage 2: UPX Compression ─────────────────────────────────────────────

Write-Host ""
Write-Log "Stage 2/4: UPX compression"
Write-Log "--------------------------"

Test-Tool upx "winget install upx OR https://upx.github.io/"

Write-Log "Compressing with UPX $UpxArgs ..."
& upx --best --lzma -q $Binary

if ($LASTEXITCODE -ne 0) {
    Write-Err "UPX compression failed with exit code $LASTEXITCODE"
}

Write-Log "Compressed: $(Get-FileSize $Binary)"

Write-Warn "UPX-compressed binaries may have 50-200ms startup delay (in-memory decompression)."
Write-Warn "UPX outputs are frequently flagged by antivirus software. Consider whitelisting."

# ── Stage 3: UPX Signature Stripping ─────────────────────────────────────

Write-Host ""
Write-Log "Stage 3/4: UPX signature stripping"
Write-Log "-----------------------------------"

if (-not (Test-Path $StripScript)) {
    Write-Err "strip_upx.py not found at $StripScript"
}

Write-Log "Stripping UPX signatures with strip_upx.py ..."
& $PythonCmd $StripScript $Binary

if ($LASTEXITCODE -ne 0) {
    Write-Err "UPX signature stripping failed with exit code $LASTEXITCODE"
}

Write-Log "Signatures stripped: $(Get-FileSize $Binary)"

# ── Stage 4: Certificate Cloning (Optional) ──────────────────────────────

if ($CloneSource) {
    Write-Host ""
    Write-Log "Stage 4/4: Certificate cloning"
    Write-Log "------------------------------"

    if (-not (Test-Path $CloneSource)) {
        Write-Err "Clone source not found: $CloneSource"
    }

    # Check if binary is PE
    $fileInfo = & file $Binary 2>&1
    if ($fileInfo -notmatch "PE32") {
        Write-Warn "Certificate cloning only supports Windows PE binaries. Skipping."
    } else {
        Test-Tool osslsigncode "winget install osslsigncode OR https://github.com/mtrojnar/osslsigncode"

        Write-Log "Cloning signature from: $CloneSource"
        Write-Log "Target: $Binary"

        $signedOutput = "${Binary}.signed"

        # Clone the signature
        & osslsigncode add-signature `
            -in $CloneSource `
            -out $signedOutput `
            2>&1

        if ($LASTEXITCODE -ne 0) {
            Write-Warn "osslsigncode failed (exit code $LASTEXITCODE). The binary may still work without cloned signature."
            if (Test-Path $signedOutput) {
                Remove-Item $signedOutput -Force
            }
        } else {
            Move-Item -Force $signedOutput $Binary
            Write-Log "Signature cloned: $(Get-FileSize $Binary)"
        }
    }
} else {
    Write-Host ""
    Write-Log "Stage 4/4: Certificate cloning (skipped)"
    Write-Log "-----------------------------------------"
    Write-Log "Set `$env:CLONE_SOURCE='<path_to_legit_pe.exe>' to enable signature cloning."
    Write-Log "Example: `$env:CLONE_SOURCE='C:\path\to\legit.exe'; .\scripts\harden.ps1"
}

# ── Summary ──────────────────────────────────────────────────────────────

Write-Host ""
Write-Log "========================================================="
Write-Log "Hardening complete!"
Write-Log "========================================================="
Write-Log "Final binary: $Binary"
Write-Log "Final size:   $(Get-FileSize $Binary)"
Write-Host ""
Write-Log "Verification commands:"
Write-Log "  .\$([System.IO.Path]::GetFileName($Binary)) version          # Check version injection"
Write-Log "  upx -t $Binary                           # Verify UPX integrity"
Write-Log "  file $Binary                             # Check binary format"
Write-Host ""
Write-Warn "Remember: Test thoroughly before deployment!"
Write-Warn "  - Startup crash test"
Write-Warn "  - Functionality test (scan, TUI, etc.)"
Write-Warn "  - Antivirus scan (expect false positives)"
