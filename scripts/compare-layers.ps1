# compare-layers.ps1 — Build all 3 layers and compare sizes (Windows PowerShell)

param(
    [Parameter(Position=0)]
    [string]$Version = "0.2.0"
)

$ErrorActionPreference = "Stop"

# Add UPX path fallback if not in PATH
$UpxFallback = "C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64"
if (-not (Get-Command upx -ErrorAction SilentlyContinue) -and (Test-Path $UpxFallback)) {
    $env:PATH = "$UpxFallback;$env:PATH"
}

$ReleaseDir = "release"
$ModulePath = "github.com/LCUstinian/FG-QiMen/internal/version.Value"
$LdFlags = "-s -w -buildid= -X ${ModulePath}=${Version}"
$GarbleBin = if ($env:GARBLE) { $env:GARBLE } else { "$env:GOPATH\bin\garble.exe" }
$GarbleSeed = if ($env:GARBLE_SEED) { $env:GARBLE_SEED } else { "random" }

function Write-Log {
    param([string]$Message)
    Write-Host "[*] $Message" -ForegroundColor Cyan
}

function Write-Err {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
    exit 1
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

function Get-FileSizeBytes {
    param([string]$Path)
    return (Get-Item $Path).Length
}

Write-Host "=============================================" -ForegroundColor Green
Write-Host " FG-QiMen 3-Layer Build Comparison" -ForegroundColor Green
Write-Host "=============================================" -ForegroundColor Green
Write-Host ""

# ── Layer 1: Native ─────────────────────────────────────────────────────
Write-Host "Layer 1: Native build (no garble, no UPX)" -ForegroundColor Yellow
Write-Host "-------------------------------------------"
$env:CGO_ENABLED = "0"

# Write a temp batch file to handle quoting correctly
$tmpBat1 = "$env:TEMP\fgqimen_layer1.bat"
$batContent1 = "@echo off`r`nset CGO_ENABLED=0`r`ngo build ""-ldflags=$LdFlags"" -trimpath -buildvcs=false -o ""$ReleaseDir\fg-qimen-native.exe"" ."
Set-Content -Path $tmpBat1 -Value $batContent1 -Encoding ASCII
$proc1 = Start-Process -FilePath $tmpBat1 -NoNewWindow -Wait -PassThru -RedirectStandardOutput "$env:TEMP\fgqimen_layer1_stdout.txt" -RedirectStandardError "$env:TEMP\fgqimen_layer1_stderr.txt"
$layer1Stdout = Get-Content "$env:TEMP\fgqimen_layer1_stdout.txt" -ErrorAction SilentlyContinue
$layer1Stderr = Get-Content "$env:TEMP\fgqimen_layer1_stderr.txt" -ErrorAction SilentlyContinue
if ($layer1Stdout) { $layer1Stdout | ForEach-Object { Write-Host $_ } }
if ($layer1Stderr) { $layer1Stderr | ForEach-Object { Write-Host $_ } }
Remove-Item "$env:TEMP\fgqimen_layer1_stdout.txt" -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\fgqimen_layer1_stderr.txt" -ErrorAction SilentlyContinue
Remove-Item $tmpBat1 -ErrorAction SilentlyContinue

if ($proc1.ExitCode -ne 0) {
    Write-Err "Layer 1 build failed with exit code $($proc1.ExitCode)"
}
$NativeSize = Get-FileSizeBytes "$ReleaseDir\fg-qimen-native.exe"
Write-Host "  -> fg-qimen-native.exe: $(Get-FileSize "$ReleaseDir\fg-qimen-native.exe")" -ForegroundColor Green
Write-Host ""

# ── Layer 2: Garble ──────────────────────────────────────────────────────
Write-Host "Layer 2: Garble obfuscation only (no UPX)" -ForegroundColor Yellow
Write-Host "-------------------------------------------"

# Install garble if missing
if (-not (Test-Path $GarbleBin)) {
    Write-Log "garble not found, installing..."
    & go install mvdan.cc/garble@latest
}

# Write a temp batch file to handle quoting correctly
$tmpBat2 = "$env:TEMP\fgqimen_layer2.bat"
$batContent2 = "@echo off`r`nset CGO_ENABLED=0`r`n""$GarbleBin"" -seed=$GarbleSeed -literals build ""-ldflags=$LdFlags"" -trimpath -buildvcs=false -o ""$ReleaseDir\fg-qimen-garble.exe"" ."
Set-Content -Path $tmpBat2 -Value $batContent2 -Encoding ASCII
$proc2 = Start-Process -FilePath $tmpBat2 -NoNewWindow -Wait -PassThru -RedirectStandardOutput "$env:TEMP\fgqimen_layer2_stdout.txt" -RedirectStandardError "$env:TEMP\fgqimen_layer2_stderr.txt"
$layer2Stdout = Get-Content "$env:TEMP\fgqimen_layer2_stdout.txt" -ErrorAction SilentlyContinue
$layer2Stderr = Get-Content "$env:TEMP\fgqimen_layer2_stderr.txt" -ErrorAction SilentlyContinue | Where-Object { $_ -notmatch "^warning: -seed only uses the first 8 bytes" }
if ($layer2Stdout) { $layer2Stdout | ForEach-Object { Write-Host $_ } }
if ($layer2Stderr) { $layer2Stderr | ForEach-Object { Write-Host $_ } }
Remove-Item "$env:TEMP\fgqimen_layer2_stdout.txt" -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\fgqimen_layer2_stderr.txt" -ErrorAction SilentlyContinue
Remove-Item $tmpBat2 -ErrorAction SilentlyContinue

if ($proc2.ExitCode -ne 0) {
    Write-Err "Layer 2 garble build failed with exit code $($proc2.ExitCode)"
}

$GarbleSize = Get-FileSizeBytes "$ReleaseDir\fg-qimen-garble.exe"
Write-Host "  -> fg-qimen-garble.exe: $(Get-FileSize "$ReleaseDir\fg-qimen-garble.exe")" -ForegroundColor Green
Write-Host ""

# ── Layer 3: Full pipeline ───────────────────────────────────────────────
Write-Host "Layer 3: Full pipeline (UPX + strip, reusing garble binary)" -ForegroundColor Yellow
Write-Host "-------------------------------------------"

# Copy Layer 2 garble binary as starting point for Layer 3
Copy-Item "$ReleaseDir\fg-qimen-garble.exe" "$ReleaseDir\fg-qimen-full.exe" -Force

$hardenScript = Join-Path $PSScriptRoot "harden.ps1"
& $hardenScript "$ReleaseDir\fg-qimen-full.exe" $Version -SkipBuild

if ($LASTEXITCODE -ne 0) {
    Write-Err "Layer 3 harden failed"
}

$FullSize = Get-FileSizeBytes "$ReleaseDir\fg-qimen-full.exe"
Write-Host ""

# ── Summary table ────────────────────────────────────────────────────────
Write-Host "=============================================" -ForegroundColor Green
Write-Host " Size Comparison" -ForegroundColor Green
Write-Host "=============================================" -ForegroundColor Green

$Ratio2 = [math]::Round($GarbleSize * 100 / $NativeSize)
$Ratio3 = [math]::Round($FullSize * 100 / $NativeSize)

Write-Host ("  {0,-30} {1,12} {2,8}" -f "Layer", "Size", "Ratio")
Write-Host ("  {0,-30} {1,12} {2,8}" -f "------------------------------", "----------", "------")
Write-Host ("  {0,-30} {1,12} {2,7}%" -f "Layer 1: Native (baseline)", (Get-FileSize "$ReleaseDir\fg-qimen-native.exe"), "100")
Write-Host ("  {0,-30} {1,12} {2,7}%" -f "Layer 2: Garble (obfuscated)", (Get-FileSize "$ReleaseDir\fg-qimen-garble.exe"), $Ratio2)
Write-Host ("  {0,-30} {1,12} {2,7}%" -f "Layer 3: Full (hardened)", (Get-FileSize "$ReleaseDir\fg-qimen-full.exe"), $Ratio3)
Write-Host ""

$SavingsKB = [math]::Round(($NativeSize - $FullSize) / 1024)
$SavingsPct = [math]::Round((1 - $FullSize / $NativeSize) * 100)

Write-Host "  Savings vs Native: $SavingsKB KB ($SavingsPct%)" -ForegroundColor Green
Write-Host ""
Write-Host "  Files in release\:" -ForegroundColor Cyan
Get-ChildItem "$ReleaseDir\fg-qimen-*.exe" | ForEach-Object { Write-Host "    $($_.Name)" }
Write-Host ""
Write-Host "[*] Compare complete" -ForegroundColor Green
