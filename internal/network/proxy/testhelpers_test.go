// testhelpers_test.go — small context-with-deadline helper used by
// the proxy tests. Kept in a separate file so adding helper
// utilities to the production package doesn't get tangled with the
// test file's test-specific code.
//
// testhelpers_test.go — proxy 测试用的小工具：带超时的 context。
// 单独成文件是为了让加生产包辅助函数不被测试特定代码纠缠。
package proxy

import (
	"context"
	"testing"
	"time"
)

// testCtx returns a context with the test's deadline. t.Deadline()
// is honored when present (so `go test -timeout` works), and a
// 30s fallback is used otherwise — long enough to not interfere
// with normal tests, short enough that a hung goroutine is
// caught by `go test -timeout` rather than waiting forever.
//
// testCtx 返回带测试 deadline 的 context。t.Deadline() 在时使用
// （这样 go test -timeout 有效），否则用 30s 后备——够长不干扰
// 普通测试，够短让挂死 goroutine 被 go test -timeout 抓住而非
// 永远等。
func testCtx(t *testing.T) context.Context {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	return ctx
}
