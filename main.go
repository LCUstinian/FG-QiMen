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

	// Embed the IANA tz database so cross-timezone scheduled
	// scans (`--tz`) work on stripped container images that
	// don't ship /usr/share/zoneinfo. Without this, a missing
	// system tz DB silently falls back to time.Local (UTC
	// offset 0 in many minimal containers) which silently
	// produces wrong cron fire times. Cost: +~450 KB binary
	// (compressed). v0.5.1 makes this default-on to remove
	// the "works on my machine, breaks in CI" surprise.
	// / 内嵌 IANA tz 数据库，让跨时区定时扫描（--tz）在精简
	// 容器镜像（没 /usr/share/zoneinfo）上也能工作。少了这个，
	// 系统 tz DB 缺失会静默回退 time.Local（很多最小容器
	// 是 UTC 偏移 0）→ cron 触发时间静默错。代价：二进制
	// +~450 KB（压缩后）。v0.5.1 改为默认开启，消除"我
	// 机器行 CI 挂"的尴尬。
	_ "time/tzdata"

	"github.com/LCUstinian/FG-QiMen/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
