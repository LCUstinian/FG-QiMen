// enum_test.go — v0.9 evidence sink tests: discovery events, share
// enumerations, FTP walks and the important-server inventory
// aggregation. The aggregation is the security-relevant part: role
// dedup per host, hostname enrichment and Close-time emission must all
// be deterministic.
//
// enum_test.go — v0.9 取证 sink 测试：发现事件、共享枚举、FTP 遍历
// 与重要服务器清单聚合。聚合是安全相关部分：按 host 的角色去重、主
// 机名充实与 Close 时输出都必须是确定的。
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

func TestWriteDiscovery(t *testing.T) {
	dir := t.TempDir()
	jPath := filepath.Join(dir, "discovery.json")
	tPath := filepath.Join(dir, "discovery.txt")
	o, err := OpenOutput(OutputConfig{DiscoveryJSONPath: jPath, DiscoveryTXTPath: tPath})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	o.WriteDiscovery(types.DiscoveryEvent{
		Time:           time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC),
		IP:             "10.9.0.6",
		MAC:            "aa:bb:cc:dd:ee:ff",
		Hostname:       "nas01",
		SourceProtocol: "nbstat",
		Attributes:     map[string]string{"z": "last", "a": "first"},
	})
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	body, err := os.ReadFile(jPath)
	if err != nil {
		t.Fatalf("read discovery.json: %v", err)
	}
	var ev types.DiscoveryEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		t.Fatalf("discovery.json not NDJSON: %v", err)
	}
	if ev.IP != "10.9.0.6" || ev.SourceProtocol != "nbstat" || ev.Hostname != "nas01" {
		t.Fatalf("json roundtrip mismatch: %+v", ev)
	}

	txt, err := os.ReadFile(tPath)
	if err != nil {
		t.Fatalf("read discovery.txt: %v", err)
	}
	line := nonEmptyLines(string(txt))[0]
	for _, want := range []string{"ip=10.9.0.6", "mac=aa:bb:cc:dd:ee:ff", "hostname=nas01", "source=nbstat", "a=first z=last"} {
		if !strings.Contains(line, want) {
			t.Errorf("txt line %q missing %q", line, want)
		}
	}
}

func TestWriteShares(t *testing.T) {
	dir := t.TempDir()
	jPath := filepath.Join(dir, "shares.json")
	tPath := filepath.Join(dir, "shares.txt")
	o, err := OpenOutput(OutputConfig{SharesJSONPath: jPath, SharesTXTPath: tPath})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	err = o.WriteShares(types.ShareEnumResult{
		Host: "10.9.0.6", Port: 445, Anonymous: true,
		ScanTime: time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC),
		Shares: []types.ShareInfo{
			{Name: "IPC$", Access: "none", Error: "access denied"},
			{Name: "public", Access: "list", Files: []types.EnumFileInfo{
				{Name: "backup", IsDir: true},
				{Name: "readme.txt", Size: 42, MTime: time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)},
			}},
		},
	})
	if err != nil {
		t.Fatalf("WriteShares: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var fp types.ShareEnumResult
	if body, err := os.ReadFile(jPath); err != nil {
		t.Fatalf("read shares.json: %v", err)
	} else if err := json.Unmarshal(body, &fp); err != nil {
		t.Fatalf("shares.json not NDJSON: %v", err)
	}
	if len(fp.Shares) != 2 || fp.Shares[1].Access != "list" || len(fp.Shares[1].Files) != 2 {
		t.Fatalf("json roundtrip mismatch: %+v", fp)
	}

	txt, err := os.ReadFile(tPath)
	if err != nil {
		t.Fatalf("read shares.txt: %v", err)
	}
	lines := nonEmptyLines(string(txt))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"10.9.0.6:445", "anonymous=true", "shares=2",
		"\\\\10.9.0.6\\public", "access=list", "d---- backup", "-a--- readme.txt  42 bytes",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("shares.txt missing %q", want)
		}
	}
}

