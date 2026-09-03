#!/usr/bin/env python3
"""ci-doc-drift-check.py — verify that each EN/ZH doc pair has
the same number of section headers (## / ###). A mismatch
suggests one side was updated and the other wasn't.

The drift check is a coarse heuristic (header count), not a
semantic diff. Its purpose is to catch the most common form
of bilingual drift — a contributor updating only one side —
without requiring a full text comparison (which would be brittle
for any natural-language rephrasing).

Run from repo root:
    python3 scripts/ci-doc-drift-check.py
Exit code 0 on pass, 1 on drift.
"""
import re
import sys
from pathlib import Path

# Each pair is (EN, ZH) sharing the same basename structure
# (the .md <-> .zh-CN.md convention). / 每对是 (EN, ZH)，
# 共享同一基名结构 (.md <-> .zh-CN.md 约定)。
PAIRS = [
    ("CHANGELOG.md", "CHANGELOG.zh-CN.md"),
    ("SECURITY.md", "SECURITY.zh-CN.md"),
    ("README.md", "README.zh-CN.md"),
    ("docs/CONFIGURATION.md", "docs/CONFIGURATION.zh-CN.md"),
    ("docs/PLUGIN_GUIDE.md", "docs/PLUGIN_GUIDE.zh-CN.md"),
    ("docs/RELEASE.md", "docs/RELEASE.zh-CN.md"),
    ("docs/ARCHITECTURE.md", "docs/ARCHITECTURE.zh-CN.md"),
    ("docs/SECURITY.md", "docs/SECURITY.zh-CN.md"),
    ("docs/README.md", "docs/README.zh-CN.md"),
]

HEADER_RE = re.compile(r"^#{1,6}\s", re.MULTILINE)


def count_headers(path: Path) -> int:
    if not path.exists():
        return 0
    return len(HEADER_RE.findall(path.read_text(encoding="utf-8")))


def main() -> int:
    drift = []
    for en_rel, zh_rel in PAIRS:
        en = Path(en_rel)
        zh = Path(zh_rel)
        en_h = count_headers(en)
        zh_h = count_headers(zh)
        if en_h != zh_h:
            drift.append((en_rel, en_h, zh_rel, zh_h))
            print(
                f"DRIFT: {en_rel} ({en_h} headers) vs {zh_rel} ({zh_h} headers)",
                file=sys.stderr,
            )
        else:
            print(f"OK: {en_rel} <-> {zh_rel} ({en_h} headers)")
    if drift:
        print(f"\n{len(drift)} doc pair(s) drifted. Update the lagging side.",
              file=sys.stderr)
        return 1
    print("\nNo drift detected.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
