// custom_test.go — tests for the user-supplied ruleset loader.
// custom_test.go — 用户提供的规则集加载器测试。
package fingerprint

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
