// version subcommand / version 子命令
package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is the FG-QiMen semantic version, set at build time via
//
//	-ldflags "-X github.com/LCUstinian/FG-QiMen/cmd.Version=0.1.0".
//
// Version 是 FG-QiMen 的语义版本号，可通过 -ldflags 注入。
var Version = "0.2.0"

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
