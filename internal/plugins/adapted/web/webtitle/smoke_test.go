// smoke_test.go — A.6 coverage baseline for the webtitle plugin.
// smoke_test.go — webtitle 插件的 A.6 覆盖率基线。
package webtitle

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/plugintest"
)

func TestSmoke(t *testing.T) {
	// The webtitle plugin's exported constructor is NewWebTitlePlugin,
	// not the convention New() that the other adapted plugins use.
	// webtitle 的导出构造是 NewWebTitlePlugin,不是其他 adapted 插件的
	// New() 约定。
	plugintest.Smoke(t, NewWebTitlePlugin())
}
