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

// csvFormulaPrefixes are the characters Excel / LibreOffice / Numbers
// interpret as formula starts when a cell begins with one. A banner is
// attacker-controlled data (remote title, Server header, fingerprint
// names), so `=cmd|'/c calc'!A0` or `=HYPERLINK(...)` planted by a
// hostile target would execute when the operator opens results.csv in
// a spreadsheet (OWASP CSV Injection). Neutralize by prefixing a
// single quote — the standard, spreadsheet-accepted escape that keeps
// the visible text unchanged.
//
// csvFormulaPrefixes 是 Excel / LibreOffice / Numbers 视为公式起点的字符。
// banner 是攻击者可控数据（远程 title、Server 头、指纹名），恶意目标
// 埋入的 `=cmd|'/c calc'!A0` 或 `=HYPERLINK(...)` 会在操作员用表格软件
// 打开 results.csv 时执行（OWASP CSV 注入）。按标准做法前置一个单引号
// 中和——表格软件接受的转义方式，且显示文本不变。
var csvFormulaPrefixes = []byte{'=', '+', '-', '@', '\t', '\r'}

// neutralizeCSVFormula guards one CSV cell against spreadsheet formula
// injection. Only apply to attacker-controlled columns (banner) — never
// to user/pass (operator wordlist data must survive copy-paste verbatim).
//
// neutralizeCSVFormula 防护单个 CSV 单元格的表格公式注入。只对攻击者
// 可控的列（banner）使用——绝不用于 user/pass（操作员词表数据必须
// 原样可复制）。
func neutralizeCSVFormula(s string) string {
	if s == "" {
		return s
	}
	for _, c := range csvFormulaPrefixes {
		if s[0] == c {
			return "'" + s
		}
	}
	return s
}
