// sarif.go — SARIF output sink (v0.4).
// sarif.go — SARIF 输出汇（v0.4）。
//
// SARIF (Static Analysis Results Interchange Format) is the JSON
// schema GitHub Code Scanning ingests natively. / SARIF
// （静态分析结果交换格式）是 GitHub Code Scanning 原生摄取
// 的 JSON schema。
//
// Strategy: collect every Result into an in-memory buffer via
// WriteResult, then emit the assembled SARIF document once in
// Close(). This is the natural shape for SARIF — one run, many
// results inside it. / 策略：通过 WriteResult 把每个 Result 收
// 集到内存 buffer，然后在 Close() 一次性输出组装好的 SARIF 文
// 档。这是 SARIF 的天然形态——一个 run，内含多个 results。
//
// Schema target: SARIF 2.1.0. / Schema 目标：SARIF 2.1.0。
package output

import (
	"encoding/json"
	"fmt"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// sarifDocument is the top-level SARIF 2.1.0 object. / sarifDocument
// 是顶层 SARIF 2.1.0 对象。
type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

// sarifRun is a single tool invocation's findings. / sarifRun 是单
// 次工具调用的发现集。
type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

// sarifTool identifies the scanner. / sarifTool 标识扫描器。
type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

// sarifDriver is the tool's name + version. / sarifDriver 是工具名 +
// 版本。
type sarifDriver struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	InformationURI string `json:"informationUri,omitempty"`
}

// sarifResult is one finding. / sarifResult 是单个发现。
type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Locations []sarifLocation `json:"locations"`
	Message   sarifMessage    `json:"message"`
}

// sarifLocation pins a finding to a host:port. / sarifLocation 把发
// 现定位到 host:port。
type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

// sarifPhysicalLocation describes where the finding was observed.
// / sarifPhysicalLocation 描述发现的观察位置。
type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

// sarifArtifactLocation is the URI-like identifier for the target.
// / sarifArtifactLocation 是目标的 URI 样式标识符。
type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// sarifMessage is the human-readable description. / sarifMessage 是
// 人类可读描述。
type sarifMessage struct {
	Text string `json:"text"`
}

// fgqimenSarifTool is the Tool block used in every emitted document.
// / fgqimenSarifTool 是每次输出文档使用的 Tool 块。
var fgqimenSarifTool = sarifTool{
	Driver: sarifDriver{
		Name:           "FG-QiMen",
		Version:        "v0.4",
		InformationURI: "https://github.com/LCUstinian/FG-QiMen",
	},
}

// fgqimenSarifRule maps plugin name → SARIF rule ID. / fgqimenSarifRule
// 把插件名映射到 SARIF 规则 ID。
func fgqimenSarifRule(plugin string) string {
	if plugin == "" {
		return "fgqimen/service-detected"
	}
	return "fgqimen/" + plugin
}

// fgqimenSarifLevel maps a hit/miss to SARIF severity. / fgqimenSarifLevel
// 把 hit/miss 映射到 SARIF 严重级。
func fgqimenSarifLevel(r *types.Result) string {
	if r.Cred != nil {
		return "error" // credential hit = actionable
	}
	return "note"
}

// fgqimenSarifMessage builds a single-line description. /
// fgqimenSarifMessage 构造单行描述。
func fgqimenSarifMessage(r *types.Result) string {
	if r.Cred != nil {
		return fmt.Sprintf("credential hit at %s:%d (%s) user=%q",
			r.Host, r.Port, r.Service, r.Cred.User)
	}
	return fmt.Sprintf("service detected at %s:%d (%s) banner=%q",
		r.Host, r.Port, r.Service, truncateForCSV(r.Banner, 200))
}

// fgqimenSarifURI is the stable per-target identifier. /
// fgqimenSarifURI 是每个目标的稳定标识符。
func fgqimenSarifURI(r *types.Result) string {
	return fmt.Sprintf("%s://%s:%d", r.Service, r.Host, r.Port)
}

// buildSARIFDocument assembles the final document from a slice of
// results. / buildSARIFDocument 从结果 slice 组装最终文档。
func buildSARIFDocument(results []*types.Result) sarifDocument {
	doc := sarifDocument{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/2.1.0/manifest/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				Tool:    fgqimenSarifTool,
				Results: make([]sarifResult, 0, len(results)),
			},
		},
	}
	for _, r := range results {
		if r == nil {
			continue
		}
		doc.Runs[0].Results = append(doc.Runs[0].Results, sarifResult{
			RuleID: fgqimenSarifRule(r.Plugin),
			Level:  fgqimenSarifLevel(r),
			Locations: []sarifLocation{
				{PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: fgqimenSarifURI(r)},
				}},
			},
			Message: sarifMessage{Text: fgqimenSarifMessage(r)},
		})
	}
	return doc
}

// writeSARIFDocument renders the document as pretty JSON and writes
// it to w. / writeSARIFDocument 渲染文档为格式化 JSON 并写入 w。
func writeSARIFDocument(w *flushCloser, results []*types.Result) error {
	doc := buildSARIFDocument(results)
	enc := json.NewEncoder(w.bw())
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
