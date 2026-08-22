// sarif_test.go — SARIF output tests.
// sarif_test.go — SARIF 输出测试。
package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestSARIFDocumentSchema: the document we emit conforms to the
// SARIF 2.1.0 shape (top-level $schema / version / runs[]).
// / 测试文档符合 SARIF 2.1.0 结构（顶层 $schema / version / runs[]）。
func TestSARIFDocumentSchema(t *testing.T) {
	results := []*types.Result{
		{Host: "10.0.0.1", Port: 22, Service: "ssh", Plugin: "ssh", Banner: "OpenSSH 8.9"},
		{Host: "10.0.0.1", Port: 22, Service: "ssh", Plugin: "ssh",
			Cred: &types.Cred{User: "admin", Pass: "admin", AuthType: "password"}},
	}
	doc := buildSARIFDocument(results)
	// Re-marshal and parse as generic map to validate JSON shape.
	// / 重序列化再 parse 成通用 map 以校验 JSON 形状。
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := probe["$schema"]; got != "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/2.1.0/manifest/sarif-schema-2.1.0.json" {
		t.Errorf("$schema = %v, want canonical SARIF 2.1.0 URL", got)
	}
	if got := probe["version"]; got != "2.1.0" {
		t.Errorf("version = %v, want 2.1.0", got)
	}
	runs, ok := probe["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("runs = %v, want one run", probe["runs"])
	}
}

// TestSARIFResultCredIsError: a credential hit renders as SARIF
// level "error". / 凭据命中渲染为 SARIF level "error"。
func TestSARIFResultCredIsError(t *testing.T) {
	r := &types.Result{
		Host: "10.0.0.1", Port: 22, Service: "ssh",
		Cred: &types.Cred{User: "admin", Pass: "admin"},
	}
	if got := fgqimenSarifLevel(r); got != "error" {
		t.Errorf("level = %q, want error", got)
	}
}

// TestSARIFResultPlainIsNote: a plain service-detect result renders
// as "note". / 普通服务探测渲染为 "note"。
func TestSARIFResultPlainIsNote(t *testing.T) {
	r := &types.Result{Host: "10.0.0.1", Port: 80, Service: "http"}
	if got := fgqimenSarifLevel(r); got != "note" {
		t.Errorf("level = %q, want note", got)
	}
}

// TestSARIFRuleID: ruleId includes the plugin name. / ruleId 含插件名。
func TestSARIFRuleID(t *testing.T) {
	r := &types.Result{Host: "x", Port: 22, Service: "ssh", Plugin: "ssh"}
	if got := fgqimenSarifRule(r.Plugin); got != "fgqimen/ssh" {
		t.Errorf("ruleId = %q, want fgqimen/ssh", got)
	}
}

// TestSARIFURIShape: the URI follows service://host:port. / URI 形
// 式为 service://host:port。
func TestSARIFURIShape(t *testing.T) {
	r := &types.Result{Host: "10.0.0.1", Port: 5432, Service: "postgresql"}
	got := fgqimenSarifURI(r)
	if !strings.HasPrefix(got, "postgresql://") {
		t.Errorf("URI = %q, want postgresql:// prefix", got)
	}
}

// TestSARIFOpenAndClose: opening SARIF output and closing it produces
// a valid JSON file at the configured path. / 打开 SARIF 输出并关
// 闭在指定路径生成合法 JSON 文件。
func TestSARIFOpenAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	out, err := OpenOutput(OutputConfig{ResultSARIFPath: path})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	now := time.Now()
	r := &types.Result{
		Host: "10.0.0.1", Port: 22, Service: "ssh", Plugin: "ssh",
		Banner: "OpenSSH 8.9", Time: now,
		Cred: &types.Cred{User: "admin", Pass: "admin"},
	}
	if err := out.WriteResult(r); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sarif: %v", err)
	}
	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse sarif: %v", err)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", doc.Version)
	}
	if len(doc.Runs) != 1 || len(doc.Runs[0].Results) != 1 {
		t.Errorf("runs[0].results length = %d, want 1", len(doc.Runs[0].Results))
	}
	if doc.Runs[0].Results[0].RuleID != "fgqimen/ssh" {
		t.Errorf("ruleId = %q, want fgqimen/ssh", doc.Runs[0].Results[0].RuleID)
	}
	if doc.Runs[0].Results[0].Level != "error" {
		t.Errorf("level = %q, want error", doc.Runs[0].Results[0].Level)
	}
}

// TestSARIFEmptyResult: closing an empty Output still produces a
// valid SARIF document with zero results. / 关闭空 Output 仍生成有
// 零个 results 的合法 SARIF 文档。
func TestSARIFEmptyResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	out, err := OpenOutput(OutputConfig{ResultSARIFPath: path})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Runs) != 1 || len(doc.Runs[0].Results) != 0 {
		t.Errorf("empty scan should produce empty results, got %v", doc.Runs[0].Results)
	}
}

// TestSARIFConcurrentWrites: parallel WriteResult calls all land in
// the same document. / 并行 WriteResult 全部落入同一文档。
func TestSARIFConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	out, err := OpenOutput(OutputConfig{ResultSARIFPath: path})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	const n = 50
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			out.WriteResult(&types.Result{
				Host: "10.0.0.1", Port: 22 + i, Service: "ssh", Plugin: "ssh",
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Runs[0].Results) != n {
		t.Errorf("results length = %d, want %d", len(doc.Runs[0].Results), n)
	}
}