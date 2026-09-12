// custom.go — load a user-supplied ruleset (file or URL) and
// merge it with the built-in (FingerprintHub + hardcoded) rules.
// Phase D (audit roadmap): operators can drop in EHole-format
// JSON files. Phase 1.6: the source can also be an HTTP(S) URL
// for live-update.
//
// custom.go — 加载用户提供的规则集（文件或 URL）并与内置
// （FingerprintHub + 硬编码）规则合并。Phase D（审计路线图）：
// 操作员可投喂 EHole 格式 JSON 文件。Phase 1.6：源也可以是
// HTTP(S) URL 以支持 live-update。
//
// Supported source paths:
//   - Local file (any path) — read with os.ReadFile
//   - HTTP(S) URL (http:// or https:// prefix) — fetch with
//     http.Get, 5s timeout
//
// Supported file formats (auto-detected by JSON shape):
//   1. "rules" key (FG-QiMen native) — flat list of rules.
//   2. "cms" key (EHole format) — each entry has "name" + "method"
//      + "keyword" / "regex" / "md5".

package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// sourceTimeout caps a single ruleset fetch. / sourceTimeout 是单
// 次规则集获取的超时。
const sourceTimeout = 10 * time.Second

// Load guards (audit M-1, revised). Go's regexp is RE2-style: match
// time is linear in the input — catastrophic backtracking does not
// exist, so "evil regex ReDoS" is not a Go threat. What IS real:
// a huge (or hostile) ruleset makes every target's matching pass
// O(rules × body), so we cap both the raw bytes and the rule count
// and fail loudly instead of silently degrading a /24 scan.
// / 加载护栏（审计 M-1，修订）。Go regexp 是 RE2 语义：匹配时间与
// 输入线性——不存在灾难性回溯，"恶意正则 ReDoS"在 Go 上不成立。
// 真实风险是：巨大（或恶意）规则集让每个目标的匹配趟变成
// O(规则数 × body)，因此对原始字节数与规则条数都设上限，超限
// 大声报错，而不是悄悄拖慢整个 /24 扫描。
const (
	maxRulesetBytes = 16 << 20 // 16 MiB, same cap as fetchURL / 与 fetchURL 相同上限
	maxCustomRules  = 10000    // rules; EHole's public CMS list is ~600 / 规则条数；EHole 公开库约 600 条
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

// LoadCustomRuleset reads a JSON file or fetches a URL, auto-detects
// its format, and registers the rules. Returns the number of rules
// added. Phase 1.6: source can be http:// or https://.
// / LoadCustomRuleset 读 JSON 文件或抓 URL，自动检测格式，
// 注册规则。返回新加规则数。Phase 1.6：源可以是 http:// 或
// https://。
func LoadCustomRuleset(path string) (int, error) {
	data, err := loadRulesetSource(path)
	if err != nil {
		return 0, err
	}
	// Uniform size cap for both sources: a URL is capped inside
	// fetchURL, but a local file (os.ReadFile) is not — a misplaced
	// multi-GB dump should fail here, not malloc. /
	// 两个来源统一上限：URL 在 fetchURL 内已限，本地文件
	//（os.ReadFile）没有——误指的几 GB 转储应在这里失败，而不是
	// 先吃进内存。
	if len(data) > maxRulesetBytes {
		return 0, fmt.Errorf("ruleset %s is %d bytes (cap %d) — refusing to load", path, len(data), maxRulesetBytes)
	}
	return parseAndRegister(data)
}

// loadRulesetSource fetches the ruleset bytes from either a local
// file or an HTTP(S) URL. / loadRulesetSource 从本地文件或
// HTTP(S) URL 取规则集字节。
func loadRulesetSource(path string) ([]byte, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return fetchURL(path)
	}
	return os.ReadFile(path)
}

// fetchURL retrieves a URL with a 10s timeout. / fetchURL 用
// 10s 超时取 URL。
func fetchURL(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "fg-qimen/0.3.1")
	client := &http.Client{Timeout: sourceTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	// 16 MiB cap to prevent a malicious server from OOMing us.
	// / 16 MiB 上限防恶意 server OOM。
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
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
	if len(wrap.Rules) > maxCustomRules {
		return 0, fmt.Errorf("ruleset declares %d rules (cap %d) — refusing to load", len(wrap.Rules), maxCustomRules)
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
	if len(wrap.CMS) > maxCustomRules {
		return 0, fmt.Errorf("ruleset declares %d rules (cap %d) — refusing to load", len(wrap.CMS), maxCustomRules)
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
