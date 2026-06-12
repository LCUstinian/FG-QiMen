// ProbeLogWriter / helpers used by types.go.
// ProbeLogWriter / types.go 用到的小工具。
package portfinger

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// probeLogWriter returns the package's stderr writer. Lazy-init so
// tests can swap it. v0.1: we always use os.Stderr.
//
// probeLogWriter 返回包的 stderr writer。懒初始化，让测试能替换。
// v0.1：始终用 os.Stderr。
var (
	logMu    sync.Mutex
	logOut   io.Writer = os.Stderr
)

func probeLogWriter() io.Writer {
	logMu.Lock()
	defer logMu.Unlock()
	return logOut
}

// SetLogWriter replaces the log sink (used by tests; package-internal).
// SetLogWriter 替换日志 sink（测试用；包内）。
func SetLogWriter(w io.Writer) {
	logMu.Lock()
	defer logMu.Unlock()
	logOut = w
}

// keep the fmt import used (for probe parser error messages).
// fmt 保留 import 以供 probe 解析器的错误消息使用。
var _ = fmt.Sprintf
