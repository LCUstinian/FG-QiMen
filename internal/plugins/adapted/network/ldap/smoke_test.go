// smoke_test.go — A.6 coverage baseline for the ldap plugin.
// smoke_test.go — ldap 插件的 A.6 覆盖率基线。
package ldap

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
