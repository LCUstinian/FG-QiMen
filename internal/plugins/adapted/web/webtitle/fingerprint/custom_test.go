// custom_test.go — tests for the user-supplied ruleset loader.
// custom_test.go — 用户提供的规则集加载器测试。
package fingerprint

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCustomRuleset_NativeFormat(t *testing.T) {
	// Reset state. / 重置状态。
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()
	t.Cleanup(func() {
		customRulesMu.Lock()
		customRules = nil
		pendingCustomEntries = nil
		customRulesMu.Unlock()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	native := `{
		"rules": [
			{
				"name": "MyCustomApp",
				"category": "app",
				"matchers": [
					{"part": "body", "type": "word", "values": ["my-custom-app-marker"]}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(native), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	added, err := LoadCustomRuleset(path)
	if err != nil {
		t.Fatalf("LoadCustomRuleset: %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
	rules := CustomRules()
	if len(rules) != 1 || rules[0].Info.Name != "MyCustomApp" {
		t.Errorf("CustomRules = %+v, want [{MyCustomApp}]", rules)
	}
	entries := CustomEntries()
	if len(entries) != 1 {
		t.Fatalf("CustomEntries len = %d, want 1", len(entries))
	}
	if len(entries[0].Matchers) != 1 {
		t.Errorf("entry matchers = %d, want 1", len(entries[0].Matchers))
	}
}

func TestLoadCustomRuleset_EHoleFormat(t *testing.T) {
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()
	t.Cleanup(func() {
		customRulesMu.Lock()
		customRules = nil
		pendingCustomEntries = nil
		customRulesMu.Unlock()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "ehole.json")
	ehole := `{
		"cms": [
			{
				"name": "WordPress",
				"method": "keyword",
				"location": "body",
				"keyword": ["wp-content", "WordPress"]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(ehole), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	added, err := LoadCustomRuleset(path)
	if err != nil {
		t.Fatalf("LoadCustomRuleset: %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
	rules := CustomRules()
	if len(rules) != 1 || rules[0].Info.Name != "WordPress" {
		t.Errorf("CustomRules = %+v, want [{WordPress}]", rules)
	}
}

func TestLoadCustomRuleset_InvalidRegex(t *testing.T) {
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	bad := `{"rules":[{"name":"Bad","matchers":[{"part":"body","type":"regex","values":["["]}]}]}`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadCustomRuleset(path)
	if err == nil {
		t.Error("expected error on invalid regex, got nil")
	}
}

// TestLoadCustomRuleset_TooManyRules — a ruleset declaring more than
// maxCustomRules rules must be refused (audit M-1 guard: matching is
// O(rules × body) per target, so an oversized set silently degrades
// every scan). / TestLoadCustomRuleset_TooManyRules — 声明超过
// maxCustomRules 条的规则集必须被拒（审计 M-1 护栏：每目标匹配是
// O(规则数 × body)，超大集合会悄悄拖慢所有扫描）。
func TestLoadCustomRuleset_TooManyRules(t *testing.T) {
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()
	t.Cleanup(func() {
		customRulesMu.Lock()
		customRules = nil
		pendingCustomEntries = nil
		customRulesMu.Unlock()
	})

	var b strings.Builder
	b.WriteString(`{"rules":[`)
	for i := 0; i <= maxCustomRules; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"r%d","matchers":[{"part":"body","type":"word","values":["x"]}]}`, i)
	}
	b.WriteString(`]}`)

	dir := t.TempDir()
	path := filepath.Join(dir, "toomany.json")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadCustomRuleset(path)
	if err == nil {
		t.Fatal("expected refusal for oversized ruleset, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to load") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestLoadCustomRuleset_TooManyBytes — a local file above
// maxRulesetBytes is refused before parsing (the URL path already
// capped inside fetchURL; the file path had no cap). /
// TestLoadCustomRuleset_TooManyBytes — 本地文件超 maxRulesetBytes
// 在解析前被拒（URL 路径在 fetchURL 内已限；文件路径原本无上限）。
func TestLoadCustomRuleset_TooManyBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.json")
	blob := make([]byte, maxRulesetBytes+1)
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadCustomRuleset(path)
	if err == nil {
		t.Fatal("expected refusal for oversized ruleset file, got nil")
	}
	if !strings.Contains(err.Error(), "bytes (cap") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadCustomRuleset_UnknownFormat(t *testing.T) {
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "weird.json")
	if err := os.WriteFile(path, []byte(`{"foo":[]}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadCustomRuleset(path)
	if err == nil {
		t.Error("expected error on unknown format, got nil")
	}
}

// TestLoadCustomRuleset_URL verifies the http:// and https://
// prefixes route through the HTTP fetcher, not the file reader.
// We use a custom httptest server so the test is hermetic.
// / TestLoadCustomRuleset_URL 验证 http:// 和 https:// 前缀
// 走 HTTP fetcher 而非 file reader。用 httptest server 让
// 测试封闭。
func TestLoadCustomRuleset_URL(t *testing.T) {
	customRulesMu.Lock()
	customRules = nil
	pendingCustomEntries = nil
	customRulesMu.Unlock()
	t.Cleanup(func() {
		customRulesMu.Lock()
		customRules = nil
		pendingCustomEntries = nil
		customRulesMu.Unlock()
	})

	body := `{"rules":[{"name":"URLTest","matchers":[{"part":"body","type":"word","values":["marker-from-url"]}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	added, err := LoadCustomRuleset(srv.URL + "/rules.json")
	if err != nil {
		t.Fatalf("LoadCustomRuleset(URL): %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}
	rules := CustomRules()
	if len(rules) != 1 || rules[0].Info.Name != "URLTest" {
		t.Errorf("CustomRules = %+v, want [{URLTest}]", rules)
	}
}

// TestLoadCustomRuleset_URL_BadStatus verifies a non-200 response
// is surfaced as an error rather than a silent skip.
// / TestLoadCustomRuleset_URL_BadStatus 验证非 200 响应以错
// 误报出而非静默跳过。
func TestLoadCustomRuleset_URL_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	_, err := LoadCustomRuleset(srv.URL + "/rules.json")
	if err == nil {
		t.Error("expected error on 500 response, got nil")
	}
}
