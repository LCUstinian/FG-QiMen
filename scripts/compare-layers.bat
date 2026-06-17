@echo off
REM compare-layers.bat — Build all 3 layers and compare sizes (Windows CMD)

setlocal enabledelayedexpansion

REM Add UPX path fallback if not in PATH
where upx >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    if exist "C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64\upx.exe" (
        set PATH=C:\Program_Tools\PackersOrUnpackers\upx-5.2.0-win64;%PATH%
    )
)

set RELEASE_DIR=release
set VERSION=%~1
if "%VERSION%"=="" set VERSION=0.2.0
set MODULE_PATH=github.com/LCUstinian/FG-QiMen/internal/version.Value
set LD_FLAGS=-s -w -buildid= -X %MODULE_PATH%=%VERSION%

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

echo =============================================
echo  FG-QiMen 3-Layer Build Comparison
echo =============================================
echo.

REM ── Layer 1: Native ────────────────────────────────────────────────────
echo Layer 1: Native build (no garble, no UPX)
echo -------------------------------------------
set CGO_ENABLED=0
go build -ldflags="%LD_FLAGS%" -trimpath -buildvcs=false -o "%RELEASE_DIR%\fg-qimen-native.exe" .
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Layer 1 build failed
    exit /b 1
)
for %%A in ("%RELEASE_DIR%\fg-qimen-native.exe") do set NATIVE_SIZE=%%~zA
echo   -^> fg-qimen-native.exe: %NATIVE_SIZE% bytes
echo.

REM ── Layer 2: Garble ────────────────────────────────────────────────────
echo Layer 2: Garble obfuscation only (no UPX)
echo -------------------------------------------
if not exist "%GARBLE_BIN%" (
    echo   garble not found, installing...
    go install mvdan.cc/garble@latest
)
set CGO_ENABLED=0
"%GARBLE_BIN%" -seed=%GARBLE_SEED_VAL% -literals build -ldflags="%LD_FLAGS%" -trimpath -buildvcs=false -o "%RELEASE_DIR%\fg-qimen-garble.exe" . 2>&1 | findstr /v "^warning: -seed only uses the first 8 bytes"
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Layer 2 garble build failed
    exit /b 1
)
for %%A in ("%RELEASE_DIR%\fg-qimen-garble.exe") do set GARB_SIZE=%%~zA
echo   -^> fg-qimen-garble.exe: %GARB_SIZE% bytes
echo.

REM ── Layer 3: Full pipeline ────────────────────────────────────────────
echo Layer 3: Full pipeline (UPX + strip, reusing garble binary)
echo -------------------------------------------

REM Copy Layer 2 garble binary as starting point for Layer 3
copy /y "%RELEASE_DIR%\fg-qimen-garble.exe" "%RELEASE_DIR%\fg-qimen-full.exe" >nul

call "%~dp0harden.bat" "%RELEASE_DIR%\fg-qimen-full.exe" "%VERSION%" "--skip-build"
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Layer 3 harden failed
    exit /b 1
)
for %%A in ("%RELEASE_DIR%\fg-qimen-full.exe") do set FULL_SIZE=%%~zA
echo.

REM ── Summary table ──────────────────────────────────────────────────────
echo =============================================
echo  Size Comparison
echo =============================================
echo   Layer                          Size         Ratio
echo   ------------------------------ ------------ ------

REM Calculate ratios using PowerShell one-liner (CMD lacks integer division)
for /f %%R in ('powershell -NoProfile -Command "[math]::Round(%GARB_SIZE% * 100 / %NATIVE_SIZE%)"') do set RATIO2=%%R
for /f %%R in ('powershell -NoProfile -Command "[math]::Round(%FULL_SIZE% * 100 / %NATIVE_SIZE%)"') do set RATIO3=%%R

echo   Layer 1: Native (baseline)     %NATIVE_SIZE% bytes      100%%
echo   Layer 2: Garble (obfuscated)   %GARB_SIZE% bytes       %RATIO2%%%
echo   Layer 3: Full (hardened)       %FULL_SIZE% bytes       %RATIO3%%%
echo.

REM Calculate savings
for /f %%S in ('powershell -NoProfile -Command "[math]::Round((%NATIVE_SIZE% - %FULL_SIZE%) / 1024)"') do set SAVINGS_KB=%%S
for /f %%P in ('powershell -NoProfile -Command "[math]::Round((1 - %FULL_SIZE% / %NATIVE_SIZE%) * 100)"') do set SAVINGS_PCT=%%P

echo   Savings vs Native: %SAVINGS_KB% KB (%SAVINGS_PCT%%%)
echo.
echo   Files in release\:
dir /b "%RELEASE_DIR%\fg-qimen-*.exe" 2>nul
echo.
echo [*] Compare complete

endlocal
