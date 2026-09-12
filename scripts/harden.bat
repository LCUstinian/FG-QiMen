@echo off
REM harden.bat — Thin wrapper around harden.ps1 (single Windows logic source).
REM
REM The full hardening pipeline (garble + UPX + strip_upx.py + optional
REM certificate cloning) lives in scripts\harden.ps1. This batch file
REM only forwards arguments so CMD users don't need PowerShell syntax.
REM Previously it duplicated ~180 lines of pipeline logic, which drifted
REM from harden.ps1 — keep this file thin.
REM
REM / harden.bat——harden.ps1 的薄包装（Windows 侧唯一逻辑源是
REM harden.ps1）。完整加固管线（garble + UPX + strip_upx.py + 可选
REM 证书克隆）都在 scripts\harden.ps1；本批处理只转发参数，让 CMD
REM 用户不必写 PowerShell 语法。此前它复制了 ~180 行管线逻辑并与
REM harden.ps1 产生漂移——保持本文件精简。
REM
REM Usage / 用法:
REM   scripts\harden.bat [binary_path] [version] [/SkipBuild]
REM   scripts\harden.bat release\fg-qimen.exe 0.3.0
REM   set CLONE_SOURCE=C:\path\to\legit.exe && scripts\harden.bat
REM
REM Environment variables are passed through to harden.ps1 unchanged:
REM   GARBLE, GARBLE_VERSION, GARBLE_SEED, UPX_HOME, CLONE_SOURCE

setlocal
set "SCRIPT_DIR=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%harden.ps1" %*
endlocal & exit /b %ERRORLEVEL%
