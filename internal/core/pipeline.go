// core/pipeline.go — plugin workers + result sink + cred loader.
// core/pipeline.go — plugin worker + 结果汇 + 凭据加载。
//
// The big stages (alive, port scan) live in their own packages
// (core/alive, core/scan). This file holds the smaller glue:
//   - runPluginWorker : consumes ScanItems, dispatches Identify + Credential
//   - runResultSink   : writes results to Output + bbolt
//   - loadCreds       : builds []types.Cred from cfg
//   - pushStats       : periodic UI.Stats pusher
//
// 主要的阶段（alive、端口扫描）独立成包（core/alive、core/scan）。
// 本文件做较小的装配：
//   - runPluginWorker : 消费 ScanItem，分发 Identify + Credential
//   - runResultSink   : 写结果到 Output + bbolt
//   - loadCreds       : 从 cfg 构造 []types.Cred
//   - pushStats       : 周期性 UI.Stats 推送
package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// runPluginWorker is the consumer that fans ScanItems out to the
// matching plugin (Identify) and dispatches credential testing
// (core/cred) for plugins that declared ModeCredential in linked/crack mode.
//
// runPluginWorker 是 consumer，把 ScanItem 分发给匹配的 plugin（Identify），
// 在 linked/crack 模式下对声明 ModeCredential 的 plugin 派发凭据测试
// （core/cred）。
//
// Stage 0 of each iteration: Nmap-style banner fingerprinting via
// fingerprint.MatchBanner. Runs before plugins so the identified
// service is in the result stream. Plugins can still run after to
// add protocol-specific detail (e.g. webtitle on http ports).
//
// 每轮迭代的 stage 0：Nmap 风格 banner 指纹（fingerprint.MatchBanner）。
// 在 plugin 之前跑，识别结果先进结果流。plugin 仍可随后跑补协议细节
// （如 http 端口的 webtitle）。
func runPluginWorker(
	ctx context.Context,
	sess *session.Session,
	in <-chan types.ScanItem,
	out chan<- *types.Result,
) {
	creds := loadCreds(sess)
	// Lazy VScan. Built on first banner we see. / 懒 VScan。
	var vscan *fingerprint.VScan
	vscanOnce := sync.Once{}

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			// Stage 0: fingerprint banner match (always on).
			// / Stage 0：fingerprint banner 匹配（始终跑）。
			if item.Banner != "" {
				vscanOnce.Do(func() { vscan = fingerprint.NewVScan() })
				if vscan != nil {
					if svc, ver, ok := vscan.MatchBanner([]byte(item.Banner)); ok {
						r := &types.Result{
							Host:    item.Host,
							Port:    item.Port,
							Service: svc,
							Banner:  formatPortfinger(svc, ver, item.Banner),
							Time:    time.Now(),
						}
						sess.State.Counters.Results.Add(1)
						sess.UI.Event(r)
						select {
						case out <- r:
						case <-ctx.Done():
							return
						}
					}
				}
			}
			// Apply the -plugins filter (if any) before dispatching.
			// Empty cfg.Plugins means "all plugins" (the default).
			// / 在分发前应用 -plugins 过滤（若有）。空 cfg.Plugins
			// 意为"全部插件"（默认）。
			selected := selectPlugins(plugins.All(), sess.Config.Plugins)
			for _, p := range selected {
				if !matchesPort(p.Ports(), item.Port) {
					continue
				}
				// Identify / 识别
				if sess.Config.Mode == types.ModeScan || sess.Config.Mode == types.ModeLinked {
					hash := types.HashKey(item.Host, fmt.Sprintf("%d", item.Port), p.Name(), "identify")
					if sess.State.Seen(hash) {
						continue
					}
					if r := p.Identify(ctx, item.Host, item.Port); r != nil {
						r.Time = nowOrZero(r.Time)
						r.Plugin = p.Name()
						r.Service = p.Name()
						sess.State.MarkSeen(hash)
						if sess.Store != nil {
							_ = sess.Store.MarkSeenPersisted(hash, time.Now())
						}
						sess.State.Counters.Results.Add(1)
						sess.UI.Event(r)
						select {
						case out <- r:
						case <-ctx.Done():
							return
						}
					}
				}
				// Credential / 凭据测试
				if (sess.Config.Mode == types.ModeCrack || sess.Config.Mode == types.ModeLinked) &&
					p.Modes()&plugins.ModeCredential != 0 && len(creds) > 0 {
					// Defer credential testing to the central credential.Scheduler
					// via dispatchCred (sync, one-target inline call). The
					// plugin's own Credential method is bypassed to avoid
					// duplicate logic. / 凭据测试走中央 credential.Scheduler
					// （sync，单 target 内联调用）。绕过 plugin 自己的
					// Credential 方法以避免重复逻辑。
					dispatchCred(ctx, sess, p.Name(), item.Host, item.Port, creds, out)
				}
			}
		}
	}
}

