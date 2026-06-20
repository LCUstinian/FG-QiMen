#!/usr/bin/env bash
# lint-hard-rule.sh — enforce the HARD no-exploit policy via grep.
# A.2 of the audit roadmap. / 通过 grep 强制 HARD 不利用规则。
#
# The project is a HARD-rule-compliant scanner: no post-auth
# actions, no CVE exploitation, no reverse/bind shell, no
# Session.Exec. The HARD rule is documented in docs/SECURITY.md
# and stamped as // HARD: comments on each authenticator. This
# script turns it from a docstring into a CI gate.
#
# The forbidden tokens below are searched for ONLY in
# internal/core/credential/auth/ and internal/plugins/adapted/
# (the plugin + auth code). Test files (*_test.go) are excluded
# — they exercise the negative path (e.g. "not SSH") and the
# 注释中mentioning "ssh.NewSession" is a documentation reference
# not a call.
#
# Searched forbidden API tokens:
#   - ssh.NewSession       (post-auth session — forbidden)
#   - .Exec(                 (command execution — forbidden in any form)
#   - .Shell(                (shell — redundant with .Exec)
#   - os/exec                (subprocess on a target — forbidden)
#   - gobfuscate             (used by fscan POC; not our toolchain)
#
# Run: bash scripts/lint-hard-rule.sh
# Exit 0 = clean; Exit 1 = forbidden token found.

set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 2

# Paths to scan. / 扫描路径。
SCAN_PATHS=(
  "internal/core/credential"
  "internal/plugins/adapted"
)

# Forbidden patterns (regex). Test files excluded via -prune.
# / 禁止的 pattern（regex）。通过 -prune 排除测试文件。
FORBIDDEN=(
  '\bssh\.NewSession\b'
  '\.Shell\('
  '\bos/exec\b'
)

# Note: we deliberately do NOT ban `.Exec(` alone — many
# legitimate helpers end in `Exec(` (e.g. Result.Exec, *sql.DB.Exec).
# Instead we ban the package path ssh.NewSession which is the
# gateway to the post-auth session API, and os/exec which spawns
# a subprocess. Any future addition of SSH Session.Exec or
# `exec.Command("ssh", ...)` will be caught.
# / 注意：我们故意不单独 ban `.Exec(` — 很多合法 helper 以
# `Exec(` 结尾（Result.Exec、*sql.DB.Exec）。我们 ban 的是
# ssh.NewSession（后门 session API 入口）和 os/exec（子进程）。

found=0
for pat in "${FORBIDDEN[@]}"; do
  # -P perl-regex, -r recursive, -n line number, --include '*.go'.
  # / -P perl 正则，-r 递归，-n 行号，--include '*.go'。
  # Exclude _test.go and the doc.go placeholder.
  # / 排除 _test.go 和 doc.go 占位文件。
  hits=$(grep -rPn \
    --include='*.go' \
    --exclude='*_test.go' \
    --exclude='doc.go' \
    "${SCAN_PATHS[@]}" \
    --regexp="$pat" 2>/dev/null || true)
  if [[ -n "$hits" ]]; then
    echo "❌ FORBIDDEN token matches pattern: $pat"
    echo "$hits"
    found=1
  fi
done

if [[ $found -ne 0 ]]; then
  echo
  echo "HARD rule violation. The project explicitly forbids
ssh.NewSession / ssh.Client.Shell / os/exec inside credential
code paths. See docs/SECURITY.md §'Hard rules'."
  exit 1
fi

echo "✅ HARD rule lint passed (no forbidden tokens in credential / plugin code)."
exit 0
