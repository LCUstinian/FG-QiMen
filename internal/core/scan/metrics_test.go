package scan

import (
	"testing"
	"time"
)

// seedStableRTT feeds n open results at a constant RTT so both EMAs
// converge to it (fast == slow ⇒ RTTRatio == 1.0).
// seedStableRTT 以恒定 RTT 喂 n 个 open 样本，让双 EMA 收敛到该值
// （fast == slow ⇒ RTTRatio == 1.0）。
func seedStableRTT(m *PoolMetrics, n int, rtt time.Duration) {
	for i := 0; i < n; i++ {
		m.RecordOpen(rtt)
	}
}

func TestPoolMetrics_RecordRTTSkipsNonPositive(t *testing.T) {
	m := &PoolMetrics{}
	m.RecordOpen(0)
	m.RecordRefused(0)
	m.RecordOpen(-time.Millisecond)
	if got := m.rttSamples.Load(); got != 0 {
		t.Fatalf("rttSamples = %d, want 0 (RTT <= 0 must never feed the EMAs)", got)
	}
	if got := m.rttFastNs.Load(); got != 0 {
		t.Fatalf("rttFastNs = %d, want 0", got)
	}
}

func TestPoolMetrics_RTTRatioNeutralBeforeWarmup(t *testing.T) {
	m := &PoolMetrics{}
	seedStableRTT(m, 19, time.Millisecond)
	if got := m.RTTRatio(); got != 1.0 {
		t.Fatalf("RTTRatio with %d samples = %v, want neutral 1.0", 19, got)
	}
	seedStableRTT(m, 1, time.Millisecond)
	if got := m.RTTRatio(); got != 1.0 {
		t.Fatalf("RTTRatio with stable RTT = %v, want 1.0", got)
	}
}

func TestPoolMetrics_RTTRatioRising(t *testing.T) {
	m := &PoolMetrics{}
	// Seed the baseline, then jump 50×: fast EMA tracks the jump much
	// quicker than slow, so the ratio must rise above the WAN stressed
	// cut-off (1.8) — the "latency escalating" signal.
	// / 先播种基线，再跳升 50 倍：fast EMA 远快于 slow 跟上跳变，比
	// 值必须升过 WAN 的 stressed 判据（1.8）——即"延迟恶化"信号。
	seedStableRTT(m, 20, time.Millisecond)
	seedStableRTT(m, 30, 50*time.Millisecond)
	if got := m.RTTRatio(); got <= 1.8 {
		t.Fatalf("RTTRatio after 50x jump = %v, want > 1.8", got)
	}
}

func TestAssessHealth_ExhaustionDrivesCongestion(t *testing.T) {
	m := &PoolMetrics{}
	var prev MetricsSnapshot
	// 3/30 exhausted on a LAN ⇒ 10% > 8% congested cut-off.
	// / LAN 上 3/30 耗尽 ⇒ 10% > 8% 拥塞判据。
	m.RecordExhausted()
	m.RecordExhausted()
	m.RecordExhausted()
	seedStableRTT(m, 27, time.Millisecond)
	health, snap := m.assessHealth(&prev, EnvLAN)
	if health != HealthCongested {
		t.Fatalf("health = %v, want HealthCongested (10%% exhaust > 8%% LAN cut-off)", health)
	}
	if snap.Total() != 30 {
		t.Fatalf("snapshot total = %d, want 30", snap.Total())
	}
	// prev must be replaced: re-assessing with no new samples yields
	// Unknown, not a re-read of the same window.
	// / prev 必须被替换：无新样本时再评估应得 Unknown，而非重复读取
	// 同一窗口。
	health, _ = m.assessHealth(&prev, EnvLAN)
	if health != HealthUnknown {
		t.Fatalf("re-assess with zero delta = %v, want HealthUnknown", health)
	}
}

func TestAssessHealth_SilenceDemotesGoodToOK(t *testing.T) {
	m := &PoolMetrics{}
	var prev MetricsSnapshot
	for i := 0; i < 30; i++ {
		m.RecordTimeout()
	}
	health, _ := m.assessHealth(&prev, EnvLAN)
	if health != HealthOK {
		t.Fatalf("health on pure silence = %v, want HealthOK (Good demoted; no ground truth)", health)
	}
}

func TestAssessHealth_OpenTrafficIsGood(t *testing.T) {
	m := &PoolMetrics{}
	var prev MetricsSnapshot
	seedStableRTT(m, 30, time.Millisecond)
	health, _ := m.assessHealth(&prev, EnvLAN)
	if health != HealthGood {
		t.Fatalf("health on healthy opens = %v, want HealthGood", health)
	}
}

func TestAssessHealth_TimeoutRatioIsNotStress(t *testing.T) {
	// The avalanche lesson: a window that is 100%% filtered must NOT
	// read as stressed/congested. Timeouts only dilute the exhaustion
	// denominator.
	// / 雪崩教训：100%% filtered 的窗口绝不能读作压力/拥塞。timeout
	// 只稀释耗尽率分母。
	m := &PoolMetrics{}
	var prev MetricsSnapshot
	seedStableRTT(m, 20, time.Millisecond) // warm the ratio to neutral
	for i := 0; i < 40; i++ {
		m.RecordTimeout()
	}
	health, _ := m.assessHealth(&prev, EnvLAN)
	if health == HealthStressed || health == HealthCongested {
		t.Fatalf("health on filtered-heavy window = %v, want not-stressed (avalanche lesson)", health)
	}
}

func TestThresholdsFor_UnknownEnvMapsToWAN(t *testing.T) {
	wan := thresholdsFor(EnvWAN)
	for _, env := range []Env{Env(""), Env("bogus")} {
		got := thresholdsFor(env)
		if got != wan {
			t.Fatalf("thresholdsFor(%q) = %+v, want WAN set %+v", env, got, wan)
		}
	}
}
