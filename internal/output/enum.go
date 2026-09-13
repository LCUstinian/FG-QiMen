// enum.go — v0.9 evidence sinks: scope discovery events, SMB share
// enumerations, FTP directory walks, and the important-server
// inventory. All follow the established per-sink-mutex pattern; the
// servers report aggregates per host and emits at Close() because a
// half-written server row mid-stream would read as a complete
// inventory.
//
// enum.go — v0.9 取证 sink：范围发现事件、SMB 共享枚举、FTP 目录遍
// 历与重要服务器清单。全部沿用既有的 per-sink-mutex 模式；servers
// 报告按 host 聚合、Close() 时输出——流中半截服务器记录会被误读
// 为完整清单。
package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// WriteDiscovery appends one out-of-scope NIC discovery event to
// discovery.json (NDJSON) and discovery.txt (human-readable). Safe for
// concurrent producers; dedup is the caller's (scope.Tracker) job.
// / WriteDiscovery 把一条范围外网卡发现事件追加到 discovery.json
// （NDJSON）与 discovery.txt（人类可读）。并发安全；去重是调用方
// （scope.Tracker）的职责。
func (o *Output) WriteDiscovery(ev types.DiscoveryEvent) {
	if o.discjson != nil {
		o.discMu.Lock()
		enc := json.NewEncoder(o.discjson)
		_ = enc.Encode(ev)
		o.discMu.Unlock()
	}
	if o.disctxt != nil {
		o.discMu.Lock()
		ts := ev.Time.Format("2006-01-02 15:04:05")
		attrs := ""
		if len(ev.Attributes) > 0 {
			keys := make([]string, 0, len(ev.Attributes))
			for k := range ev.Attributes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+ev.Attributes[k])
			}
			attrs = " attrs=[" + strings.Join(parts, " ") + "]"
		}
		fmt.Fprintf(o.disctxt, "[%s] ip=%s mac=%s hostname=%s source=%s%s\n",
			ts, ev.IP, ev.MAC, ev.Hostname, ev.SourceProtocol, attrs)
		o.discMu.Unlock()
	}
}