func TestWriteFTP(t *testing.T) {
	dir := t.TempDir()
	tPath := filepath.Join(dir, "ftp.txt")
	o, err := OpenOutput(OutputConfig{FTPTXTPath: tPath})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	err = o.WriteFTP(types.FTPEnumResult{
		Host: "10.9.0.7", Port: 21, Anonymous: false, User: "svc-backup",
		ScanTime: time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC),
		Dirs: []types.FTPDir{
			{Path: "/", Perms: "drwxr-xr-x", Files: []types.EnumFileInfo{{Name: "etc", IsDir: true}}},
			{Path: "/pub", Error: "550 permission denied"},
		},
	})
	if err != nil {
		t.Fatalf("WriteFTP: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	txt, err := os.ReadFile(tPath)
	if err != nil {
		t.Fatalf("read ftp.txt: %v", err)
	}
	joined := strings.Join(nonEmptyLines(string(txt)), "\n")
	for _, want := range []string{
		"10.9.0.7:21", "login=svc-backup", "dirs=2",
		"/  drwxr-xr-x  files=1", "d---- etc", "/pub  ---------  files=0 error=\"550 permission denied\"",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ftp.txt missing %q", want)
		}
	}
	// Anonymous walk shows the anonymous login label. / 匿名遍历显示 anonymous 标签。
	o2, err := OpenOutput(OutputConfig{FTPTXTPath: filepath.Join(dir, "ftp2.txt")})
	if err != nil {
		t.Fatalf("OpenOutput 2: %v", err)
	}
	_ = o2.WriteFTP(types.FTPEnumResult{Host: "h", Port: 21, Anonymous: true, Dirs: []types.FTPDir{{Path: "/"}}})
	_ = o2.Close()
	body, _ := os.ReadFile(filepath.Join(dir, "ftp2.txt"))
	if !strings.Contains(string(body), "login=anonymous") {
		t.Errorf("anonymous walk should be labelled login=anonymous, got %q", body)
	}
}

func TestServersInventory_Aggregation(t *testing.T) {
	dir := t.TempDir()
	jPath := filepath.Join(dir, "servers.json")
	tPath := filepath.Join(dir, "servers.txt")
	o, err := OpenOutput(OutputConfig{ServersJSONPath: jPath, ServersTXTPath: tPath})
	if err != nil {
		t.Fatalf("OpenOutput: %v", err)
	}
	o.SetHostnameLookup(func(host string) string {
		if host == "10.9.0.1" {
			return "dc01.corp"
		}
		return ""
	})
	ts := time.Date(2026, 9, 13, 22, 0, 0, 0, time.UTC)
	// Two results for the same DC host — roles/ports dedupe, services
	// and products accumulate. / 同一 DC 主机两条结果——角色/端口去
	// 重，服务与产品累积。
	_ = o.WriteResult(&types.Result{Host: "10.9.0.1", Port: 88, Time: ts, Important: []string{"domain-controller"}})
	_ = o.WriteResult(&types.Result{Host: "10.9.0.1", Port: 389, Service: "ldap", Product: "Active Directory", Time: ts, Important: []string{"domain-controller"}})
	// Untagged results must not create rows. / 无角色标记的结果不产生行。
	_ = o.WriteResult(&types.Result{Host: "10.9.0.2", Port: 80, Time: ts})
	// A file server host on another IP. / 另一 IP 的文件服务器主机。
	_ = o.WriteResult(&types.Result{Host: "10.9.0.6", Port: 445, Time: ts, Important: []string{"file-server"}})
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := make([]ServerRecord, 0, 4)
	body, err := os.ReadFile(jPath)
	if err != nil {
		t.Fatalf("read servers.json: %v", err)
	}
	for _, l := range nonEmptyLines(string(body)) {
		var r ServerRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("servers.json line not NDJSON: %v", err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 server rows, got %d: %+v", len(recs), recs)
	}
	// Host-sorted: 10.9.0.1 first. / 按 host 排序：10.9.0.1 在前。
	dc, fs := recs[0], recs[1]
	if dc.Host != "10.9.0.1" || fs.Host != "10.9.0.6" {
		t.Fatalf("host sort mismatch: %+v", recs)
	}
	if dc.Hostname != "dc01.corp" {
		t.Errorf("hostname enrichment failed: %+v", dc)
	}
	if len(dc.Roles) != 1 || dc.Roles[0] != "domain-controller" {
		t.Errorf("role dedup failed: %+v", dc)
	}
	if len(dc.Ports) != 2 || dc.Ports[0] != 88 || dc.Ports[1] != 389 {
		t.Errorf("port accumulation failed: %+v", dc)
	}
	if len(dc.Services) != 1 || dc.Services[0] != "ldap" || len(dc.Products) != 1 {
		t.Errorf("service/product accumulation failed: %+v", dc)
	}

	txt, err := os.ReadFile(tPath)
	if err != nil {
		t.Fatalf("read servers.txt: %v", err)
	}
	joined := strings.Join(nonEmptyLines(string(txt)), "\n")
	for _, want := range []string{"hosts=2", "10.9.0.1  hostname=dc01.corp", "roles=[domain-controller]", "ports=[88,389]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("servers.txt missing %q", want)
		}
	}
}
