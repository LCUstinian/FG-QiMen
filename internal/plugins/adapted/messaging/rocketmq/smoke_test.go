// smoke_test.go — A.6 coverage baseline for the rocketmq plugin.
// smoke_test.go — rocketmq 插件的 A.6 覆盖率基线。
package rocketmq

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	plugintest.Smoke(t, New())
}
