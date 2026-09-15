# FG-QiMen

> **Scan + identify, pushed to the extreme** — comprehensive, deep, fast,
> stable.
>
> **More features ≠ better.** A pure scanner + credential tester. No exploit,
> no persistence, no post-auth action — by design.

FG-QiMen is a pure CLI scanner that decouples the **port scanner (producer)** from
the **plugin workers (consumer)** via a Go channel pipeline. It supports three
run modes (`scan` / `crack` / `linked`) and two work modes (ephemeral oneshot
vs persistent project workspace with bbolt state).

**The name is the mission.** *FG* stands for the Mohist doctrine of
non-aggression (非攻, *Féi Gōng*) — the scanner identifies, it never attacks.
*QiMen* comes from Qimen Dunjia (奇门遁甲), the ancient art of measurement and
deduction — measure first, then scan.

[中文文档](README.zh-CN.md) · [Releases](https://github.com/LCUstinian/FG-QiMen/releases) · [Changelog](CHANGELOG.md)

```
┌ FG-QIMEN 0.9.0 ─ project: demo ─ mode: scan──────────────────────────────────────────────────────────────────────────  SCANNING  ┐
│  [ ▶ IDENTIFY ]  ~30s  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▂▂▃▃▄▅▅▆▆▇█│
│  rate: 28.5 hits/s    ports: 142.0/s    probed 18 / 0                                                                │
├──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
├────────────────────────────────────────────────┬─────────────────────────────────────────────────────────────────────┤
│  PROGRESS                                      │  LIVE EVENTS                                                follow ▼│
│  alive    ░░░░░░░░░░░░░░░░░░░░ 18/0            │  14:23:00  [+]  10.0.0.1:22            ssh                          │
│  ports    ░░░░░░░░░░░░░░░░░░░░ 142/0           │  14:23:01  [-]  10.0.0.2:80            http                         │
│  done 142   inflight 0                         │  14:23:02  [+]  10.0.0.3:6379          redis                        │
│  deferred 0   stall 0s                         │  14:23:03  [*]  10.0.0.4:443           https                        │
│                                                │  14:23:04  [~]  10.0.0.5:445           smb                          │
│  TOP PLUGINS                                   │  14:23:05  [-]  10.0.0.6:3306          mysql                        │
│  9          ██████░░░░░░  http-title           │  14:23:06  [+]  10.0.0.7:22            ssh                          │
│  6          ██████░░░░░░  ssh-banner           │  14:23:07  [-]  10.0.0.8:80            http                         │
│  3          ██████░░░░░░  redis                │  14:23:08  [+]  10.0.0.9:6379          redis                        │
│                                                │  14:23:09  [*]  10.0.0.10:443          https                        │
├────────────────────────────────────────────────┴─────────────────────────────────────────────────────────────────────┤
│  ERRORS: (none)                                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│[q] quit  [p] pause  e errors panel  L live overlay  ? toggle help                                                    │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Pure scanner, by design

A scanner + credential tester for authorized work. FG-QiMen stops at
scanning, identification, and credential verification — no exploit, no
post-auth action, no persistence. That is a tactical choice, not a
missing feature:

- **Attack traffic is the loudest traffic.** On a defended intranet,
  exploit attempts and POC launches are exactly what sets off alerts —
  a pure sweep finishes before anyone is even looking. Spend the noise
  budget later, on the one move that matters.
- **Exploits can take the target down.** A POC written for one version
  fired against another can crash the service — sometimes the whole
  host — turning a recon pass into a production outage. A scan only
  reads; that risk stays off the table.
- **Off-the-shelf attacks rarely pay.** A generic exploit fired at
  random targets almost never beats the one picked after a human reads
  the structured results.
- **The deliverable is decision-grade data.** Structured service
  identities, web/TLS fingerprints, credential hits — the machine maps
  the terrain, the operator aims the next step.

Full contract: [`docs/SECURITY.md`](docs/SECURITY.md).

---

## Why FG-QiMen

One binary, two postures: a **plain intranet inventory scanner** for
daily asset work, and a **low-noise, high-freedom red-team/APT recon
tool** for engagements where every packet must earn its place. Neither
posture comes from "more features" — it comes from three commitments:

1. **It measures before it scans.** Environment profiling samples RTT and
   loss across your targets first, then derives timeout and concurrency
   from live data: mean+4σ timeout from a 64-sample RTT ring, an AIMD
   congestion-control pool with slow start. Fast LAN sweeps collapse a 3s
   wait to ~600ms (**≈5× faster**); lossy WANs back off instead of
   drowning in retry storms. Anything you set explicitly always wins.
2. **It identifies, not just connects.** Every service hit carries a
   structured `product`/`version`/`confidence` identity — TCP by default,
   opt-in UDP (`--udp`) probing from nmap payloads. Web hits add
   status/title/server/matched fingerprints; HTTPS adds the **TLS
   leaf-certificate identity** (SAN/CN fields routinely expose internal
   hostnames banner matching never sees); RDP adds build/NLA/OS posture.
3. **It ships proof, not promises.** Every release carries cosign keyless
   signatures, CycloneDX + SPDX SBOMs, and SLSA L2 provenance — verify
   the binary before you run it, or rebuild from the tag and compare
   hashes byte-for-byte.

Those commitments are the tagline's four promises made real:
**comprehensive** full-spectrum coverage, **deep** decision-grade
identity, **fast** through an engine that tunes itself, and **stable**
when the network is at its worst.

<!-- gendocs:stats begin -->
**44 service plugins · 27 credential-capable · 3333 built-in web fingerprint rules · 84 UDP probe payloads**
<!-- gendocs:stats end -->

### Head-to-head

| Dimension | Fixed-parameter scanners | FG-QiMen |
|---|---|---|
| **Timeout** | one static per-probe value — every filtered port burns it | mean+4σ from a 64-sample RTT ring<br>3s → ~600ms on fast LANs (**≈5× faster**), slow paths keep the operator ceiling |
| **Concurrency** | fixed threads; one congested segment poisons the whole run | AIMD pool — slow start, additive growth while healthy, multiplicative backoff on congestion/RTT signals<br>`--threads` stays a hard cap |
| **Dead-target waste** | probes every host on every segment | two-phase /24 gateway pre-screen + host exclusion (CIDR, range, `192`/`172`/`10` RFC1918 shortcuts)<br>dead segments get zero traffic |
| **Service coverage** | TCP only | opt-in UDP probing (nmap payload DB) with the same structured identity as TCP (`--udp`, `--udp-strict`) |
| **Identity depth** | "port open" + raw banner | structured product/version/confidence<br>web: status/title/server/fingers + TLS SAN/CN<br>RDP: build/NLA/OS |
| **State & resume** | one-shot; a Ctrl+C costs the whole run | bbolt project workspace: resume, prune, export/import, cron schedules |
| **Operator experience** | log lines scrolling past | live TUI (stage ETA, hit feed, plugin chart, error breakdown) or clean plain text<br>txt/json/csv sinks with daily buckets |
| **Supply chain** | bare binaries | cosign signatures, dual SBOMs, SLSA L2 provenance, SHA-pinned CI actions<br>verify before you run |

### What that buys you

- **Healthy /24 LAN**: profiling tightens the timeout budget, the pool
  ramps to full concurrency, and the sweep finishes in seconds — with
  structured JSON you can pipe straight into other tooling.
- **Lossy VPN/WAN**: the pool backs off multiplicatively instead of
  hammering — fewer false "unreachable" verdicts, fewer retry storms.
- **Interrupted run**: Ctrl+C drains the pipeline and flushes state;
  `resume` continues the project instead of starting from zero.

---

## Quick start

```bash
# ephemeral scan
fg-qimen -H 192.168.1.0/24

# persistent project with bbolt state
fg-qimen --project corp -H 10.0.0.0/24 --mode linked

# resume a paused project
fg-qimen resume --project corp

# list projects
fg-qimen projects list
```

### Build

Requires Go 1.26+ and [`just`](https://github.com/casey/just).

```bash
just build         # → release/fg-qimen[.exe]
just all           # → release/fg-qimen-{os}-{arch}[.exe]
just --list
```

### Basic scan

```bash
# scan a /24 with default ports
fg-qimen -H 192.168.1.0/24

# specific ports
fg-qimen -H 192.168.1.0/24 --ports 22,80,443,3389,8080

# single host
fg-qimen -H 10.0.0.5 --ports 22,80,3306,6379,8080 -t 50

# custom output paths
fg-qimen -H 10.0.0.5 -ot myscan.txt -oj myscan.json
```

> **Tip — keep scan output out of the repo root:** by default the
> workspace (result sinks, bbolt state, daily buckets) is created at
> `./fgqm_workspace` relative to the cwd. Point it elsewhere with
> `--workspace <dir>` or the `FGQI_WORKSPACE` env var (flag wins) so
> debug runs and scratch scans never litter the project directory.

> **Tip — faster alive discovery on Windows:** without elevation the
> ICMP prober cannot open a raw socket, so alive detection falls back
> to spawning `ping.exe` per host (works, but slower). Run the scanner
> from an elevated shell to enable the fast ICMP path.

### Project mode

```bash
# one-time project creation
fg-qimen projects create corp-intranet

# populate targets
echo "10.0.0.0/24"   >  fgqm_workspace/projects/corp-intranet/targets.txt
echo "10.0.1.0/24"   >> fgqm_workspace/projects/corp-intranet/targets.txt

# linked mode (scan + credential test in one pass)
fg-qimen --project corp-intranet -f fgqm_workspace/projects/corp-intranet/targets.txt --mode linked \
    -u root,admin -p 123456,admin P@ssw0rd

# resume / info
fg-qimen resume --project corp-intranet
fg-qimen projects info corp-intranet

# retention: drop resume state (seen-hashes) older than a cutoff.
# results / creds are never touched. --yes skips the confirm prompt.
fg-qimen projects prune corp-intranet --before 2026-09-01 --compact --yes
```

### TUI

The TUI is **on by default** when stdout is a TTY. Force plain text with
`--no-tui`.

The dashboard composes six regions driven by a 3-breakpoint responsive
layout (narrow <80 / medium 80–119 / wide ≥120 columns); wide terminals
place STAGE and TOP PLUGINS side-by-side, narrower ones stack them:

- **Header**: per-stage `[ ▶ STAGE ]` badge with ETA on the right
  (`[ ▶ ALIVE ]   ETA ~12s`); scan rate in hits/s and ports/s
  (EWMA-smoothed) plus a 60-sample hits/s sparkline; mid-alive-sweep
  "alive N/M" counter ticks up as probes complete (no more
  stuck-at-zero until alive finishes).
- **LIVE EVENTS**: the last 20 events in a fixed ring buffer (never
  grows), severity-coloured (`✓` cred hit, `✗` error, `⚠` warning);
  each hit flashes red for ~200ms. Hidden on narrow terminals; `L`
  overlays the last 5.
- **STAGE** (left / upper): alive and ports rendered as `▓/░` progress
  bars against their totals; results / creds / errors stay as plain
  counters.
- **TOP PLUGINS** (right / lower): the 5 plugins with the most hits
  this run, rendered as a fixed-width bar chart with the
  `[plugin N]` name on the left and a `████░░` bar showing share.
- **ERRORS** (bottom): a compact `ERRORS: timeout 42  refused 15` line;
  `e` expands it to the top-4 category bars, `E` collapses it back.
  Categories come from `core.ClassifyError` (errors.Is / errors.As
  first, substring fallback).
- **Footer**: keymap hints — `[q] quit  [p] pause  e errors panel
  L live overlay  ? toggle help` (`?` opens the full help overlay).

All of this is read off a `CountersView` projection on
`internal/types.State` so the view layer is decoupled from the
scanner's internal channel layout.

### Dictionary files

```text
# users.txt (one username per line)
admin
root
test
oracle
postgres
```

```text
# pass.txt (one password per line; `#` lines are skipped)
# top-10 worst passwords
123456
password
admin
root
qwerty
```

```bash
fg-qimen -H 10.0.0.0/24 --ports 22,3306 -uf users.txt -pf pass.txt
fg-qimen scan --mode crack -f targets.txt -uf users.txt -pf pass.txt --project corp
```

---

## Features

### Architecture

- **Pipeline decoupling**: port scan (producer) → `chan ScanItem` → plugin
  workers (consumer). All stages honor `context.Context` for cancellation.
- **Three run modes**: `scan` / `crack` / `linked` (see [CLI reference](#cli-reference)).
- **Project workspace**: each project gets its own directory + bbolt DB.
- **Incremental tracking**: SHA-1-based dedup with optional bbolt persistence;
  `--resume` reloads the seen-set.
- **TUI**: Bubbletea + Lipgloss cyberpunk theme (green / amber / red on
  black); auto-fallback to plain text on non-TTY.

Full architecture write-up: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

### Output formats

Result files carry a `HH-MM-SS` local-time start stamp in the
filename so two same-day runs don't clobber each other, and live
under a `YYYY-MM-DD/` daily bucket directory (example:
`fgqm_result_14-30-22.txt`). One exception: `fgqm_creds.txt` has no
stamp — the file is opened `O_APPEND` and dedup runs on the in-memory
`State`.

| Sink | Contents |
|---|---|
| `fgqm_result_<time>.txt` / `.ndjson` / `.csv` | scan results — human-readable lines / NDJSON (one JSON object per line) / RFC 4180 CSV |
| `fgqm_creds.txt` | credential hits (cleartext; operator's working file) |
| `fgqm_rdp_<time>.ndjson` / `.txt` | RDP deep fingerprint: hostname, build, NLA flag, OS |
| `fgqm_web_<time>.ndjson` / `.txt` | structured web fingerprint per webtitle hit: URL, status, title, server, matched fingers — https targets add the TLS leaf identity (subject, SANs, issuer, validity dates, protocol version); the SAN/CN fields routinely expose internal hostnames banner matching never sees |
| `fgqm_alive_<time>.txt` | one IP per line — dedup'd host list ready for `nmap -iL` / `masscan --targets` / `curl` loops |
| `fgqm_log_<time>.txt` | run log archive (`HH:MM:SS [level] message`, same lines as the console stream); written automatically per scan — text mode (`--no-tui`) tees to console + file, TUI mode and `--silent` write file-only; created `0600` since credential-hit lines carry cleartext passwords |
| `fgqm_discovery_<time>.ndjson` / `.txt` | out-of-scope host discoveries from protocol interactions (NBNS, SMB, TLS SAN, ...): address, hostname, source protocol, discovery time; recorded on every run, opt-in scanning via `--expand-scope` |
| `fgqm_shares_<time>.ndjson` / `.txt` | SMB null-session share enumeration (`--share-enum`): share names, access level, directory metadata — evidence-only, file contents are never downloaded |
| `fgqm_ftp_<time>.ndjson` / `.txt` | FTP directory walks (`--ftp-enum`): anonymous or weak-credential logins, directory tree metadata — deliberately separate from share findings |
| `fgqm_servers_<time>.ndjson` / `.txt` | aggregated important-server inventory (DC / file / backup / VPN / DB / ...): IP, hostname, roles, evidence ports |

Notes:

- `<time>` is the `HH-MM-SS` start stamp; the `.ndjson` extension is
  deliberate — editors that validate a whole `.json` file as a single
  document would flag multi-line output as invalid.
- Explicit paths via `-ot` / `-oj` / `-oc` bypass both the bucketing
  and the stamp.

### Evidence, scope & server inventory

Three v0.9 capabilities extend the "deliver evidence" promise beyond
identification:

- **Out-of-scope discoveries** — every protocol interaction that surfaces
  a foreign host is recorded with time + source. `--expand-scope auto`
  runs ONE bounded extra round over the new hosts (RFC1918 private IPs
  only, same /24 as scanned targets, `--exclude-hosts` still applies,
  ≤256 hosts). Default `off` = record + rerun hint; the scanner never
  blocks on an interactive prompt.
- **Read-only share/FTP enumeration** — null-session SMB share listings
  and anonymous / weak-credential FTP walks, depth- and entry-capped,
  metadata only: no file downloads, no writes, no post-auth actions.
  Share and FTP findings land in separate files, never merged.
- **Important-server inventory** — hosts matching infrastructure roles
  (domain controller, file server, backup, VPN, database, ...) are
  aggregated with NBNS hostname enrichment into `fgqm_servers.*` and
  tagged `important` in the NDJSON stream.

### Plugins and credential coverage

The full plugin roster — names, default ports, and Identify/Credential
capabilities — is generated straight from the binary's live registries and
machine-checked in CI:

- [`docs/PLUGINS.md`](docs/PLUGINS.md) — every registered plugin, with default ports and capability matrix
- [`docs/FLAGS.md`](docs/FLAGS.md) — every CLI flag, grouped, with defaults

Credential testing covers every service marked ✅ in `PLUGINS.md`
(authenticator registry), all under the no-exploit enforcement
(`fgqm_creds.txt` is the only side effect).

IPv6 is first-class (single IP / CIDR / comma-list). Custom web-fingerprint
rulesets load via `--web-fingerprint <path>` (FG-QiMen native JSON or EHole
format; merged with the built-in rules). RDP NLA posture (HYBRID / SSL /
legacy) is detected by the `rdp-nla` plugin; full CredSSP authentication is
deferred.

---

## CLI reference

```
fg-qimen [flags]                                # implicit scan
fg-qimen scan [target] [flags]                  # explicit scan; target may be a CIDR/range/host
fg-qimen resume --project <name>                # resume project
fg-qimen projects list                          # list projects
fg-qimen projects create <n>                    # create project
fg-qimen projects delete <n>                    # delete project
fg-qimen projects info <n>                      # show project details
fg-qimen projects export <n> <out.fgq>          # export project to single .fgq file
fg-qimen projects import <in.fgq> <n>           # import from .fgq file
fg-qimen projects prune <n> --before <date>     # delete seen-hashes older than <date> (--compact reclaims disk)
fg-qimen schedules add <name> --cron "<expr>"   # persist a schedule in the project DB
fg-qimen schedules list                         # inspect queued schedules
fg-qimen schedules remove <name>                # drop a schedule
fg-qimen version                                # show version
fg-qimen completion bash                        # generate shell completion
```

### Quick start (6 essential flags)

For ~90% of scans you only need these six flags:

| Short | Long | Example | Purpose |
|---|---|---|---|
| `-H` | `--host` | `-H 10.0.0.0/24` | target IP / CIDR / range / comma-list |
| — | `--project` | `--project corp` | named project (persists to bbolt; omit for ephemeral) |
| `-u` | `--user` | `-u root,admin` | inline usernames (comma-separated for multiple) |
| `-p` | `--pass` | `-p admin,root` | inline passwords (comma-separated for multiple) |
| `-uf` | `--user-file` | `-uf users.txt` | usernames dictionary file (one per line) |
| `-pf` | `--pass-file` | `-pf pass.txt` | passwords dictionary file (one per line) |

The four most common pairings:

```bash
-H 1.0.0.0/8 -u admin -p root,toor              # host + inline creds
-H 1.0.0.0/8 -uf users.txt -pf passes.txt       # host + wordlists
-H 1.0.0.0/8 -f targets.txt -a                  # hosts file + alive-only
-H 1.0.0.0/8 -ot r.txt -oj r.json -oc r.csv      # all three output sinks
```

Concrete recipes:

```bash
# Minimal scan: 256-host /24 against default ports
fg-qimen -H 10.0.0.0/24

# Named project with dictionaries + small thread count
fg-qimen --project corp -H 10.0.0.0/24 -uf users.txt -pf pass.txt -t 50

# Re-attack a saved project against its previously-seen hosts
fg-qimen resume --project corp

# Crack-only: skip alive + port scan, just try creds
fg-qimen scan --project corp --mode crack -uf users.txt -pf pass.txt

# Behind an HTTP proxy (chains through any plugin's dialer)
fg-qimen -H 10.0.0.0/24 --proxy http://127.0.0.1:8080
```

> **Short-flag convention**: all lowercase, mnemonic only,
> 2-letter for namespaces (output-* / user-pass-file). `-H` is the
> sole uppercase (it avoids the `-h`/`--help` collision that cobra
> reserves). See [CHANGELOG](CHANGELOG.md) for the migration
> table from v0.5.0.

### Full flag reference

The complete flag table lives in [`docs/FLAGS.md`](docs/FLAGS.md) — it is
**generated from the same registry the binary serves**, so it can never
drift from `fg-qimen --help` (a CI guard test fails the build when it
does). `fg-qimen --help` remains the authoritative terminal rendering.

Complete CLI usage templates (one per common workflow) live in
[`docs/CONFIGURATION.md`](docs/CONFIGURATION.md).

---

## Verifying releases

Each GitHub Release ships **11 standard platform binaries + 2 hardened
editions** (linux-amd64, windows-amd64 — garble + UPX, not reproducible
by design) plus per-binary cosign signatures, signing certificates, and
CycloneDX SBOMs:

| File | Purpose |
|---|---|
| `fg-qimen-<platform>` | Compiled standard binary (no `.exe` on Linux/macOS/BSD) |
| `fg-qimen-<platform>-hardened` | Hardened binary (garble obfuscation + UPX compression; `.exe` suffix on Windows) |
| `SHA256SUMS` | sha256 checksums for every binary (13 entries) |
| `*.sig` | cosign keyless signature (OIDC, Sigstore) |
| `*.pem` | signing certificate embedding the OIDC identity |
| `*.sbom.json` | CycloneDX SBOM for the binary (per-platform) |
| `FG-QiMen-release.spdx.json` | Full SPDX SBOM across all release artifacts |

### 1. Checksums

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

A clean run prints `<binary>: OK` for each line.

### 2. Signature (keyless, OIDC)

The release pipeline uses
[cosign](https://github.com/sigstore/cosign) in keyless mode against
Sigstore's public good instance — no secret keys in the repository.

```bash
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

COSIGN_EXPERIMENTAL=1 cosign verify-blob \
  --signature fg-qimen-<platform>.sig \
  --certificate fg-qimen-<platform>.pem \
  --certificate-identity-regexp 'https://github.com/LCUstinian/FG-QiMen' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  fg-qimen-<platform>
```

A successful verify prints the SHA256 of the verified binary and the OIDC
identity that signed it. The certificate pins the GitHub Actions workflow
identity (`https://github.com/LCUstinian/FG-QiMen/.github/workflows/release.yml@refs/tags/<TAG>`).

### 3. SBOM

The CycloneDX SBOM lists every direct + transitive dependency the binary
links against. It can be ingested directly by
[Dependency-Track](https://dependencytrack.org/) or any SBOM-aware SCA tool.

```bash
# inspect components
jq '.components[] | {name, version, purl}' fg-qimen-<platform>.sbom.json
```

### 4. Source reproducibility (optional)

To rebuild a binary byte-for-byte from the matching tag:

```bash
git checkout <TAG>
go build -trimpath -ldflags='-s -w -buildid=' -o fg-qimen-local .
sha256sum fg-qimen-local
```

The hash must match the corresponding line in `SHA256SUMS`.

### 5. Reporting a discrepancy

If any of the above fails, **do not run the binary**. Open a GitHub issue at
<https://github.com/LCUstinian/FG-QiMen/issues> with the failing step's
output and the tag you tried.

---

## Localization

- **Code comments**: bilingual (Chinese + English) on every public function,
  struct, and key logic block.
- **Terminal output**: 100% English (banner, help, log, error).
- **README**: split — English ([README.md](README.md)) + Simplified Chinese
  ([README.zh-CN.md](README.zh-CN.md)).
- **CLI flag names**: English.
- **Generated docs** ([FLAGS.md](docs/FLAGS.md), [PLUGINS.md](docs/PLUGINS.md)):
  English, matching the terminal-output policy — they are renderings of
  the registry, not prose.

---

## Graceful Ctrl+C

- First **Ctrl+C**: `cancel()` root context → pipeline drains → output
  flush → bbolt `Sync()` → exit code 130.
- Second **Ctrl+C** within `--shutdown-timeout` (default 5s): hard exit
  (`os.Exit(1)`).

---

## Roadmap

Work in flight is tracked in [CHANGELOG.md](CHANGELOG.md)'s
`[Unreleased]` section. Current themes: adaptive-scanning refinement,
UDP service-coverage expansion, and supply-chain hardening.

---

## Attribution

FG-QiMen stands on the shoulders of several open-source projects. All reused
code is MIT-licensed; the per-file modification history lives in the source
headers.

**Primary inspiration**: [fscan](https://github.com/shadow1ng/fscan) by
[shadow1ng](https://github.com/shadow1ng) (MIT) — the pipeline-decoupled
scanner architecture, the service Identify + Credential plugin pattern,
and the Nmap-style port-fingerprint framework. FG-QiMen inherits the
**no-exploit** policy and drops every unauthorized-access / write / POC path
the original carried.

Full attribution bundle with third-party license texts:
[`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md).

The FG-QiMen source is released under the MIT License. See
[LICENSE](LICENSE).

---

## Disclaimer

This tool is for **authorized security testing and learning only**. Do not
scan targets without permission. The authors are not responsible for misuse.
