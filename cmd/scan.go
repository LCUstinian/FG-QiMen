// scan subcommand / scan 子命令
//
// Explicit alias for the default root behavior. Useful in scripts and
// documentation: `fg-qimen scan -h ...` is clearer than relying on the
// implicit default action of the root command.
//
// 显式调用根命令的默认行为。在脚本和文档里更直观：
// `fg-qimen scan -h ...` 比依赖根命令的隐式默认行为更清晰。
package cmd

import "github.com/spf13/cobra"

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run a scan (default action of fg-qimen)",
	Long: `Run a scan. By default this is ephemeral (oneshot) mode, writing
results to ./result.txt and ./result.json in the current directory.
Pass -p <name> to switch into persistent project mode.`,
	// Reuse the root RunE so flags and behavior are identical.
	// 复用根 RunE，flags 和行为完全一致。
	RunE: runScan,
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
