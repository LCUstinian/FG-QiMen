// Package credential implements credential (brute-force) testing.
//
// Architecture:
//   - Authenticator : the per-protocol authentication engine
//   - scheduler     : per-target throttling + first-match short-circuit
//   - auth/         : concrete Authenticator implementations, grouped by
//     domain: database / remote / messaging / filestorage / email / network
//
// Each concrete auth package (auth/database, auth/remote, etc.) has
// its own init() that calls credential.Register(self). To register
// all built-in authenticators, blank-import every category from
// cmd/root.go (or from a single meta-package that re-exports them).
//
// HARD RULE: implementations must NOT open sessions, execute commands,
// or take any other post-authentication action. The Hit is the only
// side effect. / 硬性原则：实现严禁打开 session、执行命令或任何
// 认证后动作。Hit 是唯一的副作用。
//
// 包 credential 实现凭据（爆破）测试。
// 架构：Authenticator 接口、scheduler 调度、auth/ 下按域分组的
// 具体实现（database / remote / messaging / filestorage / email /
// network）。每个 auth 子包 init() 调 credential.Register(self)。
package credential
