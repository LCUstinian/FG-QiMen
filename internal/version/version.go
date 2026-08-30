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

// Value is the FG-QiMen semantic version. Overridable at build time
// via -ldflags "-X github.com/LCUstinian/FG-QiMen/internal/version.Value=vX.Y.Z".
//
// Note: this MUST be a `var` (not `const`) for the -X linker flag
// to take effect. Constants are baked into the caller's code at
// compile time and cannot be patched at link time. The original
// declaration was `const Value = "0.2.0"` and the v0.3.1 release
// candidates built fine (the binary just always reported
// "fg-qimen 0.2.0" regardless of the ldflag), which is why the
// issue wasn't caught by the lint pass — go's compile-time
// inlining hid the broken wiring. The release.yml smoke-test
// step (which greps the version output for the tag name) is
// what surfaced the silent failure.
//
// Value 是 FG-QiMen 的语义版本号，可通过 -ldflags 在构建时覆盖。
// 注：必须用 `var`（不能用 `const`）才能让 -X linker 标志生效。
// 常量在编译时被烤进调用者代码，link 时无法修补。原声明是
// `const Value = "0.2.0"`，v0.3.1 release 候选构建时不会报错
// （binary 永远报 "fg-qimen 0.2.0"，与 ldflag 无关），所以
// 这个 bug 在 lint 阶段没被抓住——Go 的编译期内联掩盖了断
// 开的接线。release.yml 的 smoke-test 步骤（用 tag 名 grep
// version 输出）才暴露了这个静默失败。
var Value = "0.2.0"
