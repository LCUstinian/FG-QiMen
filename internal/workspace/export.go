// export.go — portable .fgq project file format.
//
// v0.4 Phase 2.4: a .fgq file is a single-file dump of a
// persistent project. Format (binary):
//
//	[4 bytes magic "FGQ1"]  (file marker + version)
//	[4 bytes header length, little-endian uint32]
//	[N bytes header JSON]   (project name, version, created_at, etc.)
//	[rest]                  bbolt data, byte-for-byte copy of fg.db
//
// v0.4 Phase 2.4：.fgq 文件是持久化项目的单文件转储。格式（二进制）：
//
//	[4 字节 magic "FGQ1"]           (文件标识 + 版本)
//	[4 字节 header 长度，小端 uint32]
//	[N 字节 header JSON]            (项目名、版本、创建时间等)
//	[剩余]                           bbolt 数据，fg.db 字节级副本
//
// The format is designed to be:
//   - Detected at a glance (4-byte magic)
//   - Forward-compatible (header is JSON with explicit version
//     field; v0.5+ can add fields without breaking v0.4 readers
//     that ignore unknown fields)
//   - Trivial to validate (header length bounds check before read)
//   - Single-file, so it can be attached to a chat message or
//     emailed to a teammate
//
// The format is designed to be:
//   - Detected at a glance (4-byte magic)
//   - Forward-compatible (header is JSON)
//   - Single-file

package workspace

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/LCUstinian/FG-QiMen/internal/version"
)

// fgqMagic is the 4-byte file marker. v0.4 ships "FGQ1" — any
// reader that doesn't recognise the magic aborts. / fgqMagic 是 4
// 字节文件标识。v0.4 发 "FGQ1"——任何不识别该 magic 的读端中止。
var fgqMagic = [4]byte{'F', 'G', 'Q', '1'}

// fgqHeader is the JSON-serialised metadata block at the start
// of the .fgq file. / fgqHeader 是 .fgq 文件起始的 JSON 序列化
// 元数据块。
type fgqHeader struct {
	Version   string    `json:"version"`   // FG-QiMen release that produced this file, e.g. "v0.4.0"
	Project   string    `json:"project"`   // project name
	CreatedAt time.Time `json:"created_at"` // export time (local)
	DBBytes   int64     `json:"db_bytes"`  // size of the embedded bbolt data
}

// Export writes the persistent project at <ProjectsRoot>/<name>/
// to a .fgq file at outPath. The output is a single file that
// can be re-imported via Import. / Export 把 <ProjectsRoot>/<name>/
// 下的持久化项目写到 outPath 的 .fgq 文件。输出是单文件，可通过
// Import 重新导入。
//
// Export closes the project's bbolt DB before reading the file
// (bbolt requires no live writers to read safely) and re-opens
// it after, so a running scan against the same project can
// resume after the export. / Export 在读文件前关闭项目的 bbolt DB
// （bbolt 要求无活跃写者才能安全读取），之后重开——同一个项目上的
// 运行中扫描可以在 export 之后继续。
func (p *Project) Export(outPath string) error {
	if p == nil {
		return errors.New("nil project")
	}
	if p.DB == nil {
		return errors.New("project has no bbolt DB (--no-state or ephemeral project); nothing to export")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", outPath, err)
	}
	// Flush + close before reading. / 读之前先 flush + close。
	dbPath := p.DBPath
	if err := p.Close(); err != nil {
		return fmt.Errorf("close bbolt before export: %w", err)
	}
	defer func() {
		// Reopen so the caller can keep using the Project.
		// Best-effort; if reopen fails, the caller will see
		// DB=nil on the returned Project. / 重新打开让调用方继续
		// 用 Project。尽力；如果重开失败，调用方会看到 Project.DB=nil。
		if db, err := bolt.Open(dbPath, 0o600, nil); err == nil {
			p.DB = db
			p.DBPath = dbPath
		}
	}()

	// Read the bbolt data. / 读 bbolt 数据。
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return fmt.Errorf("read bbolt %s: %w", dbPath, err)
	}
	header := fgqHeader{
		Version:   version.Value, // set at link time via -ldflags -X
		Project:   p.Name,
		CreatedAt: time.Now(),
		DBBytes:   int64(len(data)),
	}
	headerBytes, err := json.Marshal(&header)
	if err != nil {
		return fmt.Errorf("marshal header: %w", err)
	}
	// header length is uint32; 64 MiB upper bound matches the
	// largest realistic header (a project name and a timestamp
	// are tiny; this is purely defensive). / header 长度是 uint32；
	// 64 MiB 上界与最大合理 header 一致（项目名和时间戳都很
	// 小；这纯粹是防御性的）。
	if len(headerBytes) > 64*1024*1024 {
		return errors.New("header too large")
	}

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer func() { _ = out.Close() }()
	// Magic. / Magic。
	if _, err := out.Write(fgqMagic[:]); err != nil {
		return err
	}
	// Header length (LE uint32). / Header 长度（LE uint32）。
	if err := binary.Write(out, binary.LittleEndian, uint32(len(headerBytes))); err != nil {
		return err
	}
	// Header JSON. / Header JSON。
	if _, err := out.Write(headerBytes); err != nil {
		return err
	}
	// bbolt data, byte-for-byte. / bbolt 数据，字节级。
	if _, err := out.Write(data); err != nil {
		return err
	}
	return out.Close()
}

