// Package version exposes the FG-QiMen semantic version, set at build
// time via -ldflags.
//
//	-ldflags "-X github.com/LCUstinian/FG-QiMen/internal/version.Value=0.2.0".
//
// The value is also a sensible in-source default so `go run` from a
// fresh checkout reports a usable version.
//
// Package version 暴露 FG-QiMen 的语义版本号，可在构建时通过 -ldflags
// 注入。源代码内的默认值保证 `go run` 时也能得到可读版本。
package version

// Value is the FG-QiMen semantic version. Overridable at build time.
// Value 是 FG-QiMen 的语义版本号，构建时可覆盖。
const Value = "0.2.0"
