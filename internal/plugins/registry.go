// registry.go — explicit registration entry point.
// registry.go — 显式注册入口。
//
// To avoid an import cycle (plugins/adapted → plugins for Register, and
// plugins/ → plugins/adapted via blank import), the wiring is done
// explicitly from cmd/. This file documents the convention.
//
// 为避免导入循环（plugins/adapted → plugins 调 Register，而 plugins/ →
// plugins/adapted 通过空导入），由 cmd/ 显式装配。本文件记录约定。
//
// Usage from cmd/root.go:
//
//	import (
//	    _ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted"
//	)
//
// The blank import triggers init() in adapted/, which calls
// plugins.Register for each plugin.
package plugins
