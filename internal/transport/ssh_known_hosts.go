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
// loadKnownHostsCallback 读 known_hosts 格式文件并返回接受文件中任
// 意 key 的 HostKeyCallback。仅接受"主机模式,key-type,base64-key"
// 行；注释与空行跳过。带哈希主机的行（|1|salt|hash 形式）原样接受。
//
// Note: ssh.FixedHostKey (in golang.org/x/crypto/ssh) takes a single
// key, not a variadic list; to accept "any of N keys" we wrap it in
// a closure that checks key membership in our loaded set via
// ssh.PublicKey.Marshal() comparison.
//
// 注：ssh.FixedHostKey（golang.org/x/crypto/ssh）接受单个 key 而非
// variadic；要"接受 N 个 key 中任一"，我们用闭包配合
// ssh.PublicKey.Marshal() 字节比较来检查 key 隶属关系。
func loadKnownHostsCallback(path string) (ssh.HostKeyCallback, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open known_hosts %s: %w", path, err)
	}
	defer f.Close()

	// known: a map from key.Marshal() bytes to true, for O(1) lookup
	// at callback time. We use the wire form (Type || Blob) as the
	// canonical identity — two parsed PublicKey values compare equal
	// iff their marshalled bytes are equal.
	//
	// known：以 key.Marshal() 字节为键的 map，callback 时 O(1) 查
	// 找。用 wire 形态（Type || Blob）作规范化身份——两个解析出的
	// PublicKey 相等当且仅当它们的 marshal 字节相等。
	known := make(map[string]bool)
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
		k := parseKnownHostKey(line)
		if k != nil {
			known[string(k.Marshal())] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read known_hosts %s: %w", path, err)
	}
	if len(known) == 0 {
		return nil, fmt.Errorf("known_hosts %s has no usable entries", path)
	}
	// Wrap the membership check in a HostKeyCallback. The host pattern
	// match (the first arg) is ignored here because we only care about
	// the key — operator-supplied known_hosts is a "trust list" not a
	// host-to-key binding. (ssh's FixedHostKey also ignores the host
	// arg for the same reason.)
	//
	// 闭包包装成员关系检查。主机模式匹配（第一个参数）这里被忽略——
	// 我们只关心 key，操作员提供的 known_hosts 是"信任列表"而非
	// 主机到 key 的绑定。（ssh 的 FixedHostKey 出于同样原因也忽略
	// host 参数。）
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if key == nil {
			return fmt.Errorf("ssh: nil server host key")
		}
		if known[string(key.Marshal())] {
			return nil
		}
		return fmt.Errorf("ssh: host key not in known_hosts (tofu required; use --insecure-ssh to skip)")
	}, nil
}

// parseKnownHostKey parses a single known_hosts line and returns the
// public key, or nil on parse error. ssh.ParseKnownHosts returns
// 6 values (marker, hosts, pubKey, comment, rest, err); we only
// care about pubKey.
//
// parseKnownHostKey 解析单行 known_hosts 并返回公钥；解析错误返
// 回 nil。ssh.ParseKnownHosts 返回 6 个值（marker, hosts, pubKey,
// comment, rest, err）；我们只关心 pubKey。
func parseKnownHostKey(line string) ssh.PublicKey {
	_, _, pubKey, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil {
		return nil
	}
	return pubKey
}