// WriteShares writes a structured SMB share enumeration to shares.ndjson
// (NDJSON) and shares.txt (human-readable). The payload type lives in
// types (types.ShareEnumResult); the smb plugin produces it into
// Result.Extra and output only renders it. / WriteShares 把结构化的
// SMB 共享枚举写入 shares.ndjson（NDJSON）与 shares.txt（人类可读）。
// 载荷类型在 types（types.ShareEnumResult）：smb 插件把它放进
// Result.Extra，output 只负责渲染。
func (o *Output) WriteShares(fp types.ShareEnumResult) error {
	if o.sharesjson != nil {
		o.sharesMu.Lock()
		enc := json.NewEncoder(o.sharesjson)
		_ = enc.Encode(fp)
		o.sharesMu.Unlock()
	}
	if o.sharestxt != nil {
		o.sharesMu.Lock()
		ts := fp.ScanTime.Format("2006-01-02 15:04:05")
		fmt.Fprintf(o.sharestxt, "[%s] %s:%d  anonymous=%v shares=%d truncated=%v\n",
			ts, fp.Host, fp.Port, fp.Anonymous, len(fp.Shares), fp.Truncated)
		for _, s := range fp.Shares {
			fmt.Fprintf(o.sharestxt, "  \\\\%s\\%s  access=%s files=%d",
				fp.Host, s.Name, s.Access, len(s.Files))
			if s.Error != "" {
				fmt.Fprintf(o.sharestxt, " error=%q", s.Error)
			}
			fmt.Fprintln(o.sharestxt)
			for _, f := range s.Files {
				if f.IsDir {
					fmt.Fprintf(o.sharestxt, "    d---- %s\n", f.Name)
					continue
				}
				mt := ""
				if !f.MTime.IsZero() {
					mt = f.MTime.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(o.sharestxt, "    -a--- %s  %d bytes  %s\n", f.Name, f.Size, mt)
			}
		}
		o.sharesMu.Unlock()
	}
	return nil
}

// WriteFTP writes a structured FTP read-only directory walk to
// ftp.ndjson (NDJSON) and ftp.txt (human-readable). Deliberately separate
// from the shares sinks so share findings and FTP findings never merge
// into one report. / WriteFTP 把结构化的 FTP 只读目录遍历写入
// ftp.ndjson（NDJSON）与 ftp.txt（人类可读）。刻意与 shares sink 分开，
// 让共享发现与 FTP 发现永不合并进同一报告。
func (o *Output) WriteFTP(fp types.FTPEnumResult) error {
	if o.ftpjson != nil {
		o.ftpMu.Lock()
		enc := json.NewEncoder(o.ftpjson)
		_ = enc.Encode(fp)
		o.ftpMu.Unlock()
	}
	if o.ftptxt != nil {
		o.ftpMu.Lock()
		ts := fp.ScanTime.Format("2006-01-02 15:04:05")
		who := "anonymous"
		if !fp.Anonymous && fp.User != "" {
			who = fp.User
		}
		fmt.Fprintf(o.ftptxt, "[%s] %s:%d  login=%s dirs=%d truncated=%v\n",
			ts, fp.Host, fp.Port, who, len(fp.Dirs), fp.Truncated)
		for _, d := range fp.Dirs {
			perms := d.Perms
			if perms == "" {
				perms = "---------"
			}
			fmt.Fprintf(o.ftptxt, "  %s  %s  files=%d", d.Path, perms, len(d.Files))
			if d.Error != "" {
				fmt.Fprintf(o.ftptxt, " error=%q", d.Error)
			}
			fmt.Fprintln(o.ftptxt)
			for _, f := range d.Files {
				if f.IsDir {
					fmt.Fprintf(o.ftptxt, "    d---- %s\n", f.Name)
					continue
				}
				mt := ""
				if !f.MTime.IsZero() {
					mt = f.MTime.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(o.ftptxt, "    -a--- %s  %d bytes  %s\n", f.Name, f.Size, mt)
			}
		}
		o.ftpMu.Unlock()
	}
	return nil
}

// ServerRecord is one aggregated important-server inventory row: the
// host, its infrastructure roles, every port/service/product that
// contributed a role, and the hostname (when the scope tracker captured
// one). / ServerRecord 是聚合后的重要服务器清单行：主机、基础设施角
// 色、贡献过角色的每个端口/服务/产品，以及主机名（范围追踪器捕到
// 时填写）。
type ServerRecord struct {
	Host      string   `json:"host"`
	Hostname  string   `json:"hostname,omitempty"`
	Roles     []string `json:"roles"`
	Ports     []int    `json:"ports"`
	Services  []string `json:"services,omitempty"`
	Products  []string `json:"products,omitempty"`
	FirstSeen string   `json:"first_seen"`
	LastSeen  string   `json:"last_seen"`
}

// collectServers folds a role-tagged result into the per-host server
// inventory. Called from WriteResult (under no lock of its own — the
// serversMu guards the map). Hostnames come from the injected lookup
// (scope tracker NBNS names) when available. / collectServers 把带角
// 色标记的结果折叠进按 host 的服务器清单。由 WriteResult 调用
// （serversMu 守护 map）。主机名来自注入的查找函数（scope 追踪器的
// NBNS 名）。
func (o *Output) collectServers(r *types.Result) {
	if o.serversjson == nil && o.serverstxt != nil {
		// txt-only is a valid sink combination — keep collecting.
		// / 纯 txt 也是合法 sink 组合——继续收集。
	} else if o.serversjson == nil && o.serverstxt == nil {
		return
	}
	if len(r.Important) == 0 || r.Host == "" {
		return
	}
	o.serversMu.Lock()
	defer o.serversMu.Unlock()
	if o.serversBuf == nil {
		o.serversBuf = make(map[string]*ServerRecord)
	}
	rec, ok := o.serversBuf[r.Host]
	if !ok {
		ts := r.Time.Format("2006-01-02 15:04:05")
		rec = &ServerRecord{Host: r.Host, FirstSeen: ts, LastSeen: ts}
		if o.hostnameLookup != nil {
			rec.Hostname = o.hostnameLookup(r.Host)
		}
		o.serversBuf[r.Host] = rec
	} else {
		rec.LastSeen = r.Time.Format("2006-01-02 15:04:05")
		if rec.Hostname == "" && o.hostnameLookup != nil {
			rec.Hostname = o.hostnameLookup(r.Host)
		}
	}
	for _, role := range r.Important {
		if !containsStr(rec.Roles, role) {
			rec.Roles = append(rec.Roles, role)
		}
	}
	if !containsInt(rec.Ports, r.Port) {
		rec.Ports = append(rec.Ports, r.Port)
	}
	if r.Service != "" && !containsStr(rec.Services, r.Service) {
		rec.Services = append(rec.Services, r.Service)
	}
	if r.Product != "" && !containsStr(rec.Products, r.Product) {
		rec.Products = append(rec.Products, r.Product)
	}
}

// writeServersLocked renders the aggregated inventory to one of the
// two servers sinks. It runs under serversMu (called from Close), so
// it may read o.serversBuf freely. Host-sorted for stable output.
// / writeServersLocked 把聚合清单渲染到两个 servers sink 之一。在
// serversMu 内执行（由 Close 调用），可自由读 o.serversBuf。按 host
// 排序保证输出稳定。
func (o *Output) writeServersLocked(w *flushCloser, label string) error {
	if w == nil {
		return nil
	}
	hosts := make([]string, 0, len(o.serversBuf))
	for h := range o.serversBuf {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	switch label {
	case "servers.json":
		enc := json.NewEncoder(w)
		for _, h := range hosts {
			if err := enc.Encode(o.serversBuf[h]); err != nil {
				return err
			}
		}
		return nil
	case "servers.txt":
		fmt.Fprintln(w, "重要服务器清单 / Important Server Inventory")
		fmt.Fprintf(w, "hosts=%d\n", len(hosts))
		for _, h := range hosts {
			rec := o.serversBuf[h]
			roles := strings.Join(rec.Roles, ",")
			ports := make([]string, 0, len(rec.Ports))
			for _, p := range rec.Ports {
				ports = append(ports, fmt.Sprintf("%d", p))
			}
			hostname := rec.Hostname
			if hostname == "" {
				hostname = "-"
			}
			fmt.Fprintf(w, "  %s  hostname=%s  roles=[%s]  ports=[%s]  services=[%s]  products=[%s]  first=%s last=%s\n",
				rec.Host, hostname, roles, strings.Join(ports, ","),
				strings.Join(rec.Services, ","), strings.Join(rec.Products, ","),
				rec.FirstSeen, rec.LastSeen)
		}
		return nil
	}
	return nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
