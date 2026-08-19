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
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/output"
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
func persistResult(sess *session.Session, r *types.Result) {
	if r == nil {
		return
	}
	if sess.Out != nil {
		_ = sess.Out.WriteResult(r)
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
				_ = sess.Out.WriteCred(r)
			}
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
	_ = sess.Store.PutResult(hash, r)
	if r.Cred != nil {
		chash := types.HashKey(r.Host, fmt.Sprintf("%d", r.Port), r.Service, r.Plugin, r.Cred.User, r.Cred.Pass)
		_ = sess.Store.PutCred(chash, r)
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
