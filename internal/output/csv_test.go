// csv_test.go — round-trip tests for the CSV output sink.
//
// csv_test.go — CSV 输出 sink 的往返测试。
package output

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func TestSplitUserPass(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantPass string
	}{
		{"admin / hunter2", "admin", "hunter2"},
		{"root@host / p@ss w0rd", "root@host", "p@ss w0rd"},
		{"no-separator", "no-separator", ""},
		{"", "", ""},
		// Single-char inputs that can't contain " / " of length 3.
		// 长度不够 3 的输入无法含 " / "。
		{"a/b", "a/b", ""},
		{"a /b", "a /b", ""},
		{"a/ b", "a/ b", ""},
	}
	for _, c := range cases {
		gotU, gotP := splitUserPass(c.in)
		if gotU != c.wantUser || gotP != c.wantPass {
			t.Errorf("splitUserPass(%q) = (%q, %q), want (%q, %q)",
				c.in, gotU, gotP, c.wantUser, c.wantPass)
		}
	}
}

func TestTruncateForCSV(t *testing.T) {
	if got := truncateForCSV("hi", 5); got != "hi" {
		t.Errorf("truncateForCSV short: got %q", got)
	}
	if got := truncateForCSV("hello world", 5); got != "hello..." {
		t.Errorf("truncateForCSV long: got %q", got)
	}
}

func TestOpenOutput_WithCSV(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "fgqm_result.csv")

	out, err := OpenOutput(OutputConfig{
		ResultCSVPath: csvPath,
	})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	defer out.Close()

	if out.csv == nil {
		t.Fatalf("out.csv is nil after OpenOutput with ResultCSVPath set")
	}

	// Verify the file was created. Permission checks are Unix-only;
	// Windows file mode bits are not reliable in Go (the runtime
	// reports 0o666 for any readable file).
	//
	// 验证文件已创建。权限检查仅 Unix 有效；Windows 下 Go 运行时的
	// 文件 mode 位不可靠（任何可读文件都报告 0o666）。
	if _, err := os.Stat(csvPath); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if os.PathSeparator != '\\' {
		info, _ := os.Stat(csvPath)
		perm := info.Mode().Perm()
		if perm != 0o644 {
			t.Errorf("CSV file perm = %o, want 0o644", perm)
		}
	}
}

func TestWriteResult_CSVHeaderAndRow(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "fgqm_result.csv")
	out, err := OpenOutput(OutputConfig{ResultCSVPath: csvPath})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}

	// First result: must emit the header + 1 row.
	// 第一条 result：必须写出表头 + 1 行。
	r := &types.Result{
		Time:    time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC),
		Host:    "10.0.0.1",
		Port:    22,
		Service: "ssh",
		Plugin:  "ssh",
		Banner:  "OpenSSH_9.0",
		Cred: &types.Cred{
			User:     "root",
			Pass:     "toor",
			AuthType: "password",
		},
	}
	if err := out.WriteResult(r); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}

	// Second result: header must NOT be re-emitted.
	// 第二条 result：表头不应重复。
	r2 := &types.Result{
		Time:    time.Date(2026, 6, 19, 10, 0, 5, 0, time.UTC),
		Host:    "10.0.0.2",
		Port:    80,
		Service: "http",
		Plugin:  "http",
		Banner:  "nginx/1.25",
	}
	if err := out.WriteResult(r2); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Parse and assert. / 解析并断言。
	f, err := os.Open(csvPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (header + 2 results)", len(rows))
	}

	// Header check. / 表头检查。
	wantHeader := strings.Join(csvHeader, ",")
	if strings.Join(rows[0], ",") != wantHeader {
		t.Errorf("header row = %v, want %s", rows[0], wantHeader)
	}

	// First data row must contain a redacted user fingerprint and a
	// redacted password (since ShowCleartext=false by default).
	// RedactUser("root") == "r**t" (keep first/last, mask the middle).
	//
	// 第一行数据应包含脱敏的用户指纹和脱敏密码（默认 ShowCleartext=false）。
	// RedactUser("root") == "r**t"（保头尾，中间遮蔽）。
	if !contains(rows[1], "10.0.0.1") || !contains(rows[1], "22") {
		t.Errorf("row 1 missing host/port: %v", rows[1])
	}
	if !contains(rows[1], "r**t") {
		t.Errorf("row 1 missing redacted user 'r**t': %v", rows[1])
	}
	if contains(rows[1], "toor") {
		t.Errorf("row 1 leaked cleartext password 'toor': %v", rows[1])
	}

	// Second data row should have empty user/pass. / 第二行 user/pass 为空。
	if rows[2][7] != "" || rows[2][8] != "" {
		t.Errorf("row 2 user/pass not empty: user=%q pass=%q", rows[2][7], rows[2][8])
	}
}

func TestWriteResult_CSVCleartext(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "fgqm_result.csv")
	out, err := OpenOutput(OutputConfig{ResultCSVPath: csvPath, ShowCleartext: true})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	r := &types.Result{
		Time: time.Now(), Host: "10.0.0.3", Port: 3306,
		Service: "mysql", Plugin: "mysql", Banner: "5.7",
		Cred: &types.Cred{User: "root", Pass: "realpassword", AuthType: "password"},
	}
	if err := out.WriteResult(r); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, _ := os.ReadFile(csvPath)
	if !bytes.Contains(data, []byte("realpassword")) {
		t.Errorf("ShowCleartext=true but cleartext password not in CSV: %s", data)
	}
}

func TestWriteResult_CSVOmittedWhenPathEmpty(t *testing.T) {
	// No ResultCSVPath → out.csv is nil → no CSV file created, no panic.
	// ResultCSVPath 为空 → out.csv 为 nil → 不创建 CSV 文件,无 panic。
	out, err := OpenOutput(OutputConfig{}) // everything empty
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	r := &types.Result{Host: "1.2.3.4", Port: 80, Service: "http"}
	if err := out.WriteResult(r); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	_ = out.Close()
}

// contains is a tiny helper to keep the assertions readable.
// contains 是让断言更可读的小工具。
func contains(row []string, sub string) bool {
	for _, cell := range row {
		if strings.Contains(cell, sub) {
			return true
		}
	}
	return false
}
