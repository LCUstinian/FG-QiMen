@echo off
REM harden.bat — Full binary hardening pipeline for FG-QiMen (Windows CMD)
REM
REM Three-stage pipeline:
REM   1. Garble obfuscation (-literals, -seed=random)
REM   2. UPX extreme compression (--best --lzma)
REM   3. UPX signature stripping (scripts/strip_upx.py)
REM
REM Optional stage 4:
REM   4. Certificate cloning via osslsigncode (Windows PE only)
REM
REM Usage:
REM   scripts\harden.bat [binary_path] [version]
REM
REM Examples:
REM   scripts\harden.bat                              REM defaults: release\fg-qimen.exe 0.2.0
REM   scripts\harden.bat release\fg-qimen.exe 0.3.0   REM custom path + version
REM   set CLONE_SOURCE=C:\path\to\legit.exe && scripts\harden.bat  REM with cert cloning
REM
REM Environment variables:
REM   GARBLE         — path to garble binary (default: %GOPATH%\bin\garble.exe)
REM   GARBLE_SEED    — fixed seed for reproducible builds (default: random)
REM   CLONE_SOURCE   — path to legitimate PE for signature cloning (optional)

setlocal enabledelayedexpansion

REM Add UPX path fallback if not in PATH
where upx >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    if exist "C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64\upx.exe" (
        set PATH=C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64;%PATH%
    )
)

REM ── Configuration ───────────────────────────────────────────────────────

set BINARY=%~1
if "%BINARY%"=="" set BINARY=release\fg-qimen.exe

set VERSION=%~2
if "%VERSION%"=="" set VERSION=0.2.0

set SKIP_BUILD=%~3

if defined GARBLE (
    set GARBLE_BIN=%GARBLE%
) else (
    set GARBLE_BIN=%GOPATH%\bin\garble.exe
)

if defined GARBLE_SEED (
    set GARBLE_SEED_VAL=%GARBLE_SEED%
) else (
    set GARBLE_SEED_VAL=random
)

set MODULE_PATH=github.com/LCUstinian/FG-QiMen/internal/version.Value
set LD_FLAGS=-s -w -buildid= -X %MODULE_PATH%=%VERSION%
set UPX_ARGS=--best --lzma -q

REM Get script directory
set SCRIPT_DIR=%~dp0
set STRIP_SCRIPT=%SCRIPT_DIR%strip_upx.py

set CLONE_SOURCE=%CLONE_SOURCE%

REM ── Pre-flight checks ──────────────────────────────────────────────────

echo [*] FG-QiMen Binary Hardening Pipeline (Windows CMD)
echo [*] =========================================================
echo [*] Binary:  %BINARY%
echo [*] Version: %VERSION%
echo [*] Seed:    %GARBLE_SEED_VAL%
echo.

REM Check Python
where python >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    set PYTHON_CMD=python
) else (
    where python3 >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        set PYTHON_CMD=python3
    ) else (
        echo [ERROR] python/python3 not found. Install Python 3.6+ from https://www.python.org/
        exit /b 1
    )
)

for /f "tokens=*" %%i in ('%PYTHON_CMD% --version 2^>^&1') do set PYTHON_VERSION=%%i
echo [*] Python:  %PYTHON_CMD% (%PYTHON_VERSION%)

REM ── Stage 1: Garble Obfuscation ────────────────────────────────────────

echo.
if "%SKIP_BUILD%"=="--skip-build" (
    echo [*] Stage 1/4: Garble obfuscation (skipped, --skip-build)
    echo [*] -----------------------------------------------
    if not exist "%BINARY%" (
        echo [ERROR] Build skipped but binary not found: %BINARY%
        exit /b 1
    )
    for %%A in ("%BINARY%") do set BINARY_SIZE=%%~zA
    echo [*] Using existing binary: %BINARY_SIZE% bytes
) else (
    echo [*] Stage 1/4: Garble obfuscation
    echo [*] -----------------------------

    REM Install garble if missing
    if not exist "%GARBLE_BIN%" (
        echo [*] garble not found at %GARBLE_BIN%, installing...
        go install mvdan.cc/garble@latest
        if %ERRORLEVEL% NEQ 0 (
            echo [ERROR] Failed to install garble
            exit /b 1
        )
    )

    for /f "tokens=*" %%i in ('"%GARBLE_BIN%" version 2^>^&1 ^| findstr /r "."') do set GARBLE_VERSION=%%i
    echo [*] Garble:  %GARBLE_BIN% (%GARBLE_VERSION%)

    REM Build with garble
    echo [*] Building with garble -seed=%GARBLE_SEED_VAL% -literals ...
    set CGO_ENABLED=0
    "%GARBLE_BIN%" -seed=%GARBLE_SEED_VAL% -literals build -ldflags="%LD_FLAGS%" -trimpath -buildvcs=false -o "%BINARY%" . 2>&1 | findstr /v "^warning: -seed only uses the first 8 bytes"

    if %ERRORLEVEL% NEQ 0 (
        echo [ERROR] Build failed with exit code %ERRORLEVEL%
        exit /b 1
    )

    if not exist "%BINARY%" (
        echo [ERROR] Build failed: %BINARY% not created
        exit /b 1
    )

    for %%A in ("%BINARY%") do set BINARY_SIZE=%%~zA
    echo [*] Build complete: %BINARY_SIZE% bytes
)

