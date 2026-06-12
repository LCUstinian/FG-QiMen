// resume subcommand / resume 子命令
//
// Resume a previously interrupted or partially-completed project scan by
// loading its bbolt seen-set. Forces -resume=true and requires -p <name>.
//
// 通过加载 bbolt seen-set 续跑之前中断或未完成的项目扫描。强制启用 -resume 并
// 要求指定 -p <name>。
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a project scan from saved bbolt state",
	Long: `Resume a project scan by loading its bbolt seen-set. Requires
-p <name> to identify the project. Equivalent to:

  fg-qimen -p <name> -h ... -resume`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagProject == "" {
			return fmt.Errorf("resume requires -p <project> to identify the project workspace")
		}
		// Force resume mode regardless of caller-supplied -resume value.
		// 无论调用方是否传了 -resume，都强制开启。
		flagResume = true
		// Reuse the same scan logic.
		// 复用同一扫描逻辑。
		return runScan(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}
