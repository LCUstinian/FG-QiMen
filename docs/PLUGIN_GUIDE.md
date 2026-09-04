# Plugin Author Guide

> [中文版本](../PLUGIN_GUIDE.zh-CN.md)

How to add a new service Identify / Credential plugin to FG-QiMen.

A plugin is a Go package under `internal/plugins/adapted/<category>/<protocol>/`
that implements the `plugins.Plugin` interface and self-registers via `init()`.

## The interface

```go
type Plugin interface {
    Name() string
    Ports() []int
    Modes() Mode                       // ModeIdentify | ModeCredential | both
    Identify(ctx, host, port) *Result  // banner / version / title
    Credential(ctx, host, port, creds []Cred) *Result  // test user:pass
}
```

`Result` is `internal/types.Result`. `Cred` is `internal/types.Cred`.

## Minimal Identify-only plugin (5 lines of real code)

```go
package myproto

import (
    "context"
    "net"
    "time"

    "github.com/LCUstinian/FG-QiMen/internal/plugins"
    "github.com/LCUstinian/FG-QiMen/internal/types"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }
func init() { plugins.Register(New()) }

func (p *Plugin) Name() string                              { return "myproto" }
func (p *Plugin) Ports() []int                              { return []int{1234} }
func (p *Plugin) Modes() plugins.Mode                       { return plugins.ModeIdentify }
func (p *Plugin) Credential(context.Context, string, int, []types.Cred) *types.Result {
    return nil
}
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
    return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
        // ... your protocol probe here ...
        return &types.Result{Host: host, Port: port, Service: "myproto", Banner: "...", Time: time.Now()}
    })
}
```

## Use the shared helpers

`plugins.RawTCPIdentify(ctx, host, port, fn)` handles the boilerplate:
TCP dial, deadline, defer-close, error → nil. Pass an `fn` that does
the protocol-specific read/write.

For custom per-call timeouts:

```go
plugins.RawTCPIdentify(ctx, host, port, fn, plugins.WithIdentifyTimeout(5*time.Second))
```

## Registering the plugin

The plugin auto-registers via `init()`. To make it actually import,
add a blank import in the category's `doc.go`:

```go
// internal/plugins/adapted/database/doc.go
package database

import (
    _ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/myproto"
)
```

For the registry guard test (`TestRegistryHasAllAuthenticators`) to
pick it up, the plugin must also implement a `credential.Authenticator`
registered via `credential.Register(...)` if it advertises
`ModeCredential` (otherwise the auth path is wired through the
central `credential.Scheduler`, which expects a registered name).

## Hard rules

- **No post-auth action.** `Credential()` MUST return a
  `*Result` with `Cred` set on hit and nothing else. No session,
  no exec, no shell, no file write.
- **No CVE-based exploitation.** No EternalBlue, no deserialization
  RCE, no auth bypass.
- **No file I/O outside the framework.** Plugins return `*Result`;
  the pipeline handles persistence.

## Adding a credential authenticator

`Credential()` in the plugin is a no-op stub (`return nil`) because
the central `credential.Scheduler` owns the spraying loop.
To add a real authenticator, create a sibling package under
`internal/core/credential/auth/<category>/<protocol>/` that
implements:

```go
type Authenticator interface {
    Name() string
    DefaultPorts() []int
    Authenticate(ctx, host, port, creds []Cred, timeout) (*Hit, error)
}
```

Self-register via `credential.Register(NewXxxAuthenticator())` in
`init()`. The registry test at `internal/core/credential/cred_test.go`
pins the count and name list — add your new name there.

## Tests

- For pure-TCP plugins, mirror the memcached / redis test pattern:
  start an in-process fake server on `127.0.0.1:0` and exercise
  NoAuth / Hit / MissAll / NotXxx cases.
- For UDP plugins (SNMP), the test process IS the fake server.
- For binary protocols, use the same pattern; the auth flow is
  exercised through `Authenticate` directly.

## Lifecycle

- `init()` runs once at process start; `Register` is guarded by
  `panic on duplicate` — if you see "duplicate authenticator
  registration", one of your imports has a name collision.
- `Identify` and `Credential` are called per-host:port by the
  pipeline. They MUST be safe for concurrent use (the pipeline
  uses 200+ workers).
