// core/pipeline_sink.go — result sink + stats ticker.
//
// Splits out the consumer side of the pipeline (and the
// periodic UI.Stats pusher) from the producer side
// (pipeline_workers.go) and the pure helpers (pipeline.go).
// Data flow:
//
//	pipeline_workers.runPluginWorker
//	       ↓
//	results channel
//	       ↓
//	runResultSink  →  Output.WriteResult / WriteCred / WriteRDP
//	                →  Store.PutResult / PutCred / MarkSeenPersisted
//	                →  (consumed by the bbolt session state)
//
//	pushStats ticker (1Hz default) → UI.Stats
//
// core/pipeline_sink.go — 结果汇 + stats 滴答。
//
// 把管线的消费侧（以及周期性 UI.Stats 推送）从生产侧
// （pipeline_workers.go）和纯 helper（pipeline.go）中拆出。数据流：
//
//	pipeline_workers.runPluginWorker
//	       ↓
//	results channel
//	       ↓
//	runResultSink  →  Output.WriteResult / WriteCred / WriteRDP
//	                →  Store.PutResult / PutCred / MarkSeenPersisted
//	                →  （bbolt session state 消费）
//
//	pushStats ticker（默认 1Hz）→ UI.Stats
package core

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/roles"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/store"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// runResultSink consumes Results and writes them to Output + bbolt.
// runResultSink 消费 Result 并写入 Output + bbolt。
//
// M1 audit fix: on ctx.Done() the previous code returned immediately,
// dropping up to 1024 buffered Results in the `in` channel. Now it
// enters drain mode: non-blocking reads from `in` until the channel
// closes or is empty, persisting every buffered result so SIGINT no
// longer loses data.
//
// M1 审计修法：ctx.Done() 时旧代码立即返回，丢弃 in channel 中最多
// 1024 个缓冲 Result。现在进入 drain 模式：非阻塞读 in 直到 channel
// 关闭或为空，持久化每个缓冲结果，SIGINT 不再丢数据。
func runResultSink(ctx context.Context, sess *session.Session, in <-chan *types.Result) {
	for {
		select {
		case <-ctx.Done():
			// Drain mode: persist buffered results before returning.
			// drain 模式：返回前持久化缓冲结果。
			drainResults(sess, in)
			return
		case r, ok := <-in:
			if !ok {
				return
			}
			persistResult(sess, r)
		}
	}
}

// drainResults non-blocking-drains `in` and persists every result.
// drainResults 非阻塞排空 in 并持久化每个结果。
func drainResults(sess *session.Session, in <-chan *types.Result) {
	for {
		select {
		case r, ok := <-in:
			if !ok {
				return
			}
			persistResult(sess, r)
		default:
			return
		}
	}
}

