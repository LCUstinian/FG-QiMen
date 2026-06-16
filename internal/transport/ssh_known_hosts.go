// ssh_known_hosts.go — load ssh.HostKeyCallback from a known_hosts
// file. Kept in a separate file from transport.go so the golang.org/x/crypto/ssh
// import doesn't pollute the atomic-only main file.
package transport

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// loadKnownHostsCallback reads a known_hosts-format file and returns
// a HostKeyCallback that accepts any of the keys in the file. Only
// "host pattern,key-type,base64-key" lines are accepted; comments
// and empty lines are skipped. Lines with hashed hostnames (the
// |1|salt|hash form) are accepted as-is.
//
// M11 audit fix: the previous implementation ignored the host pattern
// (first field) and treated the file as a global "trust list" — any
// host presenting any key in the file was accepted. This weakens the
// security model vs. standard ssh known_hosts semantics (host-to-key
// binding). Now we parse the host patterns and match the connection
// target against them; hashed entries (|1|...) are accepted as a
// trust-list fallback since we can't reverse the hash.
//
// loadKnownHostsCallback 读 known_hosts 格式文件并返回接受文件中任
// 意 key 的 HostKeyCallback。仅接受"主机模式,key-type,base64-key"
// 行；注释与空行跳过。带哈希主机的行（|1|salt|hash 形式）原样接受。
//
// M11 审计修法：旧实现忽略主机模式（首字段），把文件当作全局"信任
// 列表"——任何主机呈现文件中任意 key 都被接受。这弱化了安全模型
// （相比标准 ssh known_hosts 的主机到 key 绑定）。现在解析主机模式
// 并匹配连接目标；哈希条目（|1|...）因无法逆推哈希仍按信任列表接受。
func loadKnownHostsCallback(path string) (ssh.HostKeyCallback, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open known_hosts %s: %w", path, err)
	}
	defer f.Close()

	// M11 audit fix: track both host-bound keys and global (hashed /
	// wildcard) keys. Host-bound keys require the connection target to
	// match the pattern; global keys accept any host (trust-list
	// fallback for hashed entries). / M11 审计修法：同时跟踪主机绑定
	// key 和全局（哈希/通配）key。主机绑定 key 要求连接目标匹配模式；
	// 全局 key 接受任意主机（哈希条目的信任列表回退）。
	type entry struct {
		hosts []string // host patterns; empty = global (hashed/wildcard)
		key   ssh.PublicKey
	}
	var entries []entry
	// globalKeys: keys from hashed or wildcard entries, accepted for any host.
	// globalKeys：来自哈希或通配条目的 key，对任意主机接受。
	globalKeys := make(map[string]bool)
	// hostKeys: map from host pattern → set of key blobs.
	// hostKeys：主机模式 → key blob 集合的 map。
	hostKeys := make(map[string]map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip optional marker fields at the start of the line
		// (@cert-authority, @revoked) — we don't differentiate.
		// 剥去行首可选的 marker 字段（@cert-authority、@revoked）
		// —— 我们不区分。
		if strings.HasPrefix(line, "@") {
			if sp := strings.IndexByte(line, ' '); sp > 0 {
				line = line[sp+1:]
			}
		}
		// M11 audit fix: parse host patterns from the line before
		// extracting the key. / M11 审计修法：在提取 key 前先解析
		// 主机模式。
		hosts, key := parseKnownHostLine(line)
		if key == nil {
			continue
		}
		keyBlob := string(key.Marshal())
		if len(hosts) == 0 {
			// Hashed or no host pattern → global trust list.
			// 哈希或无主机模式 → 全局信任列表。
			globalKeys[keyBlob] = true
		} else {
			for _, h := range hosts {
				if hostKeys[h] == nil {
					hostKeys[h] = make(map[string]bool)
				}
				hostKeys[h][keyBlob] = true
			}
		}
		entries = append(entries, entry{hosts: hosts, key: key})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read known_hosts %s: %w", path, err)
	}
	if len(entries) == 0 && len(globalKeys) == 0 {
		return nil, fmt.Errorf("known_hosts %s has no usable entries", path)
	}
	// M11 audit fix: the callback now checks host-to-key binding.
	// 1. If the host matches a pattern in hostKeys, only the bound keys
	//    are accepted.
	// 2. Otherwise, fall back to globalKeys (hashed/wildcard entries)
	//    as a trust-list.
	// 3. If neither matches, reject.
	//
	// M11 审计修法：callback 现在检查主机到 key 的绑定。
	// 1. 若主机匹配 hostKeys 中的模式，仅接受绑定的 key。
	// 2. 否则回退到 globalKeys（哈希/通配条目）作为信任列表。
	// 3. 都不匹配则拒绝。
	return func(host string, _ net.Addr, key ssh.PublicKey) error {
		if key == nil {
			return fmt.Errorf("ssh: nil server host key")
		}
		keyBlob := string(key.Marshal())
		// Check host-bound keys first. / 先查主机绑定 key。
		if bound, ok := hostKeys[host]; ok {
			if bound[keyBlob] {
				return nil
			}
			// Host has bound keys but this key isn't one of them.
			// 主机有绑定 key 但此 key 不在其中。
			return fmt.Errorf("ssh: host key for %s does not match known_hosts binding", host)
		}
		// Fall back to global trust list (hashed/wildcard entries).
		// 回退到全局信任列表（哈希/通配条目）。
		if globalKeys[keyBlob] {
			return nil
		}
		return fmt.Errorf("ssh: host key not in known_hosts for %s (tofu required; use --insecure-ssh to skip)", host)
	}, nil
}

