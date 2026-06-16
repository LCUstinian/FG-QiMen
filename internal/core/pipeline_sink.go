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

// persistResult writes a single result to Output + Store.
// persistResult 把单个结果写入 Output + Store。
func persistResult(sess *session.Session, r *types.Result) {
	if r == nil {
		return
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
