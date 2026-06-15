// core/pipeline_sink.go — result sink + stats ticker.
//
// Splits out the consumer side of the pipeline (and the
// periodic UI.Stats pusher) from the producer side
// (pipeline_workers.go) and the pure helpers (pipeline.go).
// Data flow:
//
//   pipeline_workers.runPluginWorker
//          ↓
//   results channel
//          ↓
//   runResultSink  →  Output.WriteResult / WriteCred / WriteRDP
//                   →  Store.PutResult / PutCred / MarkSeenPersisted
//                   →  (consumed by the bbolt session state)
//
//   pushStats ticker (1Hz default) → UI.Stats
//
// core/pipeline_sink.go — 结果汇 + stats 滴答。
//
// 把管线的消费侧（以及周期性 UI.Stats 推送）从生产侧
// （pipeline_workers.go）和纯 helper（pipeline.go）中拆出。数据流：
//
//   pipeline_workers.runPluginWorker
//          ↓
//   results channel
//          ↓
//   runResultSink  →  Output.WriteResult / WriteCred / WriteRDP
//                   →  Store.PutResult / PutCred / MarkSeenPersisted
//                   →  （bbolt session state 消费）
//
//   pushStats ticker（默认 1Hz）→ UI.Stats
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
