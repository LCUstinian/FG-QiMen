// csv.go — CSV output helpers (header definition, format helpers).
//
// csv.go — CSV 输出辅助函数（表头定义、格式辅助函数）.
//
// The actual writing path is in output.go's writeCSVvia (called from
// WriteResult). This file holds the shared column definition and small
// string utilities used by both the production path and the tests.
//
// 实际写入路径在 output.go 的 writeCSVvia（由 WriteResult 调用）。
// 本文件持有生产路径和测试共用的列定义与字符串工具函数。
package output

// csvHeader is the column order for results.csv. Keep stable — downstream
// scripts (Excel pivots, AWK, pandas) rely on column position not name.
//
// csvHeader 是 results.csv 的列顺序。保持稳定——下游脚本（Excel 数据透视表、
// AWK、pandas）依赖列位置而非列名。
var csvHeader = []string{
	"time",
	"host",
	"port",
	"service",
	"plugin",
	"state",
	"banner",
	"user",
	"pass",
}

// splitUserPass splits "user / pass" (the format from ShowUserPassword)
// into two columns. If the input doesn't contain " / ", returns it as-is
// in user and "" in pass.
//
// splitUserPass 把 "user / pass"（ShowUserPassword 的格式）拆成两列。
// 若输入不含 " / "，整段放 user，pass 返回空。
func splitUserPass(s string) (user, pass string) {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == ' ' && s[i+1] == '/' && s[i+2] == ' ' {
			return s[:i], s[i+3:]
		}
	}
	return s, ""
}

// truncateForCSV limits a banner to a sane CSV row size. Banners above
// ~1KB are usually protocol noise; truncating keeps Excel / LibreOffice
// from becoming slow on huge cells.
//
// truncateForCSV 把 banner 限制在合理大小。>1KB 的 banner 通常是协议噪音;
// 截断以防 Excel/LibreOffice 在大单元格上变慢。
func truncateForCSV(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "..."
}
