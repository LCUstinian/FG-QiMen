// topologies.go — the named topologies the bench judge can run. A1
// ships the minimal "single" loopback: one hard-identified service
// (SSH greeting), one web-identified service (HTTP), one probe-fed
// service (memcached, silent greeting → active-probe economics), plus
// blackholes (deadline burners) and refusals (RST baseline). Larger
// topologies (/24 scale, UDP, latency ladders) grow here on demand —
// per the v8 plan the matrix is not pre-built.
//
// / topologies.go —— 基准裁判可运行的具名拓扑。A1 交付最小 "single"
// 回环：一个硬识别服务（SSH greeting）、一个 web 识别服务（HTTP）、
// 一个探针喂给服务（memcached，无 greeting → 主动探针经济账），外加
// 黑洞（烧超时）与拒绝（RST 基线）。更大的拓扑（/24 规模、UDP、延
// 迟阶梯）按需在此生长——v8 方案裁定矩阵不预建。
package bench

// SingleTopology is the A1 minimal judge topology: 10 ports, 3
// services, 4 blackholes, 3 refusals. Blackholes are deliberately
// fewer than the services so one round stays fast while still
// exercising the timeout path.
// / SingleTopology 是 A1 最小裁判拓扑：10 端口 = 3 服务 + 4 黑洞 +
// 3 拒绝。黑洞刻意少于服务数——单轮保持快速的同时仍锻炼超时路径。
var SingleTopology = Topology{
	Name:       "single",
	Services:   []ServiceKind{ServiceSSH, ServiceHTTP, ServiceMemcached},
	Blackholes: 4,
	Refusals:   3,
}

// TopologyByName resolves the named topologies exposed to tools.
// / TopologyByName 解析暴露给工具的具名拓扑。
func TopologyByName(name string) (Topology, bool) {
	switch name {
	case "single":
		return SingleTopology, true
	default:
		return Topology{}, false
	}
}

// TopologyNames lists available topology names for usage text.
// / TopologyNames 列出可用拓扑名，供使用说明。
func TopologyNames() []string { return []string{"single"} }
