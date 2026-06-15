// core/pipeline.go — pure helpers and the package-level orchestration
// of the per-stage glue. The goroutine-heavy pieces (plugin
// dispatch, result sink, stats ticker) live in their own files
// (pipeline_workers.go, pipeline_sink.go) to keep this file
// readable.
//
// core/pipeline.go — 纯 helper 和各阶段装配的包级编排。goroutine
// 密集部分（plugin 分发、结果汇、stats 滴答）分到各自文件
// （pipeline_workers.go、pipeline_sink.go）让本文件保持易读。
//
// Pipeline data flow (orchestrated from cmd/scan.go → scanner.go):
//
//   port scan items → pipeline_workers.runPluginWorker
//                          ↓
//                      plugin Identify + dispatchCred
//                          ↓
//                      pipeline_sink.runResultSink
//                          ↓
//                      Output + bbolt + UI
//
// 管线数据流（从 cmd/scan.go → scanner.go 编排）：
//
//   port scan items → pipeline_workers.runPluginWorker
//                          ↓
//                      plugin Identify + dispatchCred
//                          ↓
//                      pipeline_sink.runResultSink
//                          ↓
//                      Output + bbolt + UI
package core

import (
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// loadCreds builds the []types.Cred from cfg (inline users/passes).
// loadCreds 从 cfg（内联 users/passes）构造 []types.Cred。
func loadCreds(sess *session.Session) []types.Cred {
	users := sess.Config.Users
	passes := sess.Config.Passes
	if len(users) == 0 || len(passes) == 0 {
		return nil
	}
	out := make([]types.Cred, 0, len(users)*len(passes))
	for _, u := range users {
		for _, p := range passes {
			out = append(out, types.Cred{User: u, Pass: p, AuthType: string(credential.AuthPassword)})
		}
	}
	return out
}

// selectPlugins returns the subset of all that match the
// comma-separated cfg.Plugins allow-list. Empty allow-list returns
// all (the documented "no filter" behaviour). Unknown names are
// silently dropped — the audit-fix-7 test will surface typos at
// scan start via the available-plugin log line.
//
// selectPlugins 返回 all 中匹配逗号分隔的 cfg.Plugins 白名单的子集。
// 空白名单返回全部（文档约定的"无过滤"行为）。未知名字会被静默丢弃
// ——扫描启动时通过 available-plugin 日志行让拼写错误可见。
func selectPlugins(all []plugins.Plugin, allowList string) []plugins.Plugin {
	if allowList == "" {
		return all
	}
	allowed := make(map[string]struct{})
	for _, name := range strings.Split(allowList, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	out := make([]plugins.Plugin, 0, len(allowed))
	for _, p := range all {
		if _, ok := allowed[p.Name()]; ok {
			out = append(out, p)
		}
	}
	return out
}

// matchesPort returns true if any of ports equals p.
// matchesPort 在 ports 中任一等于 p 时返回 true。
func matchesPort(ports []int, p int) bool {
	for _, x := range ports {
		if x == p {
			return true
		}
	}
	return false
}

func nowOrZero(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// (pushStats moved to pipeline_sink.go as part of the v0.2.1 god-
// file split.)

// formatPortfinger formats the matched banner into a single line.
// formatPortfinger 把匹配结果格式化为单行。
func formatPortfinger(svc, ver, banner string) string {
	out := svc
	if ver != "" {
		// Trim leading whitespace from versionInfo (the format is
		// " p/product/ v/version/ ..."). / 去掉 versionInfo 前导空格
		// （格式是 " p/product/ v/version/ ..."）。
		out += " | " + strings.TrimSpace(ver)
	}
	if len(banner) > 80 {
		banner = banner[:80] + "..."
	}
	return out + " | banner=" + strings.TrimSpace(banner)
}
