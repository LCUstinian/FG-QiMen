package types

import (
	"sync/atomic"
	"testing"
)

// TestStageConstantsAndName verifies the Stage* integer constants and the
// StageName() label function agree for every stage value.
// / TestStageConstantsAndName 验证 Stage* 整数常量与 StageName() 标签
// 函数对每个 stage 值一致。
func TestStageConstantsAndName(t *testing.T) {
	cases := []struct {
		stage int32
		name  string
	}{
		{StageIdle, "IDLE"},
		{StageAlive, "ALIVE"},
		{StagePortScan, "PORT-SCAN"},
		{StageIdentify, "IDENTIFY"},
		{StageCred, "CRED"},
		{StageDone, "DONE"},
	}
	for _, c := range cases {
		if got := StageName(c.stage); got != c.name {
			t.Errorf("StageName(%d) = %q, want %q", c.stage, got, c.name)
		}
	}
}

// TestStateStageStoreAndLoad verifies State.Stage defaults to StageIdle
// and accepts a Store(StageAlive).
// / TestStateStageStoreAndLoad 验证 State.Stage 默认 StageIdle，
// 并能正确 Store(StageAlive)。
func TestStateStageStoreAndLoad(t *testing.T) {
	s := NewState()
	if got := s.Stage.Load(); got != StageIdle {
		t.Errorf("default Stage = %d, want StageIdle=%d", got, StageIdle)
	}
	s.Stage.Store(StageAlive)
	if got := s.Stage.Load(); got != StageAlive {
		t.Errorf("after Store(StageAlive), Stage = %d, want %d", got, StageAlive)
	}
}

// TestStatePluginHitsView verifies PluginHitsView materializes the
// sync.Map of *atomic.Int64 into a plain map[string]int64.
// / TestStatePluginHitsView 验证 PluginHitsView 将 *atomic.Int64 的
// sync.Map 物化为 map[string]int64。
func TestStatePluginHitsView(t *testing.T) {
	s := NewState()
	// Direct manipulation of sync.Map for test setup.
	v, _ := s.PluginHits.LoadOrStore("ssh", &atomic.Int64{})
	v.(*atomic.Int64).Store(42)
	v2, _ := s.PluginHits.LoadOrStore("redis", &atomic.Int64{})
	v2.(*atomic.Int64).Store(7)
	view := s.PluginHitsView()
	if view["ssh"] != 42 || view["redis"] != 7 {
		t.Errorf("PluginHitsView = %v, want ssh=42 redis=7", view)
	}
}

// TestStateErrorCategoriesView verifies ErrorCategoriesView materializes
// the sync.Map of *atomic.Int64 into a plain map[string]int64.
// / TestStateErrorCategoriesView 验证 ErrorCategoriesView 将
// *atomic.Int64 的 sync.Map 物化为 map[string]int64。
func TestStateErrorCategoriesView(t *testing.T) {
	s := NewState()
	v, _ := s.ErrorCategories.LoadOrStore("timeout", &atomic.Int64{})
	v.(*atomic.Int64).Store(100)
	view := s.ErrorCategoriesView()
	if view["timeout"] != 100 {
		t.Errorf("ErrorCategoriesView[timeout] = %d, want 100", view["timeout"])
	}
}

// TestStateSnapshotIncludesStage verifies Snapshot() populates the new
// Stage int64 field.
// / TestStateSnapshotIncludesStage 验证 Snapshot() 填充新的 Stage int64
// 字段。
func TestStateSnapshotIncludesStage(t *testing.T) {
	s := NewState()
	s.Stage.Store(StageIdentify)
	snap := s.Snapshot()
	if snap.Stage != int64(StageIdentify) {
		t.Errorf("Snapshot.Stage = %d, want %d", snap.Stage, StageIdentify)
	}
}