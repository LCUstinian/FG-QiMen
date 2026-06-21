// smoke_test.go — A.6 coverage baseline for the winrm plugin.
// smoke_test.go — winrm 插件的 A.6 覆盖率基线。
package winrm

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
