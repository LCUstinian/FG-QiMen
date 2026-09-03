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
  - v0.6 target: 65%. Blocked on plugin fake-server
    infrastructure — adapted plugins run real I/O (TCP
    connect, SMTP/IMAP, HTTP, MQTT broker, etc.) and need
    httptest / fake-TCP fixtures to be unit-testable. The
    cmd/ package itself is at 64.4% on unit-testable code;
    the gap to 65% is purely plugin coverage. / v0.6 目标：
    65%。被 plugin fake-server 基础设施卡住——adapted plugin
    跑真 I/O（TCP connect、SMTP/IMAP、HTTP、MQTT broker 等），
    需要 httptest / fake-TCP fixture 才能单元测。cmd/ 单元可
    测代码已达 64.4%；到 65% 的差距完全是 plugin 覆盖。
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
    # See docstring for why 65% is deferred to v0.6.
    # 65% 是 v0.6 目标，见顶部 docstring。
    floor = 60.0
    if pct < floor:
        print(f"FAIL: coverage {pct:.1f}% < {floor:.0f}% threshold")
        return 1
    print(f"PASS: coverage {pct:.1f}% >= {floor:.0f}% threshold")
    return 0


if __name__ == "__main__":
    sys.exit(main())
