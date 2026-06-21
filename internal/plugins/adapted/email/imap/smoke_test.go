// smoke_test.go — A.6 coverage baseline for the imap plugin.
// smoke_test.go — imap 插件的 A.6 覆盖率基线。
package imap

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
