# Performance Baseline (v0.4)

> Hardware: Intel(R) Core(TM) Ultra 9 285K (24 cores), Windows 11
> Go 1.26
> Run with: `go test -bench=. -benchtime=3s -run=^$ ./internal/core/`
> Linux CI numbers may differ (Windows loopback has Defender / SmartScreen
> interference on TCP — see notes per benchmark).

| Benchmark | ns/op | ops/sec | Notes |
|---|---|---:|---|
| `BenchmarkPortScanClosedPort`    |   96,573 |    10,355 | TCP connect to 127.0.0.1:64535 (closed). Linux CI expected ~5-10× faster without Defender / SmartScreen delay. |
| `BenchmarkExpandTargetsCIDR`     |  640,845 |     1,560 | 4,096 IPs (10.0.0.0/20) via `ExpandTargetsStream`. |
| `BenchmarkHashKey`               |     113 | 8,850,000 | SHA-1 16-byte truncated dedup key. |
| `BenchmarkBuildPortIndex2`       |   4,417 |   226,000 | 30-plugin port → plugin lookup index construction. |
| `BenchmarkCrossIterator`         |   2,409 |   415,000 | 1000 hosts × 5 ports Cartesian product (5,000 items). |

## Observations

- **Per-target expansion cost is dominated by `HashKey` calls**, not
  the iterator itself. ~113 ns × 5,000 targets = ~565 µs of pure
  hashing per CrossIterator run — measurable but not bottlenecking.
- **Port index construction is sub-microsecond-per-plugin** at 30
  plugins; would stay fast up to ~5,000 plugins.
- **TCP connect latency on Windows is the floor** — Defender /
  SmartScreen round-trip dominates `PortScanClosedPort`. Linux
  CI numbers should be the user-visible baseline.

## What this baseline doesn't cover (deferred)

- End-to-end pipeline throughput (port-scan + identify + credential
  spray). A future `BenchmarkRunScanPipelineStub` should be added
  when a stable in-process fake target server is available.
- Memory profile under sustained load (`-memprofile=...`).
- `BenchmarkRunScanWithRealNetwork` against a local Docker target —
  blocked by the "fake server" backlog from the v0.4 plan.

## Reporting workflow

```bash
# local run
go test -bench=. -benchtime=3s -run=^$ ./internal/core/ | tee /tmp/bench.txt

# CI run (Linux runner expected to be ~5-10× faster on the
# PortScanClosedPort line due to no Defender hook in the loopback
# path). CI uploads the bench output as an artifact.
```

This file is updated by hand when a commit materially shifts the
numbers (e.g. a fast-path optimisation, a new allocator, a race-
safe batched write).