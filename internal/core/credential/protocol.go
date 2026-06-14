// Package credential: per-protocol authenticator interface + result types.
// Package credential: 按协议 authenticator 接口 + 结果类型。
package credential

import (
	"context"
	"sync"
	"time"
)

// Authenticator is the per-protocol authentication engine.
// Authenticator 是按协议的认证引擎。
//
// Implementations live under auth/{database,email,filestorage,messaging,network,remote}/
// (one package per protocol, self-registering via init()).
//
// 实现位于 auth/{database,email,filestorage,messaging,network,remote}/
// （每协议一个包，通过 init() 自动注册）。
//
// HARD RULE: implementations MUST NOT open sessions, execute commands,
// or take any other post-auth action. The Hit is the only side effect.
//
// 硬性原则：实现严禁打开 session、执行命令或任何认证后动作。
// Hit 是唯一的副作用。
type Authenticator interface {
	// Name returns the service identifier ("ssh", "ftp", "mysql").
	// Name 返回服务标识。
	Name() string

	// DefaultPorts returns the ports this authenticator typically
	// runs against. The Scheduler uses this to know which ports to
	// try when given a host without a specific port. / DefaultPorts
	// 返回该 authenticator 通常跑的端口。Scheduler 在给 host 但没指定
	// 端口时用这个。
	DefaultPorts() []int

	// Authenticate tries each cred in order against host:port. Returns
	// the first successful Hit, or nil if all failed. ctx is honored;
	// per-attempt timeout is taken from `timeout`. / Authenticate 按
	// 顺序用 host:port 尝试每个 cred。首个成功返回 Hit；全部失败返回 nil。
	// 遵循 ctx；单次尝试超时从 `timeout` 取。
	Authenticate(ctx context.Context, host string, port int, creds []Cred, timeout time.Duration) (*Hit, error)
}

// registry maps service name → Authenticator. Populated by each
// auth/<cat>/<protocol> package's init() (which calls Register on import).
//
// registry 映射 service 名 → Authenticator。由 auth/<cat>/<protocol> 包
// 的 init() 在 import 时调 Register 填充。
var (
	regMu sync.RWMutex
	reg   = map[string]Authenticator{}
)

// Register adds a protocol's authenticator to the registry. Safe to
// call from init(). / Register 把协议的 authenticator 加进注册表。
// 可在 init() 中调用。
func Register(auth Authenticator) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[auth.Name()]; dup {
		panic("credential: duplicate authenticator registration for " + auth.Name())
	}
	reg[auth.Name()] = auth
}

// LookupAuthenticator returns the Authenticator registered for `name`,
// or nil + false if unknown. / LookupAuthenticator 返回 `name` 对应的
// Authenticator；未知返回 nil + false。
func LookupAuthenticator(name string) (Authenticator, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := reg[name]
	return a, ok
}
