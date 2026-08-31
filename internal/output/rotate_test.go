// rotate_test.go — unit tests for the rotation logic.
//
// rotate_test.go — 轮转逻辑的单元测试。
package output

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRotatingWriter_NoRotationWhenUnderCap documents the
// no-rotation case: writes < maxBytes, no .1 file appears.
// / TestRotatingWriter_NoRotationWhenUnderCap 文档无轮转情况：
// 写 < maxBytes 时不会出 .1 文件。
func TestRotatingWriter_NoRotationWhenUnderCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	rw, err := newRotatingWriter(path, 0o644, 100, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := rw.Write([]byte("hello\n")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	// Close flushes the buffer; needed before ReadFile sees
	// the data. / Close 刷新 buffer；ReadFile 看到数据前必须
	// 先 Close。
	if err := rw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// .1 should not exist. / .1 不应存在。
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("expected no .1 file, but Stat returned %v", err)
	}
	// Active file should have all the data. / active 文件应含所有数据。
	data, _ := os.ReadFile(path)
	if got, want := string(data), "hello\nhello\nhello\nhello\nhello\n"; got != want {
		t.Errorf("file content = %q, want %q", got, want)
	}
}

// TestRotatingWriter_RotatesAtCap documents the happy-path
// rotation: write 5x cap bytes, expect .1 to exist and
// contain the earliest batch, active file to contain the
// latest. / TestRotatingWriter_RotatesAtCap 文档正常轮转：写
// 5 倍 cap 字节，期待 .1 含最早批次，active 含最新批次。
func TestRotatingWriter_RotatesAtCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	// 10-byte cap, 3 files (active + .1 + .2). / 10 字节 cap，
	// 3 个文件（active + .1 + .2）。
	rw, err := newRotatingWriter(path, 0o644, 10, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		// Each Write is 8 bytes; 8 < 10 so the first Write
		// doesn't trigger rotation. The 2nd write pushes the
		// total to 16, crossing the cap. / 每次 Write 8 字节；
		// 8 < 10 第一次 Write 不轮转。第二次写把总数推到 16，
		// 跨过 cap。
		_, err := rw.Write([]byte("AAAAAAAA"))
		if err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	_ = rw.Close()
	// .1 should exist and contain at least one "AAAAAAAA" batch
	// (the earliest, since each rotation shifts older content
	// up). / .1 应存在且至少含一个 "AAAAAAAA" 批次（最旧的，
	// 每次轮转把旧内容上移）。
	if data, err := os.ReadFile(path + ".1"); err != nil {
		t.Fatalf("read .1: %v", err)
	} else if !strings.Contains(string(data), "AAAAAAAA") {
		t.Errorf(".1 content = %q, want to contain 'AAAAAAAA'", data)
	}
	// Active file should exist and have the last batch. / active
	// 文件应存在并含最后批次。
	if data, err := os.ReadFile(path); err != nil {
		t.Fatalf("read active: %v", err)
	} else if !strings.Contains(string(data), "AAAAAAAA") {
		t.Errorf("active content = %q, want to contain 'AAAAAAAA'", data)
	}
}

// TestRotatingWriter_DropsOldestBeyondCap documents the cap:
// with maxFiles=2, only one rotated (.1) file is kept. The
// active file is always the "0th" file (no .0). /
// TestRotatingWriter_DropsOldestBeyondCap 文档上限：maxFiles=2
// 时只保留一个轮转文件（.1）。active 文件永远是"0号"（无 .0）。
func TestRotatingWriter_DropsOldestBeyondCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	rw, err := newRotatingWriter(path, 0o644, 4, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	// 5 writes of 5 bytes each → crosses 4-byte cap 4 times.
	// / 5 次 5 字节 Write → 跨过 4 字节 cap 4 次。
	for i := 0; i < 5; i++ {
		if _, err := rw.Write([]byte(strings.Repeat("a", 5))); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	_ = rw.Close()
	// .1 exists, .2 does not (only 2 files total: active + .1).
	// / .1 存在，.2 不存在（总共只 2 个文件：active + .1）。
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf(".1 should exist: %v", err)
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Errorf(".2 should not exist: %v", err)
	}
}

// TestRotatingWriter_ZeroMaxBytesNoRotation documents the
// explicit no-rotation case (maxBytes=0). /
// TestRotatingWriter_ZeroMaxBytesNoRotation 文档显式无轮转
// (maxBytes=0)。
func TestRotatingWriter_ZeroMaxBytesNoRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	rw, err := newRotatingWriter(path, 0o644, 0, 0)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer func() { _ = rw.Close() }()
	// Write a lot, no rotation should happen. / 写很多，不应轮转。
	for i := 0; i < 100; i++ {
		if _, err := rw.Write([]byte(strconv.Itoa(i) + "\n")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	// No .1 file. / 没有 .1 文件。
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf(".1 should not exist in no-rotation mode: %v", err)
	}
}
