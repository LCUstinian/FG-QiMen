// smoke_test.go — A.6 coverage baseline for the snmpv3 plugin.
// smoke_test.go — snmpv3 插件的 A.6 覆盖率基线。
package snmpv3

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