// Import reads a .fgq file from inPath and creates a new
// project at name. / Import 读 inPath 的 .fgq 文件，在 name
// 处创建新项目。
//
// The original bbolt file is reconstructed byte-for-byte and
// placed at <ProjectsRoot>/<name>/fg.db. If a project with
// this name already exists, Import refuses to overwrite unless
// the caller passed the --force flag (the calling CLI does that
// check). / 原 bbolt 文件字节级重建，置于 <ProjectsRoot>/<name>/fg.db。
// 如果同名项目已存在，Import 拒绝覆盖，除非调用方传 --force
// flag（CLI 调用方做检查）。
func Import(inPath, name string) error {
	if inPath == "" {
		return errors.New("import path is empty")
	}
	if name == "" {
		return errors.New("project name is empty")
	}
	if err := ValidateProjectName(name); err != nil {
		return err
	}

	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", inPath, err)
	}
	defer func() { _ = f.Close() }()

	// Validate magic. / 验证 magic。
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("read magic: %w", err)
	}
	if magic != fgqMagic {
		return fmt.Errorf("not a .fgq file (bad magic %q)", magic)
	}
	// Read header length. / 读 header 长度。
	var headerLen uint32
	if err := binary.Read(f, binary.LittleEndian, &headerLen); err != nil {
		return fmt.Errorf("read header length: %w", err)
	}
	if headerLen > 64*1024*1024 {
		return fmt.Errorf("header length %d exceeds 64 MiB cap", headerLen)
	}
	// Read + parse header. / 读 + parse header。
	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	var header fgqHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("parse header: %w", err)
	}
	// Copy the remaining bytes to a temp file, then rename
	// over the bbolt path. / 把剩余字节拷到临时文件，再 rename
	// 到 bbolt 路径。
	dir := filepath.Join(ProjectsRoot(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	dbPath := filepath.Join(dir, "fg.db")
	tmp, err := os.CreateTemp(dir, "fg.db.import.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if rename didn't happen.
		// / 如果 rename 未发生，尽力清理。
		_ = os.Remove(tmpName)
	}()
	// Header says the bbolt data is N bytes; the file should
	// have N more bytes after the header. / Header 说 bbolt 数
	// 据 N 字节；文件 header 后应还有 N 字节。
	if _, err := io.Copy(tmp, io.LimitReader(f, header.DBBytes)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy bbolt data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dbPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmpName, dbPath, err)
	}
	return nil
}
