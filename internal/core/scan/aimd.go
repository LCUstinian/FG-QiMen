// aimd.go — the AIMD controller's policy constants as an injectable
// tuning struct. Production callers leave it nil and get the shipped
// defaults; the A3 benchmark sweep (tools/bench -sweep) injects
// values to produce sensitivity curves.
//
// A zero-value convention applies per field: zero means "keep the
// default", so a partially-filled struct only overrides what it
// names. This is a measurement surface, not an operator surface —
// no CLI flag exposes it.
//
// / aimd.go —— AIMD 控制器策略常量的可注入调优结构。生产调用方保持
// nil 即得出厂默认；A3 基准扫描（tools/bench -sweep）注入取值以产
// 生敏感度曲线。
//
// 每字段适用零值约定：零 = 保持默认，半填结构体只覆写它点名的字段
// 。这是测量表面而非操作员表面——不暴露任何 CLI flag。
package scan

// AIMDTuning overrides the pool controller's policy constants
// (pool.go). nil = all defaults.
// / AIMDTuning 覆写池控制器的策略常量（pool.go）。nil = 全部默认。
type AIMDTuning struct {
	// SlowStartDiv is the slow-start birth divisor: the pool is born
	// at target/SlowStartDiv. Default 4.
	// / SlowStartDiv 是慢启动出生除数：池以 target/SlowStartDiv 出生
	// 。默认 4。
	SlowStartDiv int

	// AIStepDiv is the additive-increase divisor: each healthy
	// interval grows concurrency by target/AIStepDiv. Default 20
	// (5% of target per interval).
	// / AIStepDiv 是加性增除数：每个健康周期并发增长 target/AIStepDiv
	// 。默认 20（每周期 target 的 5%）。
	AIStepDiv int

	// MDStress is the multiplicative-decrease factor applied on a
	// Stressed verdict. Default 0.85.
	// / MDStress 是 Stressed 判定上施加的乘性减因子。默认 0.85。
	MDStress float64

	// MDCongest is the multiplicative-decrease factor applied on a
	// Congested verdict (and on a slow-start stress exit). Default
	// 0.5.
	// / MDCongest 是 Congested 判定（及慢启动压力退出）上施加的乘性
	// 减因子。默认 0.5。
	MDCongest float64

	// RatchetRatio is the sustained fast/slow RTT EMA ratio above
	// which the AIMD target ratchets down (maybeReduceTarget). Use a
	// very large value to disable the ratchet. Default 3.0.
	// / RatchetRatio 是触发 AIMD target 下压棘轮（maybeReduceTarget）
	// 的持续 fast/slow RTT EMA 比阈值。取极大值可关闭棘轮。默认 3.0。
	RatchetRatio float64
}

// defaultTuning is the shipped controller policy, extracted from the
// constants pool.go hard-coded before A3.
// / defaultTuning 是出厂控制器策略，提取自 A3 之前 pool.go 硬编码的
// 常量。
func defaultTuning() AIMDTuning {
	return AIMDTuning{
		SlowStartDiv: 4,
		AIStepDiv:    20,
		MDStress:     0.85,
		MDCongest:    0.5,
		RatchetRatio: 3.0,
	}
}

// resolved fills the zero fields with defaults. / resolved 用默认填
// 充零值字段。
func (t *AIMDTuning) resolved() AIMDTuning {
	r := defaultTuning()
	if t == nil {
		return r
	}
	if t.SlowStartDiv > 0 {
		r.SlowStartDiv = t.SlowStartDiv
	}
	if t.AIStepDiv > 0 {
		r.AIStepDiv = t.AIStepDiv
	}
	if t.MDStress > 0 {
		r.MDStress = t.MDStress
	}
	if t.MDCongest > 0 {
		r.MDCongest = t.MDCongest
	}
	if t.RatchetRatio > 0 {
		r.RatchetRatio = t.RatchetRatio
	}
	return r
}
