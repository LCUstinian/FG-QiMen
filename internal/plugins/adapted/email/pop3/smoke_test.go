// smoke_test.go — A.6 coverage baseline for the pop3 plugin.
// smoke_test.go — pop3 插件的 A.6 覆盖率基线。
package pop3

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