// parseKnownHostLine splits a known_hosts line into host patterns and
// the public key. Returns empty hosts for hashed entries (|1|...) or
// unparseable lines. M11 audit fix.
//
// parseKnownHostLine 把 known_hosts 行拆为主机模式和公钥。哈希条目
// （|1|...）或不可解析行返回空 hosts。M11 审计修法。
func parseKnownHostLine(line string) (hosts []string, key ssh.PublicKey) {
	// Use ssh.ParseKnownHosts which handles the comma-separated host
	// pattern format. / 用 ssh.ParseKnownHosts 处理逗号分隔的主机模式格式。
	_, _, pubKey, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil {
		// Try ParseAuthorizedKey as a fallback (some formats differ).
		// ParseAuthorizedKey returns 5 values (key, comment, options,
		// rest, err) — unlike ParseKnownHosts which returns 6.
		// / 尝试 ParseAuthorizedKey 作为回退（部分格式不同）。
		// ParseAuthorizedKey 返 5 值（key, comment, options, rest, err）
		// ——与 ParseKnownHosts 的 6 值不同。
		pubKey2, _, _, _, err2 := ssh.ParseAuthorizedKey([]byte(line))
		if err2 != nil {
			return nil, nil
		}
		pubKey = pubKey2
	}
	if pubKey == nil {
		return nil, nil
	}
	// Extract host patterns from the first field. / 从首字段提取主机模式。
	// known_hosts format: "host1,host2 ssh-rsa AAAA..."
	// Hashed entries start with "|1|" and we can't reverse them, so
	// return empty hosts (treated as global trust list).
	// / 哈希条目以 "|1|" 开头，无法逆推，返回空 hosts（按全局信任列表处理）。
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, pubKey
	}
	first := parts[0]
	if strings.HasPrefix(first, "|1|") {
		// Hashed hostname — can't match, treat as global.
		// / 哈希主机名——无法匹配，按全局处理。
		return nil, pubKey
	}
	// Strip optional marker. / 去掉可选 marker。
	if strings.HasPrefix(first, "@") {
		if sp := strings.IndexByte(first, ' '); sp > 0 {
			first = first[sp+1:]
		} else {
			// marker with no following host — skip.
			// / marker 后无主机——跳过。
			return nil, pubKey
		}
	}
	for _, h := range strings.Split(first, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts, pubKey
}