REM ── Stage 2: UPX Compression ───────────────────────────────────────────

echo.
echo [*] Stage 2/4: UPX compression
echo [*] --------------------------

where upx >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] upx not found. Install: winget install upx OR https://upx.github.io/
    exit /b 1
)

echo [*] Compressing with UPX %UPX_ARGS% ...
upx %UPX_ARGS% "%BINARY%"

if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] UPX compression failed with exit code %ERRORLEVEL%
    exit /b 1
)

for %%A in ("%BINARY%") do set BINARY_SIZE=%%~zA
echo [*] Compressed: %BINARY_SIZE% bytes

echo [!] UPX-compressed binaries may have 50-200ms startup delay (in-memory decompression).
echo [!] UPX outputs are frequently flagged by antivirus software. Consider whitelisting.

REM ── Stage 3: UPX Signature Stripping ───────────────────────────────────

echo.
echo [*] Stage 3/4: UPX signature stripping
echo [*] -----------------------------------

if not exist "%STRIP_SCRIPT%" (
    echo [ERROR] strip_upx.py not found at %STRIP_SCRIPT%
    exit /b 1
)

echo [*] Stripping UPX signatures with strip_upx.py ...
%PYTHON_CMD% "%STRIP_SCRIPT%" "%BINARY%"

if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] UPX signature stripping failed with exit code %ERRORLEVEL%
    exit /b 1
)

for %%A in ("%BINARY%") do set BINARY_SIZE=%%~zA
echo [*] Signatures stripped: %BINARY_SIZE% bytes

REM ─ Stage 4: Certificate Cloning (Optional) ────────────────────────────

if defined CLONE_SOURCE (
    echo.
    echo [*] Stage 4/4: Certificate cloning
    echo [*] ------------------------------

    if not exist "%CLONE_SOURCE%" (
        echo [ERROR] Clone source not found: %CLONE_SOURCE%
        exit /b 1
    )

    REM Check if binary is PE (simple check: file starts with MZ)
    powershell -Command "$bytes = [System.IO.File]::ReadAllBytes('%BINARY%'); if ($bytes[0] -eq 0x4D -and $bytes[1] -eq 0x5A) { exit 0 } else { exit 1 }"
    if %ERRORLEVEL% NEQ 0 (
        echo [!] Certificate cloning only supports Windows PE binaries. Skipping.
    ) else (
        where osslsigncode >nul 2>&1
        if %ERRORLEVEL% NEQ 0 (
            echo [ERROR] osslsigncode not found. Install: winget install osslsigncode OR https://github.com/mtrojnar/osslsigncode
            exit /b 1
        )

        echo [*] Cloning signature from: %CLONE_SOURCE%
        echo [*] Target: %BINARY%

        set SIGNED_OUTPUT=%BINARY%.signed

        REM Clone the signature
        osslsigncode add-signature -in "%CLONE_SOURCE%" -out "%SIGNED_OUTPUT%" 2>&1

        if %ERRORLEVEL% NEQ 0 (
            echo [!] osslsigncode failed (exit code %ERRORLEVEL%). The binary may still work without cloned signature.
            if exist "%SIGNED_OUTPUT%" del /f "%SIGNED_OUTPUT%"
        ) else (
            move /y "%SIGNED_OUTPUT%" "%BINARY%" >nul
            for %%A in ("%BINARY%") do set BINARY_SIZE=%%~zA
            echo [*] Signature cloned: %BINARY_SIZE% bytes
        )
    )
) else (
    echo.
    echo [*] Stage 4/4: Certificate cloning (skipped)
    echo [*] -----------------------------------------
    echo [*] Set CLONE_SOURCE=<path_to_legit_pe.exe> to enable signature cloning.
    echo [*] Example: set CLONE_SOURCE=C:\path\to\legit.exe ^& scripts\harden.bat
)

REM ── Summary ────────────────────────────────────────────────────────────

echo.
echo [*] =========================================================
echo [*] Hardening complete!
echo [*] =========================================================
echo [*] Final binary: %BINARY%
for %%A in ("%BINARY%") do set FINAL_SIZE=%%~zA
echo [*] Final size:   %FINAL_SIZE% bytes
echo.
echo [*] Verification commands:
echo [*]   %BINARY% version          REM Check version injection
echo [*]   upx -t %BINARY%           REM Verify UPX integrity
echo [*]   file %BINARY%             REM Check binary format
echo.
echo [!] Remember: Test thoroughly before deployment!
echo [!]   - Startup crash test
echo [!]   - Functionality test (scan, TUI, etc.)
echo [!]   - Antivirus scan (expect false positives)

endlocal
