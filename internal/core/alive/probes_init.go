// probes_init.go — registers the in-tree always-on probes (ICMP,
// TCP-ping, system-ping) into the alive package's always-on probe
// registry, mirroring the LAN-probe registration done by
// internal/discovery. Future always-on probes (e.g. an HTTP HEAD
// fallback) belong in their own file and call RegisterAlwaysOnProbe
// from their own init().
//
// probes_init.go — 把内置 always-on probe（ICMP、TCP-ping、system-ping）
// 注册到 alive 包的 always-on probe 注册表，与 internal/discovery 的
// LAN-probe 注册路径对称。未来的 always-on probe（例如 HTTP HEAD 回退）
// 应各自成文件并在 init() 中调用 RegisterAlwaysOnProbe。
//
// The registration order below is preserved in DefaultOptions:
//   1. ICMP        — raw socket echo, fastest
//   2. TCP-ping    — port-list connect, works when ICMP is blocked
//   3. system-ping — falls back to OS `ping` binary, broadest compat
//
// 注册顺序保留到 DefaultOptions 中：ICMP → TCP-ping → system-ping。
package alive

func init() {
	// Order is the documented probe chain: fastest first, broadest
	// last. Discovery's first-match semantics stop at the first hit,
	// so this is also the precedence chain.
	RegisterAlwaysOnProbe(NewICMPProbe())
	RegisterAlwaysOnProbe(NewTCPProbe())
	RegisterAlwaysOnProbe(NewSystemPingProbe())
}
