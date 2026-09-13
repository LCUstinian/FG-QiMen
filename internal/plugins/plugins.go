// Package plugins defines the unified Plugin interface and a small
// registry. Plugins live under plugins/adapted/ and register themselves
// via init().
//
// Package plugins 定义统一的 Plugin 接口和小型注册表。插件位于
// plugins/adapted/，通过 init() 自动注册。
//
// Hard rule (v0.1+): Credential() implementations MUST NOT call any
// post-authentication API (e.g. ssh.NewSession / Exec). On a hit they
// return a *Result with the Cred field set; the pipeline writes to
// creds.txt and nothing else.
//
// 硬性原则（v0.1+）：Credential() 实现严禁调用任何认证后 API
// （如 ssh.NewSession / Exec）。命中时返回带 Cred 字段的 *Result；
// 管线只写入 creds.txt，不做任何其他动作。
//
// Enumeration boundary (v0.9): a plugin MAY additionally implement the
// optional Enumerator interface for READ-ONLY, EnumLimits-bounded
// evidence collection (share listings, directory walks). Enumerator is
// the ONLY sanctioned post-auth surface; it must never read file
// contents, write, delete or execute. Credential() keeps its
// authentication-only contract unchanged — the two are dispatched from
// separate call sites in core and never mixed.
//
// 枚举边界（v0.9）：插件可以额外实现可选的 Enumerator 接口做只读、
// 受 EnumLimits 限制的取证（共享列表、目录遍历）。Enumerator 是唯一
// 被认可的认证后动作面；绝不读取文件内容、写入、删除或执行。
// Credential() 的"仅认证"契约保持不变——两者在 core 中由不同调用点
// 分发，永不混合。
package plugins

import (
	"context"
	"sort"
	"sync"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Mode is a bitfield of plugin capabilities.
// Mode 是插件能力的位标志。
type Mode int

const (
	// ModeIdentify indicates the plugin can identify / fingerprint
	// the service on its default ports.
	// ModeIdentify 表示插件可在其默认端口上识别 / 指纹化服务。
	ModeIdentify Mode = 1 << iota
	// ModeCredential indicates the plugin can test user:pass credentials
	// (authenticate only — no post-auth actions).
	// ModeCredential 表示插件可测试 user:pass 凭据（仅认证——不做认证后动作）。
	ModeCredential
)

// Plugin is the unified interface every scanner plugin must satisfy.
// Plugin 是每个扫描器插件必须实现的统一接口。
type Plugin interface {
	// Name returns a short identifier used in result/JSON output
	// (e.g. "ssh", "http", "mssql").
	// Name 返回用于 result/JSON 输出的短标识符（如 "ssh"、"http"、"mssql"）。
	Name() string
	// Ports returns the default ports this plugin should be invoked for.
	// Ports 返回应调用此插件的默认端口。
	Ports() []int
	// Modes returns the bitfield of capabilities (Identify, Credential, or both).
	// Modes 返回能力位标志（Identify、Credential 或两者）。
	Modes() Mode
	// Identify performs passive / active service identification. Returns
	// nil if the service could not be identified.
	// Identify 执行被动/主动服务识别。无法识别时返回 nil。
	Identify(ctx context.Context, host string, port int) *types.Result
	// Credential tests user:pass authentication. On hit returns a *Result
	// with Cred set; on miss returns nil.
	//
	// 凭据测试契约：测试某服务的用户名+密码认证。
	// 命中正确凭据后，仅返回 *Result（含 Cred 字段），
	// 不调用任何认证后 API（如 ssh.NewSession / Exec / Shell），
	// 不执行任何远程命令。
	//
	// Credential 接收的 creds 列表由 core/pipeline.go 构造（笛卡尔积
	// users × passes）；插件负责按服务的并发安全方式逐个测试。
	Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result
}

// Enumerator is an OPTIONAL capability interface: a Plugin that can
// collect read-only, bounded evidence (SMB share listings, FTP directory
// walks) implements it in addition to Plugin. Dispatch is flag-gated in
// core (--share-enum / --ftp-enum) and strictly separated from
// Credential — see the package enumeration-boundary note.
//
// The returned *Result carries the structured payload in Extra (e.g.
// *types.ShareEnumResult / *types.FTPEnumResult) for the dedicated
// evidence sinks. Enumerate returns nil when access is denied — negative
// evidence produces no record, keeping the evidence files focused on
// actual findings.
//
// Enumerator 是可选能力接口：能做只读、有界取证的 Plugin（SMB 共享列
// 表、FTP 目录遍历）在 Plugin 之外额外实现它。分发在 core 中由 flag
// 把关（--share-enum / --ftp-enum），与 Credential 严格分离——见包注
// 释的枚举边界说明。
//
// 返回的 *Result 把结构化载荷放进 Extra（如
// *types.ShareEnumResult / *types.FTPEnumResult）交给专用证据 sink。
// 访问被拒时 Enumerate 返回 nil——负面证据不产生记录，让证据文件
// 聚焦真实发现。
type Enumerator interface {
	// Enumerate performs ONE bounded, read-only evidence walk against
	// host:port as the given user (empty user = unauthenticated probe).
	// limits caps depth / total entries / wall clock; implementations
	// must honor ctx and never read file contents or mutate anything.
	//
	// Enumerate 对 host:port 以给定用户执行一次有界、只读取证遍历
	//（空 user = 未认证探测）。limits 限定深度 / 总条目 / 墙钟时间；
	// 实现必须遵守 ctx 且绝不读取文件内容、不做任何修改。
	Enumerate(ctx context.Context, host string, port int, user, pass string, limits types.EnumLimits) *types.Result
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Plugin{}
)

// Register adds a plugin to the global registry. Safe to call from init().
// Register 把插件加入全局注册表。可在 init() 中调用。
func Register(p Plugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[p.Name()]; dup {
		// Duplicate is a programming error — surface it via panic so
		// init() failures are obvious.
		// 重复是编程错误——通过 panic 让 init() 失败显而易见。
		panic("plugins: duplicate registration for " + p.Name())
	}
	registry[p.Name()] = p
}

// All returns a snapshot of all registered plugins, sorted by name for
// stable output.
// All 返回所有已注册插件的快照，按名称排序以保持输出稳定。
func All() []Plugin {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Plugin, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Get looks up a plugin by name. Returns nil if not found.
// Get 按名查找插件。未找到返回 nil。
func Get(name string) Plugin {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}
