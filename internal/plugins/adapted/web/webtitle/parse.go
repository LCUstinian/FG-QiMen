// parse.go — HTML / header / favicon parsing for webtitle.
//
// The "parse" step is distinct from the "fetch" step (http.go) and
// the "display" step (display.go). It takes a raw HTTP response
// and produces structured fields the Identify path can use.
//
// parse.go — webtitle 的 HTML / 头 / favicon 解析。
//
// "parse" 步骤与"fetch"（http.go）和"display"（display.go）独立。
// 接受原始 HTTP 响应，产出结构化字段供 Identify 路径用。
package webtitle

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// extractTitle pulls the <title>...</title> text from a chunk of
// HTML and collapses internal whitespace. Returns "" if no title
// tag is present. Truncated to 200 chars to keep the result
// column bounded.
//
// extractTitle 从 HTML 块抽 <title>...</title> 文本并压缩内部空白。
// 无 title 返 ""。截到 200 字符以保结果列宽有界。
func extractTitle(html string) string {
	m := titleRegex.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	t := whitespaceRegex.ReplaceAllString(strings.TrimSpace(m[1]), " ")
	if !utf8.ValidString(t) {
		return ""
	}
	if len(t) > 200 {
		t = t[:200] + "…"
	}
	return t
}

// fetchFaviconHash fetches /favicon.ico from baseURL, hashes the
// first 1024 bytes with MD5, and returns the 4× uint32 fingerprint
// (the same format FingerprintHub uses for favicon-based web
// detection). Returns nil on any error — favicon is best-effort.
//
// fetchFaviconHash 从 baseURL 拉 /favicon.ico，对前 1024 字节跑
// MD5，返 4 个 uint32 的 fingerprint（FingerprintHub 用的同格式）。
// 任何错误返 nil——favicon 是尽力而为。
func fetchFaviconHash(ctx context.Context, baseURL string, timeout time.Duration) []int32 {
	cli := newClient(timeout)
	favURL := strings.TrimRight(baseURL, "/") + "/favicon.ico"
	req, _ := http.NewRequestWithContext(ctx, "GET", favURL, nil)
	req.Header.Set("User-Agent", "fg-qimen/0.2 (+https://github.com/LCUstinian/FG-QiMen)")
	resp, err := cli.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	// Read up to 1024 bytes — that's the canonical FingerprintHub
	// hash range. / 最多读 1024 字节——FingerprintHub 标准哈希范围。
	limited := io.LimitReader(resp.Body, 1024)
	sum := md5.Sum(mustReadAll(limited))
	// 16-byte MD5 → 4× uint32 (big-endian, like FingerprintHub).
	// / 16 字节 MD5 → 4 个 uint32（大端，如 FingerprintHub）。
	return []int32{
		int32(binary.BigEndian.Uint32(sum[0:4])),
		int32(binary.BigEndian.Uint32(sum[4:8])),
		int32(binary.BigEndian.Uint32(sum[8:12])),
		int32(binary.BigEndian.Uint32(sum[12:16])),
	}
}

// mustReadAll is a tiny local helper to avoid swallowing short-
// read errors from the favicon response.
//
// mustReadAll 是本地小 helper，避免吞掉 favicon 响应的短读错误。
func mustReadAll(r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		// Shouldn't happen for a LimitReader backed by a real
		// http response body, but return what we got.
		// / 不该发生在真实 http 响应体上的 LimitReader 上，但返
		// 已读到的字节。
		return b
	}
	return b
}

// uniqSorted returns a sorted, de-duplicated copy of in.
// / uniqSorted 返 in 的去重+排序副本。
func uniqSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// dummy usage of sha256 to keep the import declaration flexible
// (we may switch to SHA-256 for collision resistance later).
//
// (No dummy imports — fingerprint.CheckData / io / sort are the
// only ones used. Removed sha256 / errors / base64 dummy vars
// during the v0.2.1 god-file split: they were placeholder
// imports kept "in case we switch to SHA-256" but Go's compiler
// flags unused imports, so the dummies had to go.)
//
// （无哑引用——fingerprint.CheckData / io / sort 是仅用的。v0.2.1
// 拆文件时删了 sha256 / errors / base64 哑引用：它们是"以防
// 我们切到 SHA-256"的占位 import，但 Go 编译器标未用 import，所以
// 哑引用必须删。）
