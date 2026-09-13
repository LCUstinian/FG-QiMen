# 插件编写指南

> [English](PLUGIN_GUIDE.md)

如何为 FG-QiMen 新增一个服务 Identify / Credential 插件。

插件是位于 `internal/plugins/adapted/<category>/<protocol>/` 的 Go 包，
实现 `plugins.Plugin` 接口并通过 `init()` 自注册。

## 接口

```go
type Plugin interface {
    Name() string
    Ports() []int
    Modes() Mode                       // ModeIdentify | ModeCredential | 两者
    Identify(ctx, host, port) *Result  // banner / 版本 / 标题
    Credential(ctx, host, port, creds []Cred) *Result  // 测试 user:pass
}
```

`Result` 是 `internal/types.Result`。`Cred` 是 `internal/types.Cred`。

## 最小 Identify-only 插件（5 行真实代码）

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
        // ... 你的协议探测逻辑 ...
        return &types.Result{Host: host, Port: port, Service: "myproto", Banner: "...", Time: time.Now()}
    })
}
```

## 使用共享助手

`plugins.RawTCPIdentify(ctx, host, port, fn)` 处理样板：TCP dial、
deadline、defer-close、错误 → nil。传入做协议特定读写的 `fn` 即可。

需要自定义单次超时：

```go
plugins.RawTCPIdentify(ctx, host, port, fn, plugins.WithIdentifyTimeout(5*time.Second))
```

## 注册插件

插件经 `init()` 自动注册。要让它真正被 import，在所属 category 的
`doc.go` 加 blank import：

```go
// internal/plugins/adapted/database/doc.go
package database

import (
    _ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/myproto"
)
```

聚合守卫测试（`internal/plugins/adapted/aggregation_test.go`）会验证：
任何插件包若漏出聚合 import 链，CI 直接红——新增插件目录无需改守卫。

如果插件广告 `ModeCredential`，还必须实现 `credential.Authenticator`
并经 `credential.Register(...)` 注册（否则 registry 守卫测试
`TestRegistryHasAllAuthenticators` 抓不到；认证路径由中央
`credential.Scheduler` 按注册名接线）。

## HARD 规则

- **无认证后动作。** `Credential()` 命中时必须只返回带 `Cred` 的
  `*Result`，别无其他。无 session、无 exec、无 shell、无文件写入。
- **无 CVE 利用。** 无 EternalBlue、无反序列化 RCE、无认证绕过。
- **框架外无文件 I/O。** 插件只返回 `*Result`；持久化由管线处理。

## 新增凭据 authenticator

插件里的 `Credential()` 是 no-op stub（`return nil`），因为喷洒循环
由中央 `credential.Scheduler` 负责。要加真正的 authenticator，在
`internal/core/credential/auth/<category>/<protocol>/` 下创建兄弟包，
实现：

```go
type Authenticator interface {
    Name() string
    DefaultPorts() []int
    Authenticate(ctx, host, port, creds []Cred, timeout) (*Hit, error)
}
```

在 `init()` 里经 `credential.Register(NewXxxAuthenticator())` 自注册。
`internal/core/credential/cred_test.go` 的 registry 测试钉死了数量与
名单——把新名字加进去。

## 测试

- 纯 TCP 插件仿照 memcached / redis 的测试模式：在
  `127.0.0.1:0` 起进程内 fake server，覆盖 NoAuth / Hit / MissAll /
  NotXxx 用例。
- UDP 插件（SNMP）：测试进程本身就是 fake server。
- 二进制协议用同一模式；auth 流程直接经 `Authenticate` 练习。

## 生命周期

- `init()` 在进程启动时跑一次；`Register` 对重复注册 panic——看到
  "duplicate authenticator registration" 说明某个 import 有名字冲突。
- `Identify` 与 `Credential` 被管线按 host:port 逐个调用。必须并发
  安全（管线用 200+ worker）。
