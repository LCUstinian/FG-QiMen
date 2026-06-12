// Package main is the entry point for the fg-qimen CLI binary.
// Package main 是 fg-qimen CLI 二进制的入口。
//
// FG-QiMen is a pipeline scanner with project workspaces. It supports three
// run modes (scan / crack / linked) and two work modes (ephemeral oneshot
// vs persistent project). The architecture is documented in
// THIRD_PARTY_LICENSES.md and the in-tree README.
//
// FG-QiMen 是一个带项目工作区的管道扫描器。它支持三种运行模式（scan / crack / linked）
// 和两种工作模式（即扫即走 vs 增量扫描）。架构详见 THIRD_PARTY_LICENSES.md 和仓库内 README。
package main

import (
	"fmt"
	"os"

	"github.com/LCUstinian/FG-QiMen/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
