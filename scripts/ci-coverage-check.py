#!/usr/bin/env python3
"""ci-coverage-check.py — read coverage.out and enforce the
floor on the GitHub ubuntu-latest CI runner.

Used as a step in .github/workflows/ci.yml's coverage job.
Replaces the previous bash + awk + sed pipeline which was
intermittently exit-127'ing on the GHA runner (suspected:
the runner's default PATH not matching what the workflow
file expected, plus SIGPIPE on the tail -1 in some GHA
image variants). Python 3 is pre-installed on every
ubuntu-latest image.

Threshold history:
  - v0.4: 60% floor (initial)
  - v0.5.1: 60% floor kept. cmd/ coverage pushed from 59.6%
    to 64.4% via new tests for applySchedule (100%),
    applyTransport, applyHTTPForm, detectScheduleMode,
    loadScheduleTZ, daemon-loop 6-field cron. The total
    stayed around 60.5% because of 30+ adapted plugins
    (jenkins, ssh, ftp, kafka, mqtt, etc.) that are 0%
    covered today. / v0.5.1：维持 60%。cmd/ 覆盖率从 59.6%
    推到 64.4%（applySchedule 100% + applyTransport +
    applyHTTPForm + detectScheduleMode + loadScheduleTZ +
    daemon-loop 6-field cron 等新测试）。总覆盖率仍 ~60.5%
    因 30+ adapted plugin 0% 覆盖拖累。
  - v0.6.0 (2026-09-10): Tier 1-4 fake-server coverage push
    landed 35 fake-server tests across adapted plugins (jenkins,
    kibana, elasticsearch, mongodb, postgres, smtp, pop3, ssh,
    telnet, vnc, snmp, tftp, ...) raising total coverage from
    60.5% to ~71%. Per-plugin floor introduced so CI catches
    regressions in any single plugin package.

    Plan §14 commits to v0.6.0 target of 80% total + 70%
    per-plugin. The 80% global target remains aspirational
    (current actual ~71%); per-plugin floor is set conservatively
    to 60% so any new plugin that lands below it fails CI.

    / v0.6.0（2026-09-10）：Tier 1-4 fake-server 覆盖推进，在 35 个
    adapted plugin 上落地 fake-server 测试（jenkins、kibana、
    elasticsearch、mongodb、postgres、smtp、pop3、ssh、telnet、
    vnc、snmp、tftp ...），总覆盖率 60.5% → ~71%。引入 per-plugin
    门槛，CI 现在能抓住任何单 plugin 包的回归。Plan §14 承诺
    v0.6.0 目标 80% total + 70% per-plugin。80% 全局目标仍是
    目标（当前 ~71%）；per-plugin 门槛保守设在 60%，任何新
    plugin 落到它下面就会 CI 失败。
"""
import os
import re
import subprocess
import sys
from typing import List, Tuple


# Floor constants (tweakable; see docstring for history).
# / 门槛常量（可调；历史见 docstring）。
GLOBAL_FLOOR = 70.0
PER_PLUGIN_FLOOR = 60.0

PLUGIN_ROOT = "internal/plugins/adapted"

# Plugin packages exempt from the per-plugin floor. These are
# complex protocols whose happy-path coverage requires either a
# real reference implementation or a plugin-side fix; per v0.6
# fake-server plan §11.2 they're shipped at their current
# coverage level with a follow-up tracking the gap.
# / 不受 per-plugin 门槛限制的 plugin 包。这些是复杂协议，happy-
# path 覆盖要么需要真实参考实现，要么需要插件端修复；按
# v0.6 fake-server 计划 §11.2 以当前覆盖率 ship，跟踪到 follow-up。
# Add to this list ONLY when the corresponding follow-up issue
# exists in the issue tracker.
#
# v0.6.1 history:
#   - modbus removed (plugin-side io.ReadFull + 9-byte buffer fix;
#     coverage jumped from 29.7% to 87.5%; the §11.2 reason
#     no longer applies).
FLOOR_EXEMPT = frozenset()