// persistResult writes a single result to Output + Store. When the
// session has a BatchWriter wired (default in project mode unless
// --no-batch), bbolt writes are enqueued and flushed in batches —
// amortising the per-write fsync overhead. The per-write path is
// preserved for ephemeral mode (Store==nil) and the --no-batch
// fallback. / persistResult 把单个结果写入 Output + Store。当 session
// 接入了 BatchWriter（项目模式默认，--no-batch 关闭）时，bbolt 写
// 入队并批量刷盘，摊销每次写的 fsync 开销。per-write 路径保留
// 给 ephemeral 模式（Store==nil）和 --no-batch 回退。
//
// Sink error propagation (audit residual): Output write errors are
// surfaced via sess.Log.Warn rather than silently discarded, so a
// failing fsync or permission error no longer masks as "result not
// visible in creds.txt" without diagnostics. / Sink 错误传播（审计残
// 项）：Output 写错误通过 sess.Log.Warn 暴露，不再静默丢弃——
// fsync 失败或权限错误不会再被掩盖成"creds.txt 看不到结果且无诊断"。
func persistResult(sess *session.Session, r *types.Result) {
	if r == nil {
		return
	}
	// v0.9: tag important-server roles BEFORE any sink writes so the
	// NDJSON line carries the evidence and the servers inventory sees
	// the tag. Positive share evidence (a null session that listed a
	// share) upgrades the host to file-server. The per-host highlight
	// logs once (MarkSeen dedup), the inventory aggregates silently.
	// / v0.9：在任何 sink 写出之前打重要服务器角色标签，让 NDJSON 行
	// 自带证据、servers 清单看得到标记。正面共享证据（null session 列
	// 出了共享）把主机升格为 file-server。逐 host 高亮只打一次
	//（MarkSeen 去重），清单静默聚合。
	if roles := roles.Evaluate(r); len(roles) > 0 {
		r.Important = roles
		if sess.State.MarkSeen("important:" + r.Host) {
			sess.Log.Info("[!] 重要服务器: %s:%d roles=%v", r.Host, r.Port, roles)
		}
	}
	if shareFP, ok := r.Extra.(*types.ShareEnumResult); ok {
		if fileRoles := roles.EvaluateShareEnum(shareFP); len(fileRoles) > 0 {
			r.Important = appendUniqueRoles(r.Important, fileRoles)
			if sess.State.MarkSeen("important:" + r.Host) {
				sess.Log.Info("[!] 重要服务器: %s roles=%v (可匿名列出的共享)", r.Host, fileRoles)
			}
		}
	}
	if sess.Out != nil {
		if err := sess.Out.WriteResult(r); err != nil {
			sess.Log.Warn("output write result failed: %v", err)
		}
		// P2-5 (audit): creds.txt is opened with O_APPEND, so without
		// dedup a re-dispatched (host, port, user, pass) hit would be
		// appended a second time on --resume or after a retry. We
		// gate WriteCred on the in-memory State seen-set keyed by
		// chash (host+port+service+plugin+user+pass); MarkSeen returns
		// true on first occurrence only. / P2-5（审计）：creds.txt 用
		// O_APPEND 打开，不去重则重发的 (host, port, user, pass) 命中
		// 会在 --resume 或重试后追加第二遍。我们用按 chash
		// （host+port+service+plugin+user+pass）索引的内存 State 去
		// 重 gate WriteCred；MarkSeen 仅在首次出现时返 true。
		if r.Cred != nil {
			chash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin, r.Cred.User, r.Cred.Pass)
			if sess.State.MarkSeen(chash) {
				if err := sess.Out.WriteCred(r); err != nil {
					sess.Log.Warn("output write cred failed: %v", err)
				}
			}
		}
		// Typed side-channel: if a plugin stashed a
		// *types.RDPFingerprint in Extra, dual-write it
		// to rdp.json / rdp.txt. The type lives in types (not
		// output) so the producing plugin never imports the
		// output layer. / 类型化旁路：如果插件把
		// *types.RDPFingerprint 放在 Extra 里，双写到
		// rdp.json / rdp.txt。类型放 types（非 output），
		// 生产方插件因此不依赖输出层。
		if rdpFP, ok := r.Extra.(*types.RDPFingerprint); ok {
			if err := sess.Out.WriteRDP(*rdpFP); err != nil {
				sess.Log.Warn("output write rdp failed: %v", err)
			}
		}
		// Same side-channel for webtitle: *types.WebFingerprint
		// dual-writes web.json / web.txt (response facts + TLS leaf
		// identity). / webtitle 的同一旁路：*types.WebFingerprint
		// 双写 web.json / web.txt（响应事实 + TLS 叶子证书身份）。
		if webFP, ok := r.Extra.(*types.WebFingerprint); ok {
			if err := sess.Out.WriteWeb(*webFP); err != nil {
				sess.Log.Warn("output write web failed: %v", err)
			}
			// v0.9 (需求A): TLS SAN / CN routinely leak internal
			// hostnames the operator never asked for — record each as a
			// tls-san discovery event (hostname-only events cannot be
			// probed but are evidence; the tracker decides in/out of
			// scope). / v0.9（需求A）：TLS SAN/CN 常暴露操作员从未要求
			// 的内网主机名——逐条记为 tls-san 发现事件（仅主机名的事件
			// 无法探测但属证据；范围内外由追踪器判定）。
			if sess.Scope != nil {
				for _, san := range webFP.CertSANs {
					if san == "" {
						continue
					}
					ev := types.DiscoveryEvent{
						Time:           r.Time,
						SourceProtocol: "tls-san",
						Attributes:     map[string]string{"via": r.Host},
					}
					if ip := net.ParseIP(strings.TrimSpace(san)); ip != nil {
						ev.IP = ip.String()
					} else {
						ev.Hostname = strings.TrimSpace(san)
					}
					sess.Scope.Record(ev)
				}
			}
		}
		// v0.9 enumeration side-channels: SMB shares → shares.ndjson/txt,
		// FTP walks → ftp.ndjson/txt. / v0.9 枚举旁路：SMB 共享 →
		// shares.ndjson/txt，FTP 遍历 → ftp.ndjson/txt。
		if shareFP, ok := r.Extra.(*types.ShareEnumResult); ok {
			if err := sess.Out.WriteShares(*shareFP); err != nil {
				sess.Log.Warn("output write shares failed: %v", err)
			}
		}
		if ftpFP, ok := r.Extra.(*types.FTPEnumResult); ok {
			if err := sess.Out.WriteFTP(*ftpFP); err != nil {
				sess.Log.Warn("output write ftp failed: %v", err)
			}
		}
	}
	if sess.Store == nil {
		return
	}
	hash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin)
	// Batched path: enqueue and let the BatchWriter goroutine flush.
	// / 批量路径：入队让 BatchWriter goroutine 刷盘。
	if sess.BatchWriter != nil {
		sess.BatchWriter.Enqueue(store.PutOp{Kind: store.PutOpResult, Hash: hash, Value: r})
		if r.Cred != nil {
			chash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin, r.Cred.User, r.Cred.Pass)
			sess.BatchWriter.Enqueue(store.PutOp{Kind: store.PutOpCred, Hash: chash, Value: r})
		}
		return
	}
	// Per-write path (--no-batch fallback or pre-batch callers).
	// / Per-write 路径（--no-batch 回退或 pre-batch 调用方）。
	if err := sess.Store.PutResult(hash, r); err != nil {
		sess.Log.Warn("store put result failed: %v", err)
	}
	if r.Cred != nil {
		chash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin, r.Cred.User, r.Cred.Pass)
		if err := sess.Store.PutCred(chash, r); err != nil {
			sess.Log.Warn("store put cred failed: %v", err)
		}
	}
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

// appendUniqueRoles merges extra role strings into base, preserving
// order and skipping duplicates. / appendUniqueRoles 把附加角色串并入
// base，保序去重。
func appendUniqueRoles(base, extra []string) []string {
	for _, e := range extra {
		found := false
		for _, b := range base {
			if b == e {
				found = true
				break
			}
		}
		if !found {
			base = append(base, e)
		}
	}
	return base
}
