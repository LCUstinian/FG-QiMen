#!/usr/bin/env python3
"""ci-coverage-check.py — read coverage.out and enforce the
60% floor on the GitHub ubuntu-latest CI runner.

Used as a step in .github/workflows/ci.yml's coverage job.
Replaces the previous bash + awk + sed pipeline which was
intermittently exit-127'ing on the GHA runner (suspected:
the runner's default PATH not matching what the workflow
file expected, plus SIGPIPE on the tail -1 in some GHA
image variants). Python 3 is pre-installed on every
ubuntu-latest image.
"""
import re
import subprocess
import sys


def main() -> int:
    # 1. Generate the cover-func output (writes to stdout).
    #    We could read coverage.out and parse it ourselves, but
    #    delegating to go tool cover -func keeps the format
    #    definition in one place (the Go toolchain). / 用 go tool
    #    cover -func 拿 cover-func 输出（写 stdout）。我们也可以
    #    直接 parse coverage.out，但委托给 go tool cover -func
    #    把格式定义单点放在 Go 工具链。
    result = subprocess.run(
        ["go", "tool", "cover", "-func", "coverage.out"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr)
        return result.returncode

    # 2. Find the `total:\t...\tNN.N%` line. / 找 `total:\t...\tNN.N%` 行。
    m = re.search(r"total:\s+\(statements\)\s+(\d+\.\d+)%", result.stdout)
    if not m:
        sys.stderr.write("FAIL: could not parse go tool cover -func output\n")
        sys.stderr.write(result.stdout)
        return 1

    pct = float(m.group(1))
    print(f"Total coverage: {pct:.1f}%")

    # 3. Enforce the 60% floor. / 强制 60% 门槛。
    floor = 60.0
    if pct < floor:
        print(f"FAIL: coverage {pct:.1f}% < {floor:.0f}% threshold")
        return 1
    print(f"PASS: coverage {pct:.1f}% >= {floor:.0f}% threshold")
    return 0


if __name__ == "__main__":
    sys.exit(main())
