// version subcommand / version 子命令
package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/version"
)

// Version is re-exported for backward compatibility (older scripts
// link against cmd.Version via -ldflags). The source of truth is
// internal/version.Value; new code should import that directly.
//
// Version 重新导出以兼容旧版 -ldflags 注入脚本。真实值以
// internal/version.Value 为准，新代码应直接导入。
//
// Deprecated: use internal/version.Value instead.
var Version = version.Value

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and build information",
	Long:  "Print the FG-QiMen version, Go runtime version, and OS/arch.",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "fg-qimen %s\n", Version)
		fmt.Fprintf(out, "go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
