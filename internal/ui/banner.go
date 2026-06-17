// banner.go — Startup banner for FG-QiMen.
// banner.go — FG-QiMen 的启动横幅。
package ui

import (
	"fmt"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/version"
)

const bannerWidth = 58

// RenderBanner returns the startup banner.
// When colored is true, ANSI color codes are applied.
// RenderBanner 返回启动横幅。
// colored 为 true 时应用 ANSI 颜色码。
func RenderBanner(colored bool) string {
	var sb strings.Builder

	top := "╔" + strings.Repeat("═", bannerWidth) + "╗"
	bot := "╚" + strings.Repeat("═", bannerWidth) + "╝"
	side := "║"

	title := fmt.Sprintf("  FG-QiMen v%s", version.Value)
	subtitle := "  Network Reconnaissance & Credential Scanner"

	// Center the text
	titlePad := (bannerWidth - len(title)) / 2
	subPad := (bannerWidth - len(subtitle)) / 2

	titleLine := side + strings.Repeat(" ", titlePad) + title + strings.Repeat(" ", bannerWidth-titlePad-len(title)) + side
	subLine := side + strings.Repeat(" ", subPad) + subtitle + strings.Repeat(" ", bannerWidth-subPad-len(subtitle)) + side
	emptyLine := side + strings.Repeat(" ", bannerWidth) + side

	if colored {
		cyan := "\033[36m"
		dim := "\033[90m"
		reset := "\033[0m"
		sb.WriteString(cyan + top + "\n" + reset)
		sb.WriteString(emptyLine + "\n")
		sb.WriteString(cyan + titleLine + "\n" + reset)
		sb.WriteString(dim + subLine + "\n" + reset)
		sb.WriteString(emptyLine + "\n")
		sb.WriteString(cyan + bot + "\n" + reset)
	} else {
		sb.WriteString(top + "\n")
		sb.WriteString(emptyLine + "\n")
		sb.WriteString(titleLine + "\n")
		sb.WriteString(subLine + "\n")
		sb.WriteString(emptyLine + "\n")
		sb.WriteString(bot + "\n")
	}

	return sb.String()
}