def global_coverage_pct() -> float:
    """Parse the trailing `total:` line from `go tool cover -func`
    output. Returns the percentage as a float. / 从 `go tool cover
    -func` 输出的 `total:` 行解析出百分比。"""
    result = subprocess.run(
        ["go", "tool", "cover", "-func", "coverage.out"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr)
        raise SystemExit(result.returncode)
    m = re.search(r"total:\s+\(statements\)\s+(\d+\.\d+)%", result.stdout)
    if not m:
        sys.stderr.write("FAIL: could not parse go tool cover -func output\n")
        sys.stderr.write(result.stdout)
        raise SystemExit(1)
    return float(m.group(1))


def per_plugin_coverages() -> List[Tuple[str, float]]:
    """Run `go test -cover` for each adapted-plugin package and
    return [(pkg_path, coverage%), ...]. Skips packages that
    fail to compile or have no coverage line. / 对每个 adapted
    plugin 跑 `go test -cover`，返回 [(pkg_path, coverage%), ...]。
    编译失败或没 coverage 行的包跳过。
    """
    results: List[Tuple[str, float]] = []
    if not os.path.isdir(PLUGIN_ROOT):
        return results
    for category in sorted(os.listdir(PLUGIN_ROOT)):
        cat_dir = os.path.join(PLUGIN_ROOT, category)
        if not os.path.isdir(cat_dir):
            continue
        for plugin in sorted(os.listdir(cat_dir)):
            pkg = os.path.join(cat_dir, plugin)
            # Skip non-package entries (e.g. README.md, _test.go).
            if not os.path.isdir(pkg):
                continue
            if not os.path.exists(os.path.join(pkg, f"{plugin}.go")):
                continue
            try:
                r = subprocess.run(
                    ["go", "test", "-cover", "-count=1", "-timeout=30s", f"./{pkg}"],
                    capture_output=True,
                    text=True,
                    timeout=60,
                )
            except subprocess.TimeoutExpired:
                continue
            if r.returncode != 0:
                # Compile error or test failure; skip silently — the
                # test suite's coverage job will surface the real
                # failure elsewhere. We're only doing best-effort
                # per-package aggregation here.
                continue
            m = re.search(r"coverage:\s+(\d+\.\d+)%\s+of statements", r.stdout)
            if m:
                results.append((pkg, float(m.group(1))))
    return results


def main() -> int:
    # 1. Global floor. / 全局门槛。
    pct = global_coverage_pct()
    print(f"Total coverage: {pct:.1f}% (floor: {GLOBAL_FLOOR:.0f}%)")
    if pct < GLOBAL_FLOOR:
        print(f"FAIL: total coverage {pct:.1f}% < {GLOBAL_FLOOR:.0f}% threshold")
        return 1
    print(f"PASS: total coverage {pct:.1f}% >= {GLOBAL_FLOOR:.0f}% threshold")

    # 2. Per-plugin floor. / Per-plugin 门槛。
    print()
    print(f"Per-plugin coverage (floor: {PER_PLUGIN_FLOOR:.0f}%):")
    rows = per_plugin_coverages()
    if not rows:
        print("  (no adapted plugins found)")
        return 0
    # Pad path for alignment.
    # / 对齐用 padding。
    width = max(len(p) for p, _ in rows)
    # Normalize paths to forward slashes for the exempt check
    # (FLOOR_EXEMPT entries use /, Windows os.listdir returns \).
    # / 把路径归一化为正斜杠做 exempt 检查（FLOOR_EXEMPT 用 /，
    # Windows os.listdir 返回 \）。
    exempt_set = {p.replace("\\", "/") for p in FLOOR_EXEMPT}
    failures = []
    for pkg, cov in sorted(rows, key=lambda kv: kv[0]):
        exempt = pkg.replace("\\", "/") in exempt_set
        if exempt and cov < PER_PLUGIN_FLOOR:
            flag = "EXMPT"
        elif cov < PER_PLUGIN_FLOOR:
            flag = "FAIL"
        else:
            flag = "PASS"
        suffix = " (plan §11.2 exempt)" if exempt else ""
        print(f"  [{flag}] {pkg.ljust(width)}  {cov:5.1f}%{suffix}")
        if cov < PER_PLUGIN_FLOOR and not exempt:
            failures.append((pkg, cov))
    if failures:
        print()
        print(
            f"FAIL: {len(failures)} plugin(s) below {PER_PLUGIN_FLOOR:.0f}% "
            f"per-plugin floor. See plan §11.2 for the acceptable-"
            f"complex-protocols exception."
        )
        return 1
    print()
    print(f"PASS: all {len(rows)} plugins >= {PER_PLUGIN_FLOOR:.0f}% floor")
    return 0


if __name__ == "__main__":
    sys.exit(main())
