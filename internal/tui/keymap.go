// keymap.go — key bindings for the v0.7.0 TUI additions. Existing
// keys (q, p, arrows) live in the main Model.Update handler; this
// file holds the new spec B+C keys. / keymap.go — v0.7.0 TUI 新增
// 键的绑定。已有键（q、p、箭头）在主 Model.Update handler 中；
// 本文件保存新的 Spec B+C 键。
package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap holds the v0.7.0 key bindings. Construct with DefaultKeymap().
// / Keymap 持有 v0.7.0 键绑定。用 DefaultKeymap() 构造。
type Keymap struct {
	ToggleErrors key.Binding
	ClearErrors  key.Binding
	LiveOverlay  key.Binding // narrow mode only
	Help         key.Binding
}

// DefaultKeymap returns the standard key bindings.
// / DefaultKeymap 返回标准键绑定。
func DefaultKeymap() Keymap {
	return Keymap{
		ToggleErrors: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "errors panel")),
		ClearErrors: key.NewBinding(
			key.WithKeys("E"),
			key.WithHelp("E", "clear errors")),
		LiveOverlay: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "live overlay")),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help")),
	}
}
