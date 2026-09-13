// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// enum.go — read-only FTP directory walk (需求B.2). Logs in (anonymous
// probe or a weak-credential hit supplied by the dispatcher), then walks
// the tree via NLST-style LIST commands capturing METADATA ONLY (names,
// sizes, mtimes, dir/file type). File contents are NEVER transferred
// (Retr is never called); nothing is written, renamed or deleted. This
// is the only sanctioned post-auth surface of the ftp plugin
// (plugins.Enumerator), kept strictly apart from the authentication-only
// Credential stub.
//
// enum.go — 只读 FTP 目录遍历（需求B.2）。登录（匿名探测或分发方提供
// 的弱口令命中），通过 LIST 命令遍历目录树，只捕获元数据（文件名、
// 大小、mtime、目录/文件类型）。绝不传输文件内容（从不调用 Retr）；
// 不写入、不重命名、不删除。这是 ftp 插件唯一被认可的认证后动作面
// （plugins.Enumerator），与仅认证的 Credential stub 严格分离。
package ftp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	ftplib "github.com/jlaffaye/ftp"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Compile-time proof that the ftp plugin offers the optional read-only
// enumeration capability. / 编译期证明 ftp 插件提供可选只读枚举能力。
var _ plugins.Enumerator = (*Plugin)(nil)

// Enumerate implements plugins.Enumerator. An empty user means the
// anonymous probe ("anonymous"/"anonymous"); the dispatcher also passes
// a weak-credential hit (user/pass) for the credentialed walk — both are
// read-only tree walks. Returns nil when login fails (no record for
// negative evidence). / Enumerate 实现 plugins.Enumerator。空 user 表示
// 匿名探测（"anonymous"/"anonymous"）；分发方也会传弱口令命中
// （user/pass）做带凭据遍历——两者都是只读树遍历。登录失败返回 nil
// （负面证据不记录）。
func (p *Plugin) Enumerate(ctx context.Context, host string, port int, user, pass string, limits types.EnumLimits) *types.Result {
	if user == "" {
		// Anonymous probe: RFC 959 anonymous login, conventional
		// non-empty password. / 匿名探测：RFC 959 匿名登录，惯用的
		// 非空口令。
		user, pass = "anonymous", "anonymous"
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = 1
	}
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = 200
	}
	if limits.HostTimeout <= 0 {
		limits.HostTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limits.HostTimeout)
	defer cancel()

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := ftplib.Dial(addr, ftplib.DialWithContext(ctx))
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Quit() }()

	if err := conn.Login(user, pass); err != nil {
		// Access denied — the walk is not possible, no record.
		// 访问被拒——无法遍历，不记录。
		return nil
	}

	res := &types.FTPEnumResult{
		Host:      host,
		Port:      port,
		Anonymous: isAnonymousUser(user),
		User:      user,
		Limits:    limits,
		ScanTime:  time.Now(),
	}
	// budget is the TOTAL captured entry count across the whole tree —
	// one huge directory cannot starve the rest or balloon the file.
	// / budget 是整棵树的捕获条目总数——单个巨型目录无法挤占其余部
	// 分或撑爆证据文件。
	budget := limits.MaxEntries
	dirs, trunc := walkFTP(ctx, conn, "/", 1, limits, &budget)
	res.Dirs = dirs
	res.Truncated = trunc
	if len(res.Dirs) == 0 {
		// Defensive: even a refused root LIST records the dir row with
		// the error, so this only fires on a nil walk. / 防御性：即使
		// 根目录 LIST 被拒也会记录带错误的目录行，这里只在空遍历时
		// 触发。
		return nil
	}
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "ftp",
		Banner:  fmt.Sprintf("FTP walk as %s: dirs=%d truncated=%v", user, len(res.Dirs), res.Truncated),
		Extra:   res,
	}
}

// walkFTP lists one directory level and recurses while depth < maxDepth.
// Each visited directory becomes one FTPDir row (path + flat file list);
// per-directory LIST errors are recorded on that row, non-fatal. budget
// drains globally across the tree. / walkFTP 列出单层目录并在
// depth < maxDepth 时递归。每个访问过的目录成为一行 FTPDir（路径 +
// 扁平文件列表）；单目录 LIST 错误记录在该行上、非致命。budget 在整
// 棵树范围内全局消耗。
func walkFTP(ctx context.Context, conn *ftplib.ServerConn, path string, depth int, limits types.EnumLimits, budget *int) (dirs []types.FTPDir, truncated bool) {
	if ctx.Err() != nil {
		return nil, true
	}
	entries, err := conn.List(path)
	if err != nil {
		// Refused / unreadable directory: record the row with the error
		// — for the anonymous walk this IS the finding (path exists,
		// listing denied). / 目录被拒 / 不可读：记录带错误的行——对匿
		// 名遍历这就是发现（路径存在，列表被拒）。
		return []types.FTPDir{{Path: path, Error: err.Error()}}, false
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	dir := types.FTPDir{Path: path}
	var subdirs []string
	for _, e := range entries {
		if ctx.Err() != nil || *budget <= 0 {
			dir.Truncated = true
			break
		}
		*budget--
		isDir := e.Type == ftplib.EntryTypeFolder
		if isDir {
			subdirs = append(subdirs, e.Name)
		}
		dir.Files = append(dir.Files, types.EnumFileInfo{
			Name:  e.Name,
			Size:  int64(e.Size),
			IsDir: isDir,
			MTime: e.Time,
		})
	}
	dirs = append(dirs, dir)
	if dir.Truncated {
		truncated = true
	}
	if depth < limits.MaxDepth {
		for _, name := range subdirs {
			if ctx.Err() != nil || *budget <= 0 {
				truncated = true
				break
			}
			sub, subTrunc := walkFTP(ctx, conn, childPath(path, name), depth+1, limits, budget)
			dirs = append(dirs, sub...)
			if subTrunc {
				truncated = true
			}
		}
	}
	return dirs, truncated
}

// childPath joins an FTP path ("/" root, forward-slash separated).
// childPath 拼接 FTP 路径（"/" 根，正斜杠分隔）。
func childPath(parent, name string) string {
	if parent == "" || parent == "/" {
		return "/" + name
	}
	return strings.TrimSuffix(parent, "/") + "/" + name
}

// isAnonymousUser reports whether the login is the anonymous account
// (either of the two conventional names). / isAnonymousUser 报告登录是
// 否为匿名账号（两个惯用名都算）。
func isAnonymousUser(user string) bool {
	return user == "anonymous" || user == "ftp"
}
