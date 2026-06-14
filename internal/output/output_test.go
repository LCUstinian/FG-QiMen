// output_test.go — focused unit tests for the multi-format result
// sink in internal/output.
//
// Verifies the contract callers rely on:
//   - OpenOutput creates parent directories (and is robust to
//     a partially-filled OutputConfig — empty paths are skipped)
//   - WriteResult writes a single line per result to the TXT sink
//     and a single JSON object per result to the NDJSON sink
//   - WriteCred is a no-op when r.Cred is nil
//   - WriteRDP emits both a structured NDJSON line and a
//     human-readable text line
//   - Flush + Close are safe to call in any order
//
// No test exercises concurrent writes — Output is documented as
// internally mutex-serialised, and the production caller
// (core.RunScan) is the only writer. Add a concurrency test when
// the v0.3+ scheduler actually fans out multiple writers.
//
// output_test.go — internal/output 多格式结果汇的单测。
package output

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func sampleResult() *types.Result {
	return &types.Result{
		Time:    time.Date(2026, 6, 14, 1, 23, 45, 0, time.UTC),
		Host:    "192.168.1.10",
		Port:    22,
		Service: "ssh",
		Banner:  "OpenSSH 9.6",
	}
}

func sampleResultWithCred() *types.Result {
	r := sampleResult()
	r.Cred = &types.Cred{User: "admin", Pass: "root"}
	return r
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		out = append(out, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

// TestOpenOutputAllEmptyPaths: a config with every path empty is
// legal — OpenOutput returns a valid *Output with all flushCloser
// slots nil. The contract is "open what is configured, skip what is
// not"; this test guards the skip path.
func TestOpenOutputAllEmptyPaths(t *testing.T) {
	o, err := OpenOutput(OutputConfig{})
	if err != nil {
		t.Fatalf("OpenOutput(empty): %v", err)
	}
	if o == nil {
		t.Fatal("OpenOutput returned nil *Output")
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestOpenOutputCreatesParentDirs: callers commonly pass just a
// file name and rely on OpenOutput to mkdir -p. Verified by passing
// a path with a missing intermediate directory and confirming the
// file lands at the right place.
func TestOpenOutputCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "nested", "results")
	cfg := OutputConfig{
		ResultTXTPath: filepath.Join(resultsDir, "result.txt"),
	}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.WriteResult(sampleResult()); err != nil {
		t.Errorf("WriteResult: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(cfg.ResultTXTPath); err != nil {
		t.Errorf("expected file at %s; stat err = %v", cfg.ResultTXTPath, err)
	}
}

// TestWriteResultTXT: the human-readable line is single-line, contains
// host/port/service/banner, and is appended (i.e. a second Write
// produces two lines).
func TestWriteResultTXT(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{ResultTXTPath: filepath.Join(dir, "r.txt")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.WriteResult(sampleResult()); err != nil {
		t.Errorf("WriteResult 1: %v", err)
	}
	if err := o.WriteResult(sampleResult()); err != nil {
		t.Errorf("WriteResult 2: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	lines := readLines(t, cfg.ResultTXTPath)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, "192.168.1.10:22") {
			t.Errorf("line %d missing host:port: %q", i, line)
		}
		if !strings.Contains(line, "[ssh]") {
			t.Errorf("line %d missing [ssh] tag: %q", i, line)
		}
		if !strings.Contains(line, "OpenSSH 9.6") {
			t.Errorf("line %d missing banner: %q", i, line)
		}
	}
}

// TestWriteResultJSON: the NDJSON sink contains one JSON object per
// line, and the object round-trips back to the input Result.
func TestWriteResultJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{ResultJSONPath: filepath.Join(dir, "r.json")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	want := sampleResult()
	if err := o.WriteResult(want); err != nil {
		t.Errorf("WriteResult: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	lines := readLines(t, cfg.ResultJSONPath)
	if len(lines) != 1 {
		t.Fatalf("got %d JSON lines, want 1", len(lines))
	}
	var got types.Result
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal: %v\nline=%q", err, lines[0])
	}
	if got.Host != want.Host || got.Port != want.Port || got.Service != want.Service || got.Banner != want.Banner {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestWriteCredSkipsWhenNil: a result without Cred must not produce
// a line in creds.txt. The file itself IS created at OpenOutput time
// (the contract is "open what is configured, skip what is not" —
// opening an empty sink up front is the documented behaviour), but
// it must remain empty when no cred hits arrive.
func TestWriteCredSkipsWhenNil(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{CredsPath: filepath.Join(dir, "creds.txt")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.WriteCred(sampleResult()); err != nil {
		t.Errorf("WriteCred(nil cred): %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	lines := readLines(t, cfg.CredsPath)
	if len(lines) != 0 {
		t.Errorf("creds.txt should be empty when no cred hits; got %d lines: %v", len(lines), lines)
	}
}

// TestWriteCredAppendsLine: a result with Cred writes one line in the
// documented format "host:port  service  user / pass  timestamp".
// (No [brackets] around the service tag — creds.txt is for grep, not
// for pretty-printing.)
func TestWriteCredAppendsLine(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{CredsPath: filepath.Join(dir, "creds.txt")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.WriteCred(sampleResultWithCred()); err != nil {
		t.Errorf("WriteCred: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	lines := readLines(t, cfg.CredsPath)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	want := []string{"192.168.1.10:22", "ssh", "admin", "root"}
	for _, w := range want {
		if !strings.Contains(lines[0], w) {
			t.Errorf("creds line missing %q: %q", w, lines[0])
		}
	}
}

// TestWriteRDPEmitsBoth: a single WriteRDP call produces one
// NDJSON line in rdp.json AND one human-readable line in rdp.txt.
func TestWriteRDPEmitsBoth(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{
		RDPJSONPath: filepath.Join(dir, "rdp.json"),
		RDPTXTPath:  filepath.Join(dir, "rdp.txt"),
	}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	fp := RDPFingerprint{
		Host:             "10.0.0.5",
		Port:             3389,
		ServerName:       "WIN-SRV01",
		Domain:           "corp",
		DomainJoined:     true,
		OSVersion:        "10.0.19045",
		OSBuild:          "19045",
		NLASupported:     true,
		CredSSPSupported: true,
		ProtocolVersion:  0x00080004,
		ScanTime:         time.Date(2026, 6, 14, 1, 0, 0, 0, time.UTC),
	}
	if err := o.WriteRDP(fp); err != nil {
		t.Errorf("WriteRDP: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	jsonLines := readLines(t, cfg.RDPJSONPath)
	if len(jsonLines) != 1 {
		t.Fatalf("got %d rdp.json lines, want 1", len(jsonLines))
	}
	var got RDPFingerprint
	if err := json.Unmarshal([]byte(jsonLines[0]), &got); err != nil {
		t.Fatalf("unmarshal rdp.json: %v", err)
	}
	if got.Host != fp.Host || got.ServerName != fp.ServerName || got.Domain != fp.Domain {
		t.Errorf("rdp.json round-trip: got %+v, want %+v", got, fp)
	}

	txtLines := readLines(t, cfg.RDPTXTPath)
	if len(txtLines) != 1 {
		t.Fatalf("got %d rdp.txt lines, want 1", len(txtLines))
	}
	for _, w := range []string{"10.0.0.5:3389", "WIN-SRV01", "corp", "10.0.19045"} {
		if !strings.Contains(txtLines[0], w) {
			t.Errorf("rdp.txt line missing %q: %q", w, txtLines[0])
		}
	}
}

// TestFlushForceWriteBeforeClose: Flush must push the buffer to
// disk so a reader can see results without waiting for Close. This
// matters for the long-running scan: -v logging in another goroutine
// would otherwise block on the buffer until scan exit.
func TestFlushForceWriteBeforeClose(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{ResultTXTPath: filepath.Join(dir, "r.txt")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.WriteResult(sampleResult()); err != nil {
		t.Errorf("WriteResult: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
	// Now read the file WITHOUT calling Close first — Flush must have
	// made the line visible.
	lines := readLines(t, cfg.ResultTXTPath)
	if len(lines) != 1 {
		t.Errorf("got %d lines after Flush, want 1", len(lines))
	}
	if err := o.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestCloseIdempotent: calling Close twice must not panic and must
// return the same (or both-nil) error — a redundant close after
// graceful shutdown should be a no-op.
func TestCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := OutputConfig{ResultTXTPath: filepath.Join(dir, "r.txt")}
	o, err := OpenOutput(cfg)
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second Close may return an error (e.g. file already closed) but
	// must not panic. We only assert "no panic".
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("second Close panicked: %v", r)
		}
	}()
	_ = o.Close()
}
