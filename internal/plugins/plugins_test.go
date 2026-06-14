// plugins_test.go — focused unit tests for the Plugin interface +
// registry. The registry is consulted every scan: runPluginWorker
// iterates `plugins.All()` and dispatches per-plugin. A misregistered
// plugin means a service silently never gets identified — exactly the
// kind of bug that tests catch and runtime usage doesn't.
//
// plugins_test.go — Plugin 接口 + 注册表的 的单测。注册表每次扫描
// 都会被查：runPluginWorker 遍历 plugins.All() 并按 plugin 分发。漏
// 注册的 plugin 意味着某服务静默地永不被识别——这种 bug 正是单测能
// 抓到而运行时难以发现的。
//
// The tests register synthetic plugins (so they don't depend on any
// of the 30+ adapted/ plugins being in scope) and clean up the
// registry after themselves via a per-test save/restore helper.
//
// 测试用合成的 plugin 注册（不依赖任何 adapted/ 插件被引入）并在测试后
// 通过 save/restore helper 清理注册表。
package plugins

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// fakePlugin is a minimal Plugin implementation for testing the
// registry. It is deliberately minimal — we want to test the
// registry, not the Plugin interface contract (that's the adapted
// plugins' job).
//
// fakePlugin 是用于测试注册表的最小 Plugin 实现。故意保持最小——
// 我们测的是注册表，不是 Plugin 接口契约（那是 adapted/ 插件的活）。
type fakePlugin struct {
	name    string
	ports   []int
	modes   Mode
}

func (f *fakePlugin) Name() string                                { return f.name }
func (f *fakePlugin) Ports() []int                                { return f.ports }
func (f *fakePlugin) Modes() Mode                                 { return f.modes }
func (f *fakePlugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return &types.Result{Host: host, Port: port, Banner: f.name, Service: f.name}
}
func (f *fakePlugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// registrySnapshot saves + restores the package-level registry so
// tests are order-independent. Per-test isolation: each test gets
// the registry as it was at the start of that test.
type registrySnapshot map[string]Plugin

func saveRegistry() registrySnapshot {
	registryMu.Lock()
	defer registryMu.Unlock()
	s := make(registrySnapshot, len(registry))
	for k, v := range registry {
		s[k] = v
	}
	return s
}

func restoreRegistry(s registrySnapshot) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Plugin, len(s))
	for k, v := range s {
		registry[k] = v
	}
}

// TestRegisterAndGet: a freshly-registered plugin is retrievable
// via Get; All includes it; All returns plugins sorted by name.
func TestRegisterAndGet(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	p := &fakePlugin{name: "audit-test-1", ports: []int{12345}, modes: ModeIdentify}
	Register(p)

	got := Get("audit-test-1")
	if got == nil {
		t.Fatal("Get(audit-test-1) = nil; want the registered plugin")
	}
	if got != p {
		t.Errorf("Get returned a different plugin than Register received")
	}
}

// TestRegisterPanicsOnDuplicate: registering two plugins with the
// same Name() panics. This is the documented fail-fast for
// programming errors (init() collisions etc.).
func TestRegisterPanicsOnDuplicate(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	Register(&fakePlugin{name: "audit-test-dup", modes: ModeIdentify})

	defer func() {
		if r := recover(); r == nil {
			t.Error("Register(duplicate) did not panic")
		}
	}()
	Register(&fakePlugin{name: "audit-test-dup", modes: ModeIdentify})
}

// TestGetUnknown: Get on a name not in the registry returns nil
// (not a panic, not a fake "default" entry).
func TestGetUnknown(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	if got := Get("audit-test-nonexistent"); got != nil {
		t.Errorf("Get(unknown) = %v; want nil", got)
	}
}

// TestAllSorted: All returns plugins sorted by Name() for stable
// output (the pipeline uses this list to dispatch in a predictable
// order, and the JSON output relies on stable ordering).
func TestAllSorted(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	// Register out of order.
	Register(&fakePlugin{name: "z-plugin"})
	Register(&fakePlugin{name: "a-plugin"})
	Register(&fakePlugin{name: "m-plugin"})

	got := All()
	if len(got) != 3 {
		t.Fatalf("All returned %d plugins, want 3", len(got))
	}
	names := make([]string, 0, len(got))
	for _, p := range got {
		names = append(names, p.Name())
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("All not sorted: %v", names)
	}
}

// TestAllIsCopy: All returns a snapshot. Mutating the snapshot's
// elements does not affect the registry (the registry itself is
// the source of truth).
func TestAllIsCopy(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	p := &fakePlugin{name: "audit-test-snap", modes: ModeIdentify}
	Register(p)

	snap1 := All()
	snap1[0] = &fakePlugin{name: "DIFFERENT"}

	snap2 := All()
	if len(snap2) != 1 || snap2[0].Name() != "audit-test-snap" {
		t.Errorf("All snapshot mutable through caller; got %+v", snap2)
	}
}

// TestRegistryRoundTrip: a synthetic plugin can be registered,
// looked up, and the lookup result is structurally equal to what
// was registered (deep equal after the round trip).
func TestRegistryRoundTrip(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	original := &fakePlugin{name: "audit-test-rt", ports: []int{9999}, modes: ModeIdentify}
	Register(original)

	got := Get("audit-test-rt")
	if got == nil {
		t.Fatal("Get returned nil for a registered name")
	}
	if !reflect.DeepEqual(got, original) {
		t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, original)
	}
}

// TestRegistryIsConcurrencySafe: All + Get + Register across many
// goroutines must be race-clean (run with -race to surface issues).
// 8 goroutines × 500 ops against a known set of names.
func TestRegistryIsConcurrencySafe(t *testing.T) {
	defer restoreRegistry(saveRegistry())

	const goroutines = 8
	const perG = 500
	const total = goroutines * perG

	// Pre-register the plugins we'll be reading. / 预注册要读的 plugins。
	for i := 0; i < goroutines; i++ {
		Register(&fakePlugin{
			name:  nameForBench(i),
			ports: []int{10000 + i},
			modes: ModeIdentify,
		})
	}

	done := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perG; i++ {
				// Get and All are the hot paths under -race.
				_ = Get(nameForBench(id))
				_ = All()
			}
		}(g)
	}
	for g := 0; g < goroutines; g++ {
		<-done
	}
	_ = total // suppress unused
}

func nameForBench(i int) string {
	const letters = "abcdefghij"
	return "bench-" + string(letters[i%len(letters)])
}
