// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// enum.go — read-only SMB share enumeration (需求B.1). Probes the host
// with an UNAUTHENTICATED session (guest, empty password — go-smb2
// v1.1.0 has no true-anonymous support; guest-with-empty-password is the
// closest supported unauthenticated probe) and lists share names. For
// every share that mounts, directory entries are read via SMB
// QUERY_DIRECTORY — METADATA ONLY. File contents are NEVER read
// (OpenFile is never called with read-through); nothing is written,
// deleted or executed. This is the only sanctioned post-auth surface of
// the smb plugin (plugins.Enumerator), kept strictly apart from the
// authentication-only Credential stub.
//
// enum.go — 只读 SMB 共享枚举（需求B.1）。用未认证会话探测主机
// （guest，空口令——go-smb2 v1.1.0 不支持真匿名；guest+空口令是最接近
// 的受支持未认证探测），列出共享名。每个可挂载的共享，通过 SMB
// QUERY_DIRECTORY 读目录条目——只读元数据。绝不读取文件内容（从不
// 以读穿方式调用 OpenFile）；不写入、不删除、不执行。这是 smb 插件
// 唯一被认可的认证后动作面（plugins.Enumerator），与仅认证的
// Credential stub 严格分离。
package smb

import (
	"context"
	"net"
	"sort"
	"strconv"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Compile-time proof that the smb plugin offers the optional read-only
// enumeration capability. / 编译期证明 smb 插件提供可选只读枚举能力。
var _ plugins.Enumerator = (*Plugin)(nil)

// maxErrString caps error strings written to evidence files — SMB
// status strings can embed long server-supplied paths.
// / 写入证据文件的错误串上限——SMB 状态串可能嵌服务器提供的长路径。
const maxErrString = 120

// Enumerate implements plugins.Enumerator. user/pass are IGNORED: SMB
// enumeration is strictly the unauthenticated probe (a credentialed
// share walk is not offered — creds belong to the auth-only Credential
// path). Returns nil when the server rejects the session or listing
// yields nothing; a successful session with zero listable shares still
// records evidence of an open unauthenticated session.
//
// Enumerate 实现 plugins.Enumerator。忽略 user/pass：SMB 枚举严格只
// 做未认证探测（不提供带凭据的共享遍历——凭据属于仅认证的 Credential
// 路径）。服务器拒绝会话时返回 nil；会话成功但无可列出共享时仍记录
// 未认证会话开放的证据。
func (p *Plugin) Enumerate(ctx context.Context, host string, port int, user, pass string, limits types.EnumLimits) *types.Result {
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
	var dial net.Dialer
	tcpConn, err := dial.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer tcpConn.Close()

	// Unauthenticated session: guest with an empty password. The NTLMv2
	// initiator signs with a null-password key — servers that allow
	// guest/null access accept it; hardened servers reject the Session
	// Setup and we record nothing. / 未认证会话：guest + 空口令。
	// NTLMv2 initiator 用空口令密钥签名——允许 guest/null 访问的服务
	// 器接受；加固的服务器拒绝 Session Setup，我们不记录。
	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{User: "guest", Password: ""},
	}
	s, err := dialer.DialContext(ctx, tcpConn)
	if err != nil {
		return nil
	}
	s = s.WithContext(ctx)
	defer func() { _ = s.Logoff() }()

	res := &types.ShareEnumResult{
		Host:      host,
		Port:      port,
		Anonymous: true, // unauthenticated session succeeded / 未认证会话成功
		Limits:    limits,
		ScanTime:  time.Now(),
	}
	names, err := s.ListSharenames()
	if err != nil {
		// Session OK but the share list itself was refused — still
		// evidence that an unauthenticated session opens. / 会话建立
		// 但共享列表被拒——仍是未认证会话开放的证据。
		res.Shares = []types.ShareInfo{{Error: trimErr(err)}}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "smb",
			Banner:  "unauthenticated session OK; share list refused",
			Extra:   res,
		}
	}
	sort.Strings(names)

	// budget is the TOTAL captured entry count across every share —
	// one huge share cannot starve the others or balloon the file.
	// / budget 是跨所有共享的捕获条目总数——单个巨型共享无法挤占其
	// 他共享或撑爆证据文件。
	budget := limits.MaxEntries
	listable := 0
	for _, name := range names {
		if ctx.Err() != nil {
			res.Truncated = true
			break
		}
		si := types.ShareInfo{Name: name}
		fs, err := s.Mount(name)
		if err != nil {
			si.Access = "none"
			si.Error = trimErr(err)
			res.Shares = append(res.Shares, si)
			continue
		}
		files, trunc := walkShare(ctx, fs, "", 1, limits, &budget)
		_ = fs.Umount()
		si.Files = files
		si.Truncated = trunc
		if trunc {
			res.Truncated = true
		}
		si.Access = "list"
		listable++
		res.Shares = append(res.Shares, si)
	}
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "smb",
		Banner:  "unauthenticated share list: " + strconv.Itoa(listable) + "/" + strconv.Itoa(len(names)) + " listable",
		Extra:   res,
	}
}

