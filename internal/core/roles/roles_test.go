// roles_test.go — rule-table classification tests: port gates, optional
// service/product/banner constraints, dedup, and the share-evidence
// file-server path. Guarded by the regex-injection lesson: substring
// semantics only, so a product value that CONTAINS a rule's needle is
// matched, but regex metacharacters in results are inert.
//
// roles_test.go — 规则表分类测试：端口门、可选 service/product/banner
// 约束、去重、以及共享证据的 file-server 路径。正则注入教训的守卫：
// 只有子串语义——结果里含正则元字符也不会起作用，只会被当普通文本。
package roles

import (
	"reflect"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func TestEvaluateWith_PortOnlyRules(t *testing.T) {
	table := []Rule{
		{Role: RoleDC, Ports: []int{88}},
		{Role: RoleDC, Ports: []int{3268, 3269}},
		{Role: RoleVirt, Ports: []int{902}},
	}
	tests := []struct {
		name string
		res  *types.Result
		want []string
	}{
		{"kerberos 88 is DC", &types.Result{Port: 88}, []string{"domain-controller"}},
		{"global catalog 3269 is DC", &types.Result{Port: 3269}, []string{"domain-controller"}},
		{"esxi agent 902", &types.Result{Port: 902}, []string{"virtualization"}},
		{"wrong port matches nothing", &types.Result{Port: 22}, nil},
		{"nil result matches nothing", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateWith(table, tt.res)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluateWith(%+v) = %v, want %v", tt.res, got, tt.want)
			}
		})
	}
}

func TestEvaluateWith_Constraints(t *testing.T) {
	table := []Rule{
		{Role: RoleDC, Ports: []int{389}, Product: "active directory"},
		{Role: RoleVPN, Ports: []int{443}, Product: "fortigate"},
		{Role: RoleDB, Ports: []int{3306}, Service: "mysql"},
		{Role: RoleBackup, Ports: []int{80}, Banner: "veeam"},
	}
	tests := []struct {
		name string
		res  *types.Result
		want []string
	}{
		{
			"ldap without AD product is NOT a DC",
			&types.Result{Port: 389, Product: "OpenLDAP"},
			nil,
		},
		{
			"ldap with AD product (case-insensitive) is a DC",
			&types.Result{Port: 389, Product: "Active Directory LDAP"},
			[]string{"domain-controller"},
		},
		{
			"443 without VPN product is nothing",
			&types.Result{Port: 443, Product: "nginx"},
			nil,
		},
		{
			"443 fortigate (mixed case) is VPN",
			&types.Result{Port: 443, Product: "FortiGate-100F"},
			[]string{"vpn-server"},
		},
		{
			"3306 without mysql service is nothing",
			&types.Result{Port: 3306, Service: "mariadb"},
			nil,
		},
		{
			"3306 mysql service is DB",
			&types.Result{Port: 3306, Service: "mysql"},
			[]string{"database-server"},
		},
		{
			"banner substring veeam is backup",
			&types.Result{Port: 80, Banner: "Welcome to Veeam Backup"},
			[]string{"backup-server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateWith(table, tt.res)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluateWith(%+v) = %v, want %v", tt.res, got, tt.want)
			}
		})
	}
}

func TestEvaluateWith_MultiRoleAndDedup(t *testing.T) {
	// One result may carry several roles; each role appears at most once
	// even when two rules map the same result to it. / 一个结果可携带多
	// 个角色；两条规则命中同一角色时也只出现一次。
	table := []Rule{
		{Role: RoleBackup, Ports: []int{443}, Product: "veeam"},
		{Role: RoleOOB, Ports: []int{443, 80}, Product: "ilo"},
		{Role: RoleOOB, Ports: []int{443}, Product: "ipmi"},
		{Role: RoleVPN, Ports: []int{1194}},
	}
	got := EvaluateWith(table, &types.Result{Port: 443, Product: "Veeam iLO"})
	want := []string{"backup-server", "oob-management"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("multi-role = %v, want %v", got, want)
	}
	got = EvaluateWith(table, &types.Result{Port: 443, Product: "ipmi ilo"})
	want = []string{"oob-management"} // two OOB rules, one entry / 两条 OOB 规则，一个条目
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedup = %v, want %v", got, want)
	}
}

func TestEvaluate_DefaultTable(t *testing.T) {
	// Spot-check the shipped table: the requirement's named core roles
	// must all fire on realistic evidence. / 抽查内置表：需求点名的核心
	// 角色必须在真实证据上全部命中。
	tests := []struct {
		name string
		res  *types.Result
		want []string
	}{
		{
			"DC via global catalog",
			&types.Result{Port: 3268, Service: "ldap"},
			[]string{"domain-controller"},
		},
		{
			"file server via port 445 alone is NOT tagged (evidence too thin)",
			&types.Result{Port: 445, Service: "smb"},
			nil,
		},
		{
			"backup via veeam console port",
			&types.Result{Port: 9392},
			[]string{"backup-server"},
		},
		{
			"vpn via openvpn port",
			&types.Result{Port: 1194},
			[]string{"vpn-server"},
		},
		{
			"db via redis service",
			&types.Result{Port: 6379, Service: "redis"},
			[]string{"database-server"},
		},
		{
			"web port without product evidence stays untagged",
			&types.Result{Port: 443, Service: "https"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Evaluate(tt.res); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Evaluate(%+v) = %v, want %v", tt.res, got, tt.want)
			}
		})
	}
}

func TestEvaluateShareEnum(t *testing.T) {
	tests := []struct {
		name string
		fp   *types.ShareEnumResult
		want []string
	}{
		{"nil payload", nil, nil},
		{"no shares", &types.ShareEnumResult{}, nil},
		{
			"session ok but nothing listable",
			&types.ShareEnumResult{Shares: []types.ShareInfo{{Access: "none"}}},
			nil,
		},
		{
			"one listable share is a file server",
			&types.ShareEnumResult{Shares: []types.ShareInfo{{Access: "none"}, {Access: "list"}}},
			[]string{"file-server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvaluateShareEnum(tt.fp); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluateShareEnum(%+v) = %v, want %v", tt.fp, got, tt.want)
			}
		})
	}
}
