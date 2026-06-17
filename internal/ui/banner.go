// banner.go — ASCII art startup banner for FG-QiMen.
// banner.go — FG-QiMen 的 ASCII 艺术启动横幅。
package ui

import (
	"fmt"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/version"
)

const (
	bannerWidth = 52
	bannerArt   = `
  ███████╗ ██████╗ ██████╗  ██████╗ ███████╗
  ██╔════╝██╔═══██╗██╔══██╗██╔═══██╗██╔════╝
  █████╗  ██║   ██║██████╔╝██║   ██║███████╗
  ██╔══╝  ██║   ██║██╔══██╗██║   ██║╚════██║
  ██║     ██████╔╝██║  ██║╚██████╔╝███████║
  ╚═╝      ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝
`
)

// RenderBanner returns the full startup banner as a string.
// When colored is true, ANSI color codes are applied.
// RenderBanner 返回完整的启动横幅字符串。
// colored 为 true 时应用 ANSI 颜色码。
func RenderBanner(colored bool) string {
	var sb strings.Builder

	if colored {
		cyan := "\033[36m"
		reset := "\033[0m"
		sb.WriteString(cyan + bannerArt + reset)
	} else {
		sb.WriteString(bannerArt)
	}

	line := fmt.Sprintf("  v%s  |  Network Reconnaissance & Credential Scanner", version.Value)
	if colored {
		sb.WriteString("\033[90m" + line + "\033[0m\n")
	} else {
		sb.WriteString(line + "\n")
	}

	sb.WriteString(strings.Repeat("─", bannerWidth) + "\n")

	return sb.String()
}
