// Package protocols contains concrete Authenticator implementations.
// Package protocols 包含具体的 Authenticator 实现。
//
// Each file's init() calls cred.Register(self) so the protocol is
// available to the pipeline as soon as this package is imported.
// Import it from cmd/root.go via a blank import to register all
// built-in protocols at startup.
//
// 每个文件的 init() 调 cred.Register(self)，所以包被 import 后协议立刻
// 可用。在 cmd/root.go 用 blank import 启动时注册所有内置协议。
package protocols

import (
	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
)

func init() {
	// Register all built-in authenticators. / 注册所有内置 authenticator。
	cred.Register(NewSSHAuthenticator())
	cred.Register(NewFTPAuthenticator())
	cred.Register(NewMySQLAuthenticator())
	cred.Register(NewRedisAuthenticator())
	cred.Register(NewMemcachedAuthenticator())
	cred.Register(NewMongoAuthenticator())
	cred.Register(NewMSSQLAuthenticator())
	cred.Register(NewSMBAuthenticator())
}
