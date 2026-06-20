// custom_test.go — tests for the user-supplied ruleset loader.
// custom_test.go — 用户提供的规则集加载器测试。
package fingerprint

import (
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