// dispatchCred runs a single-target credential test via core/cred.
// dispatchCred 通过 core/cred 跑单 target 凭据测试。
func dispatchCred(
	ctx context.Context,
	sess *session.Session,
	serviceName, host string,
	port int,
	commonCreds []types.Cred,
	out chan<- *types.Result,
) {
	auth, ok := credential.LookupAuthenticator(serviceName)
	if !ok || auth == nil {
		return
	}
	// Translate types.Cred → credential.Cred at the boundary. The two types
	// carry the same payload but live in different packages to avoid
	// a common/cred import cycle. / 在边界把 types.Cred 翻译成
	// credential.Cred。两个类型内容一样但在不同包以避免 common/cred 循环引用。
	creds := make([]credential.Cred, len(commonCreds))
	for i, c := range commonCreds {
		creds[i] = credential.Cred{User: c.User, Pass: c.Pass, Method: credential.AuthMethod(c.AuthType)}
	}
	hit, err := auth.Authenticate(ctx, host, port, creds, 3*time.Second)
	if err != nil || hit == nil {
		return
	}
	sess.State.Counters.Creds.Add(1)
	r := &types.Result{
		Host:    host,
		Port:    port,
		Service: serviceName,
		Time:    time.Now(),
		Cred: &types.Cred{
			User:     hit.Cred.User,
			Pass:     hit.Cred.Pass,
			AuthType: string(hit.Cred.Method),
		},
	}
	sess.UI.CredFound(r)
	select {
	case out <- r:
	case <-ctx.Done():
	}
}

// runResultSink consumes Results and writes them to Output + bbolt.
// runResultSink 消费 Result 并写入 Output + bbolt。
func runResultSink(ctx context.Context, sess *session.Session, in <-chan *types.Result) {
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-in:
			if !ok {
				return
			}
			if r == nil {
				continue
			}
			if sess.Out != nil {
				_ = sess.Out.WriteResult(r)
				if r.Cred != nil {
					_ = sess.Out.WriteCred(r)
				}
				// Typed side-channel: if a plugin stashed a
				// *output.RDPFingerprint in Extra, dual-write it
				// to rdp.json / rdp.txt. / 类型化旁路：如果插件把
				// *output.RDPFingerprint 放在 Extra 里，双写到
				// rdp.json / rdp.txt。
				if rdpFP, ok := r.Extra.(*output.RDPFingerprint); ok {
					_ = sess.Out.WriteRDP(*rdpFP)
				}
			}
			if sess.Store != nil {
				hash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin)
				_ = sess.Store.PutResult(hash, r)
				if r.Cred != nil {
					chash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin, r.Cred.User, r.Cred.Pass)
					_ = sess.Store.PutCred(chash, r)
				}
			}
		}
	}
}

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

// pushStats periodically pushes the current counters snapshot to the UI.
// Exits when ctx is canceled.
//
// pushStats 周期性把当前计数器快照推给 UI。ctx 取消时退出。
func pushStats(ctx context.Context, sess *session.Session, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sess.UI.Stats(sess.State)
		}
	}
}

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
