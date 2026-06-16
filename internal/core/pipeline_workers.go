// core/pipeline_workers.go — plugin dispatch + credential spray.
//
// Splits out the goroutine-heavy "fan ScanItem out to the right
// plugin and try creds" path from the pure helpers in
// pipeline.go. The split is by data flow, not size: this file
// has a single start, no top-level gluing. RunScan in scanner.go
// starts N copies of runPluginWorker + one runResultSink.
//
// core/pipeline_workers.go — plugin 分发 + 凭据喷洒。
//
// 把 goroutine 密集的"ScanItem 分发到对应 plugin 并试凭据"路径
// 从 pipeline.go 的纯 helper 中拆出。拆法按数据流而非文件大小：此
// 文件只有一个入口，零顶层装配。scanner.go 的 RunScan 启 N 个
// runPluginWorker + 一个 runResultSink。
package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// runPluginWorker is the consumer that fans ScanItems out to the
// matching plugin (Identify) and dispatches credential testing
// (core/cred) for plugins that declared ModeCredential in
// linked/crack mode.
//
// Stage 0 of each iteration: Nmap-style banner fingerprinting via
// fingerprint.MatchBanner. Runs before plugins so the identified
// service is in the result stream. Plugins can still run after to
// add protocol-specific detail (e.g. webtitle on http ports).
//
// runPluginWorker 是 consumer，把 ScanItem 分发给匹配的 plugin
// （Identify），在 linked/crack 模式下对声明 ModeCredential 的
// plugin 派发凭据测试（core/cred）。
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
	// M7 audit fix: recover from panics in plugin Identify / credential
	// Authenticate so a single buggy plugin doesn't crash the whole scan.
	// M7 审计修法：恢复 plugin Identify / credential Authenticate 中的
	// panic，避免单个有 bug 的 plugin 拖垮整个扫描。
	defer func() {
		if r := recover(); r != nil {
			sess.Log.Warn("plugin worker panic: %v", r)
		}
	}()
	creds := loadCreds(sess)
	// Lazy VScan. Built on first banner we see. / 懒 VScan。
	var vscan *fingerprint.VScan
	vscanOnce := sync.Once{}
	// Pre-compute the plugin filter once instead of per item (m6 audit).
	// 预先计算一次 plugin 过滤，而非每个 item 重算（m6 审计）。
	selected := selectPlugins(plugins.All(), sess.Config.Plugins)

	for {
		select {
		case <-ctx.Done():
			// M2 audit fix: do not return immediately; drain remaining
			// items so already-in-flight work is not silently dropped.
			// We exit after the channel closes or drains empty.
			// M2 审计修法：不立即返回；排空剩余 item，避免已 in-flight
			// 的工作被静默丢弃。channel 关闭或排空后退出。
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			// Stage 0: fingerprint banner match (always on).
			// Stage 0：fingerprint banner 匹配（始终跑）。
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
						// M2 audit fix: on ctx.Done(), persist the already-
						// constructed result synchronously instead of
						// dropping it. / M2 审计修法：ctx.Done() 时同步
						// 持久化已构造的结果，而非丢弃。
						select {
						case out <- r:
						case <-ctx.Done():
							persistResultInline(sess, r)
							return
						}
					}
				}
			}
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
						// M2 audit fix: persist already-constructed result
						// on ctx.Done() instead of dropping. / M2 审计修法：
						// ctx.Done() 时持久化已构造的结果，而非丢弃。
						select {
						case out <- r:
						case <-ctx.Done():
							persistResultInline(sess, r)
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

// persistResultInline writes a result directly to Output + Store when the
// normal sink path is unavailable (ctx canceled, out channel blocked).
// Used by M2 drain paths. / persistResultInline 在正常 sink 路径不可用
// （ctx 取消、out channel 阻塞）时直接把结果写入 Output + Store。M2 drain 路径使用。
func persistResultInline(sess *session.Session, r *types.Result) {
	if r == nil {
		return
	}
	if sess.Out != nil {
		_ = sess.Out.WriteResult(r)
		if r.Cred != nil {
			_ = sess.Out.WriteCred(r)
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
	if err != nil {
		// (P3 / F11 in the v0.2 audit) Authenticate returned an error
		// distinct from "miss" — network failure, ctx cancel, protocol
		// parse error, etc. The previous code dropped these silently,
		// making a misconfigured DSN / firewall / wrong port look
		// identical to a clean run. Surface to the log so an operator
		// running -v sees the failure.
		//
		// （v0.2 审计 P3 / F11）Authenticate 返回了和"miss"不同的
		// 错误——网络失败、ctx 取消、协议解析错误等。旧代码静默丢
		// 弃，让配错的 DSN / 防火墙 / 错端口看起来和干净跑一样。暴露到
		// log 让操作员在 -v 下看到失败。
		sess.Log.Warn("cred auth error: %s:%d [%s]: %v", host, port, serviceName, err)
		return
	}
	if hit == nil {
		// Authentic miss — no cred matched. Don't log per-miss to
		// keep the log clean; the State.Counters.Results increment
		// in the caller tells the operator "we did try".
		//
		// 认证 miss——没有凭据匹配。不逐条记 miss 以保 log 干净；调用
		// 方的 State.Counters.Results 自增告诉操作员"我们试过了"。
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
