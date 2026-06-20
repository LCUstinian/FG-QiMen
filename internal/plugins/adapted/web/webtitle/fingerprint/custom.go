// custom.go — load a user-supplied ruleset file and merge it with
// the built-in (FingerprintHub + hardcoded) rules. Phase D (audit
// roadmap): operators can drop in EHole-format JSON files without
// us maintaining a separate library.
//
// custom.go — 加载用户提供的规则集文件并与内置（FingerprintHub +
// 硬编码）规则合并。Phase D（审计路线图）：操作员可投喂 EHole 格式
// JSON 文件而我们不维护单独的库。
//
// Supported file formats (auto-detected by JSON shape):
//   1. "rules" key (FG-QiMen native) — flat list of rules.
//   2. "cms" key (EHole format) — each entry has "name" + "method"
//      + "keyword" / "regex" / "md5".

package fingerprint

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
)

// matcher mirrors the FingerprintHub HTTP matcher shape. Defined
// as a named type so we can build it without inline struct literals
// of the same shape. / matcher 镜像 FingerprintHub HTTP matcher
// 形状。定义为命名类型以便直接构造。
type matcher struct {
	Type            string   `json:"type"`
	Words           []string `json:"words"`
	Regex           []string `json:"regex"`
	Hash            []string `json:"hash"`
	Part            string   `json:"part"`
	CaseInsensitive bool     `json:"case-insensitive"`
	Condition       string   `json:"condition"`
}

// httpEntry is one HTTP probe entry. / httpEntry 是一个 HTTP 探测
// 条目。
type httpEntry struct {
	Method   string    `json:"method"`
	Path     []string  `json:"path"`
	Matchers []matcher `json:"matchers"`
}

// customRule is the FG-QiMen native format. / customRule 是
// FG-QiMen 原生格式。
type customRule struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Matchers []struct {
		Part   string   `json:"part"`
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"matchers"`
}

// eHoleRule is the EHole format. / eHoleRule 是 EHole 格式。
type eHoleRule struct {
	CMS []struct {
		Name     string   `json:"name"`
		Method   string   `json:"method"`
		Location string   `json:"location"`
		Keyword  []string `json:"keyword"`
		Regex    []string `json:"regex"`
		MD5      []string `json:"md5"`
	} `json:"cms"`
}

var (
	customRulesMu sync.RWMutex
	customRules   []EnhancedFingerprint
)

// LoadCustomRuleset reads a JSON file, auto-detects its format, and
// registers the rules. Returns the number of rules added. /
// LoadCustomRuleset 读 JSON 文件，自动检测格式，注册规则。返回新
// 加规则数。
func LoadCustomRuleset(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read ruleset: %w", err)
	}
	return parseAndRegister(data)
}

// parseAndRegister detects the JSON shape and dispatches. /
// parseAndRegister 检测 JSON 形状并分派。
func parseAndRegister(data []byte) (int, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return 0, err
	}
	if _, ok := probe["rules"]; ok {
		return registerNative(data)
	}
	if _, ok := probe["cms"]; ok {
		return registerEHole(data)
	}
	return 0, fmt.Errorf("unknown ruleset format (expected 'rules' or 'cms' top-level key)")
}

// registerNative parses the FG-QiMen native format. / registerNative
// 解析 FG-QiMen 原生格式。
func registerNative(data []byte) (int, error) {
	var wrap struct {
		Rules []customRule `json:"rules"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return 0, err
	}
	customRulesMu.Lock()
	defer customRulesMu.Unlock()
	for _, r := range wrap.Rules {
		ef := EnhancedFingerprint{}
		ef.Info.Name = r.Name
		matches := []matcher{}
		for _, m := range r.Matchers {
			t := m.Type
			if t == "regex" {
				for _, p := range m.Values {
					if _, err := regexp.Compile(p); err != nil {
						return 0, fmt.Errorf("invalid regex %q in rule %q: %w", p, r.Name, err)
					}
				}
			}
			matches = append(matches, matcher{
				Type:  t,
				Words: m.Values,
				Part:  m.Part,
			})
		}
		if len(matches) == 0 {
			continue
		}
		// We can't directly build the inner HTTP struct (it's
		// anonymous in EnhancedFingerprint), so we use a temp
		// value with the matching shape via json round-trip.
		// / EnhancedFingerprint 的 inner HTTP struct 是匿名的，
		// 不能直接构造。我们通过 JSON 往返构建。
		entryJSON, _ := json.Marshal(httpEntry{
			Method:   "GET",
			Path:     []string{"/"},
			Matchers: matches,
		})
		var entry httpEntry
		_ = json.Unmarshal(entryJSON, &entry)
		// Re-attach the populated httpEntry to ef.HTTP via
		// the public AddMatcher helper if available; otherwise
		// use the LoadCustomRuleset caller path. / 如果有
		// 公开 AddMatcher helper 就用它；否则走 LoadCustomRuleset
		// caller 路径。这里直接通过 JSON 注入到 ef。
		_ = entry // placeholder — we use a side channel below
		// For simplicity: store the entry in a parallel slice
		// that the matcher engine reads after the built-in DB.
		// / 为简单：把 entry 存到并行 slice，matcher 引擎在
		// 内置 DB 之后读。
		pendingCustomEntries = append(pendingCustomEntries, entry)
		customRules = append(customRules, ef)
	}
	return len(wrap.Rules), nil
}

// registerEHole parses the EHole format. / registerEHole 解析
// EHole 格式。
func registerEHole(data []byte) (int, error) {
	var wrap eHoleRule
	if err := json.Unmarshal(data, &wrap); err != nil {
		return 0, err
	}
	customRulesMu.Lock()
	defer customRulesMu.Unlock()
	for _, c := range wrap.CMS {
		ef := EnhancedFingerprint{}
		ef.Info.Name = c.Name
		var t string
		switch c.Method {
		case "keyword":
			t = "word"
		case "regex":
			t = "regex"
		case "md5":
			t = "favicon"
		default:
			t = c.Method
		}
		if t == "regex" {
			for _, p := range c.Regex {
				if _, err := regexp.Compile(p); err != nil {
					return 0, fmt.Errorf("invalid regex %q in rule %q: %w", p, c.Name, err)
				}
			}
		}
		entry := httpEntry{
			Method: "GET",
			Path:   []string{"/"},
			Matchers: []matcher{{
				Type:  t,
				Words: c.Keyword,
				Regex: c.Regex,
				Hash:  c.MD5,
				Part:  c.Location,
			}},
		}
		pendingCustomEntries = append(pendingCustomEntries, entry)
		customRules = append(customRules, ef)
	}
	return len(wrap.CMS), nil
}

var pendingCustomEntries []httpEntry

// CustomRules returns a snapshot of the registered custom rules.
// Safe for concurrent use. / CustomRules 返回注册的自定义规则
// 快照。并发安全。
func CustomRules() []EnhancedFingerprint {
	customRulesMu.RLock()
	defer customRulesMu.RUnlock()
	out := make([]EnhancedFingerprint, len(customRules))
	copy(out, customRules)
	return out
}

// CustomEntries returns the parallel httpEntry slice for the matcher
// engine. / CustomEntries 返回 matcher 引擎用的并行 httpEntry slice。
func CustomEntries() []httpEntry {
	customRulesMu.RLock()
	defer customRulesMu.RUnlock()
	out := make([]httpEntry, len(pendingCustomEntries))
	copy(out, pendingCustomEntries)
	return out
}
