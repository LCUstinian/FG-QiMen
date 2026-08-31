// export_test.go — round-trip tests for the .fgq format.
//
// export_test.go — .fgq 格式的往返测试。
package workspace

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// TestExportImportRoundTrip is the canonical happy-path: export
// a project, then import the resulting .fgq file as a new
// project, then check the imported bbolt has the required
// buckets. / TestExportImportRoundTrip 是规范的正向路径：导出
// 一个项目，把生成的 .fgq 文件作为新项目导入，然后检查导入的
// bbolt 有所需的 bucket。
func TestExportImportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Create the source project. / 创建源项目。
	src, err := Open("source")
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	// Write one entry into the targets bucket so the file is
	// non-empty (otherwise the round-trip is trivial).
	// / 在 targets bucket 写一条记录让文件非空（否则往返平凡）。
	if err := src.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("targets"))
		return b.Put([]byte("192.168.1.1"), []byte("ok"))
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	// Export. / Export。
	outPath := filepath.Join(dir, "source.fgq")
	if err := src.Export(outPath); err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Source is now reopened by Export's deferred reopen; close
	// it before importing. / Export 已用 deferred reopen 重开了
	// source；import 前关掉。
	_ = src.Close()
	// Validate the file header by hand. / 手工验证文件头。
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read %s: %v", outPath, err)
	}
	if !bytes.HasPrefix(data, fgqMagic[:]) {
		t.Fatalf("file missing FGQ1 magic; got %q", data[:4])
	}
	headerLen := binary.LittleEndian.Uint32(data[4:8])
	if int(headerLen) > len(data)-8 {
		t.Fatalf("header length %d exceeds file size", headerLen)
	}
	// Import under a new name. / 用新名字 import。
	if err := Import(outPath, "imported"); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// Open and verify the bucket exists with the seed entry.
	// / 打开并验证 bucket 存在且含种子条目。
	dst, err := Open("imported")
	if err != nil {
		t.Fatalf("Open imported: %v", err)
	}
	defer func() { _ = dst.Close() }()
	var got string
	if err := dst.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("targets"))
		if b == nil {
			return errNoBucketForTest
		}
		got = string(b.Get([]byte("192.168.1.1")))
		return nil
	}); err != nil {
		t.Fatalf("verify imported: %v", err)
	}
	if got != "ok" {
		t.Errorf("imported value = %q, want %q", got, "ok")
	}
}

// TestExportRejectsEphemeral documents the no-DB error path:
// an ephemeral (--no-state) project has DB=nil and Export
// refuses. / TestExportRejectsEphemeral 文档无 DB 错误路径：
// ephemeral（--no-state）项目 DB=nil，Export 拒绝。
func TestExportRejectsEphemeral(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p, err := OpenWithOptions("ephem", OpenOptions{NoState: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = p.Close() }()
	err = p.Export(filepath.Join(dir, "out.fgq"))
	if err == nil {
		t.Fatal("Export on ephemeral project should fail; got nil")
	}
}

// TestImportRejectsBadMagic documents the format-validation
// step: a file that does not start with the FGQ1 magic is
// rejected before any bytes are written. /
// TestImportRejectsBadMagic 文档格式验证步骤：不以 FGQ1 magic
// 开头的文件在任何字节写入前就被拒绝。
func TestImportRejectsBadMagic(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	bad := filepath.Join(dir, "bad.fgq")
	if err := os.WriteFile(bad, []byte("NOPE!"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := Import(bad, "x"); err == nil {
		t.Fatal("Import of bad-magic file should fail; got nil")
	}
}

// TestExportHeaderJSONRoundTrip locks the JSON shape: export
// a fresh project, parse the header, and confirm the documented
// fields are present. / TestExportHeaderJSONRoundTrip 钉死 JSON
// 形态：导出新项目，parse header，确认文档化字段存在。
func TestExportHeaderJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p, err := Open("hdrtest")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	out := filepath.Join(dir, "hdr.fgq")
	if err := p.Export(out); err != nil {
		t.Fatalf("Export: %v", err)
	}
	_ = p.Close()
	raw, _ := os.ReadFile(out)
	headerLen := binary.LittleEndian.Uint32(raw[4:8])
	hdr := raw[8 : 8+headerLen]
	// We expect the documented fields to appear in the JSON.
	// / 期望文档化字段出现在 JSON 里。
	for _, needle := range []string{`"version"`, `"project"`, `"created_at"`, `"db_bytes"`} {
		if !bytes.Contains(hdr, []byte(needle)) {
			t.Errorf("header missing %q: %s", needle, hdr)
		}
	}
	_ = time.Now() // keep the time import honest
}

// errNoBucketForTest is used by the round-trip test to assert
// the targets bucket survived the import. / errNoBucketForTest
// 供 round-trip 测试用，确认 targets bucket 在 import 后还存在。
var errNoBucketForTest = errors.New("workspace_test: targets bucket missing after import")
