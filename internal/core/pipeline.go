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
//	port scan items → pipeline_workers.runPluginWorker
//	                       ↓
//	                   plugin Identify + dispatchCred
//	                       ↓
//	                   pipeline_sink.runResultSink
//	                       ↓
//	                   Output + bbolt + UI
//
// 管线数据流（从 cmd/scan.go → scanner.go 编排）：
//
//	port scan items → pipeline_workers.runPluginWorker
//	                       ↓
//	                   plugin Identify + dispatchCred
//	                       ↓
//	                   pipeline_sink.runResultSink
//	                       ↓
//	                   Output + bbolt + UI
package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// loadCreds builds the []types.Cred from cfg (inline users/passes plus
// user-file / pass-file dictionaries) and surfaces loader errors. The
// audit flagged the previous implementation as silently ignoring
// UserFile / PassFile, so a CLI invocation with `-user-file u.txt
// -pass-file p.txt` produced zero auth attempts with no diagnostic.
// Now loadCreds delegates to credential.LoadInto (the same loader used
// by the standalone crack-mode path) so inline and file inputs share
// one source of truth: dedup, MaxUsers / MaxPasses / MaxCredPairs
// limits, and unreadable-file errors. A non-nil error means no creds
// were produced and RunScan must abort — starting network workers
// with zero creds would waste a port scan against every target.
//
// loadCreds 从 cfg（内联 users/passes + user-file / pass-file 字典）
// 构造 []types.Cred 并向上抛 loader 错误。审计发现旧实现静默忽略
// UserFile / PassFile，导致 CLI 用 `-user-file u.txt -pass-file p.txt`
// 时零次认证且无诊断。现在 loadCreds 委托给 credential.LoadInto（crack
// 模式独立路径用的同一 loader），让内联和文件输入共用：去重、
// MaxUsers / MaxPasses / MaxCredPairs 上限、不可读文件错误。非 nil
// error 表示未产出任何 cred，RunScan 必须中止——带零 cred 启动网络
// worker 会让端口扫描对每个目标空跑一遍。
func loadCreds(sess *session.Session) ([]types.Cred, error) {
	cfg := sess.Config
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	pool := credential.NewPool()
	added, err := credential.LoadInto(pool, credential.LoadOptions{
		Users:    cfg.Users,
		Passes:   cfg.Passes,
		UserFile: cfg.UserFile,
		PassFile: cfg.PassFile,
	})
	if err != nil {
		return nil, err
	}
	if added == 0 {
		return nil, nil
	}
	src := pool.All()
	out := make([]types.Cred, len(src))
	for i, c := range src {
		out[i] = types.Cred{User: c.User, Pass: c.Pass, AuthType: string(c.Method)}
	}
	return out, nil
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

// buildPortIndex creates a map from port number to plugins that handle it.
// This enables O(1) lookup instead of O(n) linear search for each item.
//
// buildPortIndex 创建端口号到处理该端口的插件的映射。
// 这使得每个 item 的查找从 O(n) 线性搜索优化为 O(1)。
func buildPortIndex(pluginList []plugins.Plugin) map[int][]plugins.Plugin {
	index := make(map[int][]plugins.Plugin)
	for _, p := range pluginList {
		for _, port := range p.Ports() {
			index[port] = append(index[port], p)
		}
	}
	return index
}

func nowOrZero(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// wantIdentify reports whether the current mode runs the Identify
// stage for plugins. Scan and Linked both run Identify; Crack does
// not (Crack skips port scan + Identify and goes straight to
// Credential). / wantIdentify 报告当前模式是否对插件跑 Identify 阶
// 段。Scan 和 Linked 都跑；Crack 不跑（直接进 Credential）。
//
// v0.4: extracted from the previous ModeScan || ModeLinked literal in
// pipeline_workers.go so the policy lives in one place and the
// runner is mode-agnostic. / v0.4：从 pipeline_workers.go 的
// ModeScan || ModeLinked 字面量抽出，策略单点存放，runner 模式无关。
func wantIdentify(mode types.RunMode) bool {
	return mode == types.ModeScan || mode == types.ModeLinked
}

// wantCredential reports whether the current mode runs the Credential
// stage. Crack and Linked both run Credential; Scan does not.
// / wantCredential 报告当前模式是否跑 Credential 阶段。Crack 和
// Linked 都跑；Scan 不跑。
func wantCredential(mode types.RunMode) bool {
	return mode == types.ModeCrack || mode == types.ModeLinked
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
