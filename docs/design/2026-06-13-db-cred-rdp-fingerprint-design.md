# DB Credential Completion + RDP Deep Fingerprint — Design

> Date: 2026-06-13
> Slice: credentials (DB completion) + fingerprint (RDP deep)
> Vulnerability side: explicitly excluded (per project no-exploit stance, obs #151)

## Context

FG-QiMen v0.1 ships 13 Identify plugins and 8 Credential authenticators
(`SSH / FTP / MySQL / Redis / Memcached / MongoDB / MSSQL / SMB`). Three
gaps remain in the credential and fingerprint space that the user has
chosen to close in a single, deep-dive round:

1. **PostgreSQL Credential** — `plugins/adapted/postgresql` is Identify-only
   today (raw `StartupMessage` probe). No authenticator exists.
2. **Elasticsearch Credential** — `plugins/adapted/elasticsearch` is
   Identify-only (HTTP `GET /` + JSON parse). No authenticator exists.
3. **RDP Deep Fingerprint** — `common.RDPFingerprint` struct and
   `common.Output.WriteRDP()` are already in place (pre-shaped by v0.1
   plan obs #372), but no plugin exists to fill them. The v0.1 README
   still says "RDP deep fingerprint (planned v0.2)".

User directive (verbatim): *"凭据认证类的可以，命令执行或别的漏洞不行"* —
credential authentication is fine; command execution or other
vulnerabilities are not.

## Goals

- **G1.** Add `core/cred/protocols/postgresql.go` with `lib/pq`-backed
  authenticator. Plugin `Modes()` flips to
  `ModeIdentify | ModeCredential`.
- **G2.** Add `core/cred/protocols/elasticsearch.go` with HTTP Basic
  Auth probe. Plugin `Modes()` flips to
  `ModeIdentify | ModeCredential`.
- **G3.** Add `plugins/adapted/rdp/rdp.go` + `wire.go`. Identify
  performs an NLA-aware TPKT/X.224/COTP/MCS handshake and fills
  `common.RDPFingerprint` with: `ServerName`, `OSBuild`, `OSVersion`,
  `NLASupported`, `ProtocolVersion`. `rdp.json` / `rdp.txt` are now
  populated.
- **G4.** All new code passes `go vet` and `go test ./...`. All
  existing tests stay green.
- **G5.** README reflects the new state: 14 plugins, 10
  authenticators, RDP deep fingerprint delivered (no longer "planned").

## Non-Goals (HARD)

These are **explicitly excluded** from this slice and from any future
slice until the project policy is changed. They are listed here so
that no reviewer mistake can let them in.

- **NG-1. NO empty-credential probe as a hit** — fscan's
  `testUnauthorizedAccess` for PG (empty password → "trust auth" vuln)
  and ES (`Credential{Username: "", Password: ""}` → "unauth access"
  vuln) are **vulnerability detection**, not credential authentication.
  They are not implemented. If a server accepts an empty cred, the
  pipeline logs nothing — the empty cred is just tried and skipped like
  any other wrong cred.
- **NG-2. NO post-auth query** — never run `SELECT version()`,
  `SHOW DATABASES`, `db.version()`, `GET _cluster/health`, or any
  query. `PingContext` for SQL drivers and `GET /` for ES are the only
  protocol-level side effects.
- **NG-3. NO NLA real credential test** — RDP NLA authentication with
  NTLM/Kerberos (fscan's `rdpCrack`) requires establishing a CredSSP
  session and is a credential-test path, but for RDP it requires
  session-level work that we explicitly defer to v0.3+. The RDP plugin
  is **Identify only** in this slice. No `rdp/cred` is added.
- **NG-4. NO TLS cert extraction** — `RDPFingerprint.CertSubject /
  CertIssuer / CertValidFrom / CertValidTo / CertThumbprint` stay
  empty in this slice. v0.2+ will add a separate TLS upgrade branch
  (PROTOCOL_SSL) to fill them.
- **NG-5. NO command execution on hit** — on a credential hit, the
  pipeline writes to `creds.txt` and nothing else. No SSH `Exec`,
  no SMB `create file`, no Mongo `find`. This is enforced by the
  `cred.Authenticator` contract (see
  [core/cred/cred.go](../../core/cred/cred.go)) and audited by the
  `no-exploit` review checklist.
- **NG-6. NO fscan code in our source** — per obs #945, no
  `fscan` / `shadow1ng` strings appear in FG-QiMen code. Attribution
  lives only in `README.md`'s Acknowledgments section. Wire-protocol
  implementations here are written from MS-RDPBCGR (Microsoft public
  spec) and the lib/pq / database/sql idioms — fscan is read for
  *understanding* but not for *copying*.

## Design — Section 2: PostgreSQL Credential

### Authenticator

```go
// core/cred/protocols/postgresql.go (new)
type PostgreSQLAuthenticator struct{}
func (a *PostgreSQLAuthenticator) Name() string   { return "postgresql" }
func (a *PostgreSQLAuthenticator) DefaultPorts() []int {
    return []int{5432, 5433, 5434} // +5434 for PG replication/backup
}
```

### Flow (per cred, mirrors `mssql.go`)

1. Skip creds with non-empty `c.Method != "password"`.
2. Fall back user: `c.User == ""` → `"postgres"` (PG install default
   superuser).
3. Build DSN: `postgres://<user>:<pass>@<host>:<port>/postgres?sslmode=disable&connect_timeout=<sec>`.
   `net.JoinHostPort` gives `[::1]:5432` for IPv6. `net/url.UserPassword`
   escapes special chars in user/pass.
4. `sql.Open("postgres", dsn)`. Set `ConnMaxLifetime(timeout)`,
   `MaxOpenConns(1)`, `MaxIdleConns(0)`.
5. `db.PingContext(ctx-with-timeout)`. The driver performs
   StartupMessage → SCRAM-SHA-256/MD5/cleartext auth internally.
6. On nil error → `&cred.Hit{Cred: c, Attempts: i+1, Time: now}`.
7. On error → next cred. After all fail → `nil, nil`.

### Plugin change

`plugins/adapted/postgresql/postgresql.go`: change `Modes()` to return
`ModeIdentify | ModeCredential`. Body unchanged.

### Dependency

`go.mod` adds `github.com/lib/pq v1.10.x` (a blank import in the
authenticator file; no other use).

### Tests (`postgresql_test.go`)

Mirror `mongo_test.go`'s `fakeServer` pattern. A `net.Listener` that:

- Accepts the TCP connection.
- Reads the StartupMessage from the client.
- Asserts the user field matches the test input.
- Sends back `'R' AuthenticationOk` (1 byte tag + no body) for the
  happy path, or `'E' ErrorResponse` (1 byte tag + error fields) for
  the miss path.

Cases:

| Case | Expect |
|---|---|
| Right user/pass | `Hit` returned, `Hit.Attempts == 1` |
| Wrong user/pass | `nil, nil`, all creds tried |
| Empty creds list | `nil, nil`, no connection |
| Connection refused | `nil, err` |
| IPv6 host `[::1]` | DSN wraps host in `[::1]`, no panic |

## Design — Section 3: Elasticsearch Credential

### Authenticator

```go
// core/cred/protocols/elasticsearch.go (new)
type ElasticsearchAuthenticator struct{}
func (a *ElasticsearchAuthenticator) Name() string   { return "elasticsearch" }
func (a *ElasticsearchAuthenticator) DefaultPorts() []int {
    return []int{9200, 9300} // match existing Identify plugin
}
```

### Flow (per cred)

1. Skip creds with non-empty `c.Method != "password"`.
2. Fall back user: `c.User == ""` → `"elastic"` (ES install default).
3. For each protocol `https` then `http` (mirror Identify plugin's
   `detectProtocol` pattern):
   - Build `req := http.NewRequestWithContext(ctx, "GET", url, nil)`.
   - If `c.User != ""` or `c.Pass != ""` →
     `req.Header.Set("Authorization", "Basic "+base64(user+":"+pass))`.
   - `client.Do(req)` with `Timeout: timeout` and
     `InsecureSkipVerify: true`.
   - Status codes:
     - `200` AND body contains `"elasticsearch"` OR `"cluster_name"`
       OR `"lucene_version"` → `Hit`.
     - `401` / `403` → wrong cred, next cred.
     - Other → next cred (do not classify as hit).
   - Body read capped at 4 KiB.

**Critical**: the empty-cred path (`c.User == "" && c.Pass == ""`) is
a normal iteration of the loop, NOT a separate "unauth probe". It
just adds no `Authorization` header and is treated like any other
miss. This is NG-1.

### Plugin change

`plugins/adapted/elasticsearch/elasticsearch.go`: change `Modes()` to
return `ModeIdentify | ModeCredential`. Body unchanged.

### Dependency

None — standard library only (`net/http`, `encoding/base64`).

### Tests (`elasticsearch_test.go`)

Use `httptest.NewServer`. Cases:

| Case | Expect |
|---|---|
| Right user/pass | `Hit` returned, attempts == 1 |
| Wrong user/pass | `nil, nil` |
| Empty creds list | `nil, nil`, no requests sent |
| Server returns 200 with non-ES body | `nil, nil` (body guard) |
| Server returns 401 | `nil, nil` |

## Design — Section 4: RDP Deep Fingerprint

### Layered protocol (one-shot summary)

```
TCP
└── TPKT     (RFC 1006)            — 4-byte header: version(1) + reserved(1) + length(2 BE)
    └── X.224 CR (0xE0) / CC (0xD0) — Connection Request / Confirm, BER-encoded
        └── COTP DT (0xF0)          — Data TPDU, variable-length encoding
            └── MCS                 — Multipoint Communication Service
                └── GCC             — Generic Conference Control (T.124)
                    └── serverCore  ← fields we extract
```

### 4-step handshake (no login, no attach)

1. **TPKT + X.224 CR**: send `requestedProtocols = PROTOCOL_HYBRID`
   (0x00000002) so the server's CC tells us whether it supports NLA.
2. **TPKT + X.224 CC**: read the server's `selectedProtocol`. If it
   is `PROTOCOL_HYBRID` or `PROTOCOL_HYBRID_EX`, set
   `fp.NLASupported = true`. Record `fp.ProtocolVersion`.
3. **TPKT + COTP DT + MCS Connect-Initial**: send with
   `gccConferenceCreateRequest`, including a minimal `clientCore` and
   `clientSecurity`. Magic `0x14 0x76 0x62 0x36 0x88 0x4E 0xCE 0x53`
   is the GCC Conference Create Request key.
4. **TPKT + COTP DT + MCS Connect-Response**: read. The
   `gccConferenceCreateResponse` payload contains `serverCore`. Parse
   `clientName` (32 bytes, null-trimmed) and `clientBuild` (4 bytes
   little-endian) and `version` (4 bytes). Close the connection
   immediately — no attach, no login, no session.

### `serverCore` layout (MS-RDPBCGR §2.2.1.3.2)

Fields we extract:

| Offset | Size | Field | Use |
|---|---|---|---|
| 0 | 4 | version | `fp.OSVersion` via `versionToOSName()` table |
| 12 | 4 | clientBuild | `fp.OSBuild` (e.g. 19041 = Win10 2004) |
| 16 | 32 | clientName | `fp.ServerName` (NetBIOS hostname) |

`version` known values:

| Version | OS |
|---|---|
| 0x00080004 | Windows 7 / Server 2008 R2 and later through Server 2016 |
| 0x00080005 | Windows 10 1607+ / Server 2019 / Windows 11 |
| 0x000A0000 | (reserved / unknown — return raw hex) |

### File split

| File | LOC estimate | Role |
|---|---|---|
| `plugins/adapted/rdp/rdp.go` | ~250 | Orchestrator: 4-step handshake, fill `RDPFingerprint`, fall back to `PROTOCOL_RDP` if HYBRID not accepted |
| `plugins/adapted/rdp/wire.go` | ~700 | TPKT/X.224/COTP/MCS/GCC BER encoders and decoders. Pure framing, no business logic. |

### Identify() shape

```go
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
    conn, err := dialRDPRaw(ctx, host, port, 3*time.Second)
    if err != nil { return nil }
    defer conn.Close()

    fp := common.RDPFingerprint{Host: host, Port: port, ScanTime: time.Now()}

    // Step 1+2: probe NLA support
    cc, err := wire.SendX224ConnectionRequest(conn, wire.PROTOCOL_HYBRID)
    if err != nil { return nil }
    fp.NLASupported = cc.SelectedProtocol == wire.PROTOCOL_HYBRID ||
                      cc.SelectedProtocol == wire.PROTOCOL_HYBRID_EX
    fp.ProtocolVersion = uint32(cc.SelectedProtocol)

    if cc.SelectedProtocol == wire.PROTOCOL_SSL {
        // Pure TLS path: v0.2+. v0.1 closes silently.
        return nil
    }

    // Step 3: send MCS Connect-Initial
    if err := wire.SendMCSConnectInitial(conn); err != nil { return nil }

    // Step 4: read MCS Connect-Response, extract serverCore
    sc, err := wire.ReadMCSConnectResponse(conn)
    if err != nil { return nil }
    fp.ServerName = strings.TrimRight(string(sc.ClientName[:]), "\x00")
    fp.OSBuild    = strconv.FormatUint(uint64(sc.ClientBuild), 10)
    fp.OSVersion  = sc.VersionToOSName()

    return &common.Result{
        Host: host, Port: port, Service: "rdp",
        Banner: fmt.Sprintf("RDP %s build=%s nla=%v",
            fp.ServerName, fp.OSBuild, fp.NLASupported),
        Time: time.Now(),
    }
}
```

### Output dual-write

The Identify result goes into `result.txt` / `result.json` like any
other plugin. For the structured `rdp.json` / `rdp.txt` files, the
**pipeline** also calls `out.WriteRDP(fp)`. To pass the typed
`RDPFingerprint` through the `common.Result` channel:

- `common.Result.Extra` (currently `string`, **unused in the
  entire codebase** per `grep -rE "\.Extra\s*="`) is **repurposed**
  to type `any`. Zero migration cost because no consumer exists.
- The RDP plugin sets `r.Extra = &fp` (pointer, so consumers
  type-assert cheaply).
- `core/pipeline.go` line 110, after `sess.UI.Event(r)`, type-asserts:
  ```go
  if rdp, ok := r.Extra.(*common.RDPFingerprint); ok {
      sess.Output.WriteRDP(*rdp)  // writes rdp.json + rdp.txt
  }
  ```
  Other plugins keep using `Extra` for free-form `string` (the
  field still serializes as JSON `extra`); they just lose the
  compile-time guarantee that it's a string.

Alternative considered and rejected: passing `out *common.Output`
into `Identify()`. Rejected because the Plugin interface contract
already has `Identify(ctx, host, port) *common.Result`; broadening
the signature breaks the 13 existing plugins. The repurposed `Extra`
field keeps the contract stable.

### Tests (`rdp_test.go`)

Use a `net.Listener` running on `127.0.0.1:0`. Cases:

| Case | Server fakes | Expect |
|---|---|---|
| Happy path HYBRID | CC with PROTOCOL_HYBRID + Connect-Response with `clientName="WIN-SRV-01"`, `clientBuild=19041`, `version=0x00080004` | `fp.ServerName == "WIN-SRV-01"`, `fp.OSBuild == "19041"`, `fp.NLASupported == true`, `fp.OSVersion == "Windows 10 / Server 2019 (0x00080004)"` |
| Plain RDP (no NLA) | CC with PROTOCOL_RDP, then full Connect-Response | `fp.NLASupported == false` |
| Timeout | Server accepts but never writes | `Identify` returns nil within 3 s |
| Connection refused | No server | `Identify` returns nil within dial timeout |
| PROTOCOL_SSL | CC with PROTOCOL_SSL | `Identify` returns nil (v0.2+ scope) |

Server fake is a single goroutine per test: `conn.SetDeadline`,
read CR, write CC, read Connect-Initial, write Connect-Response,
close.

## Design — Section 5: Output, Files, Tests, Docs, Migration

### File-level diff

```
go.mod                                                              (lib/pq added)
go.sum                                                              (lib/pq hashes)

core/cred/protocols/postgresql.go          (new,  ~110 LoC)
core/cred/protocols/postgresql_test.go     (new,  ~150 LoC)
core/cred/protocols/elasticsearch.go       (new,  ~100 LoC)
core/cred/protocols/elasticsearch_test.go  (new,  ~130 LoC)
core/cred/protocols/doc.go                 (modify,  +2 lines for new authenticators)

plugins/adapted/rdp/rdp.go                 (new,  ~250 LoC)
plugins/adapted/rdp/wire.go                (new,  ~700 LoC)
plugins/adapted/rdp/rdp_test.go            (new,  ~250 LoC)
plugins/adapted/postgresql/postgresql.go   (modify,  Modes() returns Identify|Credential)
plugins/adapted/elasticsearch/elasticsearch.go (modify,  Modes() returns Identify|Credential)
plugins/adapted/doc.go                     (modify,  +1 line: blank import rdp)

common/result.go                                  (modify,  Extra string → Extra any)
core/pipeline.go                                  (modify,  +WriteRDP routing after Event)

README.md                                   (modify,  凭据 8→10, 插件表 +rdp row, drop "v0.2" RDP caveat)
```

### Tests + acceptance

- `go vet ./...` clean.
- `go test ./...` green, including the 5 new test files.
- Manual smoke (recorded in a test/ run): `fg-qimen -H 127.0.0.1 -p 5432` →
  rdp.json entry appears for an open 3389.

### Migration & rollback

- No bbolt schema change (no new key spaces, no version bump).
- No CLI flag change.
- No data migration.
- Rollback = `git revert` the commit. No forward-only data dependencies.

### Risk register

| Risk | Mitigation |
|---|---|
| RDP wire protocol implementation may have hidden edge cases (malformed TPKT, BER decoding corner cases) | `rdp_test.go` covers 5 cases; integration test in `test/` with a real Windows VM is out of scope for v0.1 (FG-QiMen development happens on Linux/macOS) |
| `lib/pq` adds ~30 LoC to binary and ~1 MB extra size | Justfile build already runs with `-ldflags="-s -w"`; final binary stays well under the v0.1 budget |
| `common.Result.Extra` field could be misused by other plugins | Document the field as "set by plugins that need a side-channel write; consumers must type-assert and ignore unknown types" |
| PROTOCOL_HYBRID on older servers (pre-Win7) may not return the fields we want | Tests cover the legacy PROTOCOL_RDP fallback; `serverCore` is still sent in that path |

## Acceptance criteria (definition of done for this slice)

- [ ] All 5 new test files pass.
- [ ] `go vet ./...` and `go test ./...` are green.
- [ ] README reflects 14 plugins, 10 authenticators, RDP deep fingerprint
      delivered.
- [ ] Codebase contains zero `fscan` / `shadow1ng` references in
      `.go` files (obs #945).
- [ ] No empty-cred "vuln" path exists in the new code
      (manual grep for `unauth` / `unauthorized` returns no hits
      in the new files).
- [ ] No post-auth query string (`SELECT`, `SHOW`, `version()`,
      `GET _cluster`) appears in the new code.
- [ ] RDP `Identify` does not call `Attach`, `Login`, or any session
      API. The handshake stops at MCS Connect-Response.
- [ ] One end-to-end smoke test (recorded in `test/README.md`) shows
      the rdp.json / rdp.txt files being populated.

## Open questions

None at draft time. The user has confirmed:
- Empty-cred-as-vuln is excluded (NG-1, NG-2 reinforced).
- RDP cred test is out of scope (NG-3).
- RDP TLS cert extraction is out of scope (NG-4).
- No fscan code in our source (NG-6).
