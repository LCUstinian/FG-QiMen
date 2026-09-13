// Package roles classifies scan results into infrastructure roles
// ("important server" tags): domain controllers, file servers, backup
// servers, VPN gateways, databases, virtualization hosts, mail servers
// and out-of-band management cards.
//
// Matching is deliberately conservative and evidence-based: a rule
// requires an exact port, and MAY additionally require a service name,
// a product substring or a banner substring (case-insensitive). No
// regex — the fingerprint regex-injection lesson applies here too;
// substring matching is predictable and auditable.
//
// Package roles 把扫描结果归类为基础设施角色（"重要服务器"标签）：
// 域控、文件服务器、备份服务器、VPN 网关、数据库、虚拟化主机、邮
// 件服务器与带外管理卡。
//
// 匹配刻意保守且以证据为据：规则要求端口精确命中，可选要求服务名、
// 产品子串或 banner 子串（大小写不敏感）。不用正则——指纹层的正则
// 注入教训在此同样适用；子串匹配可预测、可审计。
package roles

import (
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Role is an infrastructure role tag. / Role 是一个基础设施角色标签。
type Role string

// The full v0.9 role vocabulary. The first four are the requirement's
// named core; the second four are the approved extended set.
// / v0.9 角色全表。前四个是需求点名的核心；后四个是确认的扩展集。
const (
	RoleDC         Role = "domain-controller"
	RoleFileServer Role = "file-server"
	RoleBackup     Role = "backup-server"
	RoleVPN        Role = "vpn-server"
	RoleDB         Role = "database-server"
	RoleVirt       Role = "virtualization"
	RoleMail       Role = "mail-server"
	RoleOOB        Role = "oob-management"
)

// Rule maps one port (required) plus optional service / product /
// banner substrings (all AND-complemented, case-insensitive) to a
// role. / Rule 把一个端口（必填）加可选服务/产品/banner 子串（全部
// AND 组合，大小写不敏感）映射到一个角色。
type Rule struct {
	Role    Role
	Ports   []int
	Service string // exact service name, "" = any / 精确服务名，"" = 任意
	Product string // case-insensitive substring, "" = any / 大小写不敏感子串，"" = 任意
	Banner  string // case-insensitive substring, "" = any / 大小写不敏感子串，"" = 任意
}

// DefaultTable is the built-in rule table. Port-only rules carry the
// known caveat inline (e.g. a standalone MIT KDC also serves 88 —
// accepted false-positive surface; in intranet practice 88 is a DC).
// / DefaultTable 是内置规则表。仅端口规则自带备注（如独立 MIT KDC
// 也开 88——已接受的误报面；内网实践中 88 基本就是域控）。
func DefaultTable() []Rule {
	return []Rule{
		// Domain controllers. Kerberos 88 is DC in practice (standalone
		// KDC = accepted rare false positive); global catalog 3268/3269
		// is DC-only; LDAP 389/636 only counts when the product names
		// Active Directory. / 域控。Kerberos 88 实践中即域控（独立
		// KDC = 已接受的罕见误报）；全局编录 3268/3269 是域控专属；
		// LDAP 389/636 仅在产品名包含 Active Directory 时计入。
		{Role: RoleDC, Ports: []int{88}},
		{Role: RoleDC, Ports: []int{3268, 3269}},
		{Role: RoleDC, Ports: []int{389, 636}, Product: "active directory"},

		// Backup servers. Veeam 9392 (console)/9393, Backup Exec 10000
		// (NDMP), NetBackup 13724 (vnetd); banner/product hints for the
		// rest. / 备份服务器。Veeam 9392（控制台）/9393，Backup Exec
		// 10000（NDMP），NetBackup 13724（vnetd）；其余靠 banner/产品提示。
		{Role: RoleBackup, Ports: []int{9392, 9393}},
		{Role: RoleBackup, Ports: []int{10000}},
		{Role: RoleBackup, Ports: []int{13724}},
		{Role: RoleBackup, Ports: []int{443, 80}, Product: "veeam"},
		{Role: RoleBackup, Ports: []int{443, 80}, Product: "netbackup"},

		// VPN gateways. OpenVPN 1194 (TCP mode); SSL-VPN products on
		// 443. / VPN 网关。OpenVPN 1194（TCP 模式）；443 上的 SSL-VPN 产品。
		{Role: RoleVPN, Ports: []int{1194}},
		{Role: RoleVPN, Ports: []int{443}, Product: "openvpn"},
		{Role: RoleVPN, Ports: []int{443}, Product: "anyconnect"},
		{Role: RoleVPN, Ports: []int{443}, Product: "fortigate"},
		{Role: RoleVPN, Ports: []int{443}, Product: "fortinet"},
		{Role: RoleVPN, Ports: []int{443}, Product: "pulse secure"},
		{Role: RoleVPN, Ports: []int{443}, Product: "sonicwall"},

		// Database servers. Service-name driven — the DB plugins own
		// these service claims. / 数据库服务器。按服务名驱动——这些服
		// 务断言由 DB 插件负责。
		{Role: RoleDB, Ports: []int{3306}, Service: "mysql"},
		{Role: RoleDB, Ports: []int{5432}, Service: "postgresql"},
		{Role: RoleDB, Ports: []int{1433}, Service: "mssql"},
		{Role: RoleDB, Ports: []int{1521}, Service: "oracle"},
		{Role: RoleDB, Ports: []int{27017}, Service: "mongodb"},
		{Role: RoleDB, Ports: []int{6379}, Service: "redis"},
		{Role: RoleDB, Ports: []int{9200}, Service: "elasticsearch"},

		// Virtualization hosts. ESXi agent 902; VMware products on 443.
		// / 虚拟化主机。ESXi agent 902；443 上的 VMware 产品。
		{Role: RoleVirt, Ports: []int{902}},
		{Role: RoleVirt, Ports: []int{443}, Product: "vmware"},
		{Role: RoleVirt, Ports: []int{443}, Product: "vcenter"},
		{Role: RoleVirt, Ports: []int{443}, Product: "esxi"},

		// Mail servers. The email plugins' service claims; Exchange
		// products on 443/80. / 邮件服务器。email 插件的服务断言；
		// 443/80 上的 Exchange 产品。
		{Role: RoleMail, Ports: []int{25, 465, 587}, Service: "smtp"},
		{Role: RoleMail, Ports: []int{143, 993}, Service: "imap"},
		{Role: RoleMail, Ports: []int{110, 995}, Service: "pop3"},
		{Role: RoleMail, Ports: []int{443, 80}, Product: "exchange"},

		// Out-of-band management. IPMI/RMCP+ 623 (UDP phase); iLO /
		// iDRAC / Supermicro products on web ports. / 带外管理。
		// IPMI/RMCP+ 623（UDP 阶段）；web 端口上的 iLO / iDRAC /
		// Supermicro 产品。
		{Role: RoleOOB, Ports: []int{623}},
		{Role: RoleOOB, Ports: []int{443, 80, 17988}, Product: "ilo"},
		{Role: RoleOOB, Ports: []int{443, 80}, Product: "idrac"},
		{Role: RoleOOB, Ports: []int{443, 80}, Product: "supermicro"},
		{Role: RoleOOB, Ports: []int{443, 80}, Product: "ipmi"},
	}
}

// Evaluate returns every role the rule table attributes to r. Port
// always must match; service / product / banner constrain further when
// set. Deterministic order (table order). / Evaluate 返回规则表给 r
// 归类的全部角色。端口必须命中；service/product/banner 设了就进一步
// 约束。顺序确定（按表序）。
func Evaluate(r *types.Result) []string {
	return EvaluateWith(DefaultTable(), r)
}

// EvaluateWith is the table-injectable core of Evaluate.
// / EvaluateWith 是 Evaluate 的可注入表核心。
func EvaluateWith(table []Rule, r *types.Result) []string {
	if r == nil {
		return nil
	}
	var out []Role
	for _, rule := range table {
		if !containsInt(rule.Ports, r.Port) {
			continue
		}
		if rule.Service != "" && !strings.EqualFold(rule.Service, r.Service) {
			continue
		}
		if rule.Product != "" && !strings.Contains(strings.ToLower(r.Product), strings.ToLower(rule.Product)) {
			continue
		}
		if rule.Banner != "" && !strings.Contains(strings.ToLower(r.Banner), strings.ToLower(rule.Banner)) {
			continue
		}
		if !containsRole(out, rule.Role) {
			out = append(out, rule.Role)
		}
	}
	if len(out) == 0 {
		return nil
	}
	roles := make([]string, len(out))
	for i, ro := range out {
		roles[i] = string(ro)
	}
	return roles
}

// EvaluateShareEnum tags the file-server role from positive share
// evidence: a null session that mounted a share and listed files IS a
// file server, by direct observation rather than port inference.
// / EvaluateShareEnum 从正面共享证据打 file-server 角色：null
// session 能挂载并列出文件的机器就是文件服务器——直接观察而非端口推断。
func EvaluateShareEnum(fp *types.ShareEnumResult) []string {
	if fp == nil {
		return nil
	}
	for _, s := range fp.Shares {
		if s.Access == "list" {
			return []string{string(RoleFileServer)}
		}
	}
	return nil
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func containsRole(list []Role, r Role) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}
