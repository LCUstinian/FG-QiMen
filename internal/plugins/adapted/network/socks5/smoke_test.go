// smoke_test.go — A.6 coverage baseline for the socks5 plugin.
// smoke_test.go — socks5 插件的 A.6 覆盖率基线。
package socks5

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