// walkShare reads directory entries METADATA-ONLY. Share.ReadDir is
// avoided deliberately: it calls File.Readdir(-1) (unbounded — a huge
// share would accumulate every entry in memory). Open + Readdir(n+1)
// keeps per-directory reads bounded AND detects "there was more" without
// a second query. Subdirectories are descended while depth < maxDepth;
// entries flatten into one list with path-prefixed names. budget drains
// globally across the host.
//
// walkShare 只读目录条目元数据。刻意不用 Share.ReadDir：它调用
// File.Readdir(-1)（无界——巨型共享会把所有条目积进内存）。Open +
// Readdir(n+1) 让单目录读取有界，且无需第二次查询即可判定"还有更
// 多"。depth < maxDepth 时下探子目录；条目以路径前缀名拍平进一个列
// 表。budget 在整个 host 范围内全局消耗。
func walkShare(ctx context.Context, fs *smb2.Share, dir string, depth int, limits types.EnumLimits, budget *int) (files []types.EnumFileInfo, truncated bool) {
	if ctx.Err() != nil {
		return nil, true
	}
	f, err := fs.Open(dir)
	if err != nil {
		// Per-directory read errors are non-fatal — the caller keeps
		// what previous levels captured. / 单目录读错误非致命——调用
		// 方保留之前层级已捕获的内容。
		return nil, false
	}
	defer f.Close()
	// +1 so "there was more" is detectable without a second query.
	// / +1 使"还有更多"无需第二次查询即可判定。
	fis, err := f.Readdir(limits.MaxEntries + 1)
	if err != nil && len(fis) == 0 {
		return nil, false
	}
	if len(fis) > limits.MaxEntries {
		fis = fis[:limits.MaxEntries]
		truncated = true
	}
	sort.Slice(fis, func(i, j int) bool { return fis[i].Name() < fis[j].Name() })

	var subdirs []string
	for _, fi := range fis {
		if ctx.Err() != nil || *budget <= 0 {
			truncated = true
			break
		}
		*budget--
		if fi.IsDir() {
			subdirs = append(subdirs, fi.Name())
		}
		files = append(files, types.EnumFileInfo{
			Name:  fi.Name(),
			Size:  fi.Size(),
			IsDir: fi.IsDir(),
			MTime: fi.ModTime(),
		})
	}
	if depth < limits.MaxDepth {
		for _, name := range subdirs {
			if ctx.Err() != nil || *budget <= 0 {
				truncated = true
				break
			}
			child := name
			if dir != "" {
				child = dir + "\\" + name
			}
			sub, subTrunc := walkShare(ctx, fs, child, depth+1, limits, budget)
			files = append(files, sub...)
			if subTrunc {
				truncated = true
			}
		}
	}
	return files, truncated
}

// trimErr caps an error string for evidence-file stability.
// trimErr 截断错误串以保证证据文件稳定。
func trimErr(err error) string {
	s := err.Error()
	if len(s) > maxErrString {
		s = s[:maxErrString]
	}
	return s
}
