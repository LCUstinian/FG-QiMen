// Package protocols: HTTP form-based credential authenticator.
// Package protocols：HTTP form 凭据认证器。
//
// Strategy: POST a login form with the candidate cred in configurable
// fields. Success is detected by either a configurable substring
// appearing in the response body, OR a redirect to a configurable
// path. / 策略：用候选凭据 POST 一个登录表单到可配置字段。成功通
// 过响应正文出现可配置子串、或重定向到可配置路径来检测。
//
// Designed to mirror fscan's -http form brute-force flag with a
// minimal set of configuration knobs:
// / 设计目标是用最少的配置项复刻 fscan 的 -http form 爆破 flag：
//
//   --http-form-url <URL>          target URL (full), e.g. http://x/login
//   --http-form-fields <a=b,c=d>    form fields; $user$ / $pass$ placeholders
//   --http-form-success <substr>    substring present on success
//   --http-form-failure <substr>    substring present on failure (default "invalid")
//
// All flags are optional. When --http-form-url is empty the
// authenticator is a no-op (every cred returns miss).
// / 所有 flag 都是可选。--http-form-url 为空时认证器为 no-op（每个
// 凭据返回 miss）。
//
// HARD RULE: on a hit we return. We do NOT maintain the resulting
// session cookie, follow authenticated requests, or issue any
// privileged operation. / 硬性原则：命中即返回。不保留返回的
// session cookie，不发后续认证请求，不执行任何特权操作。
package network

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// Package-level configuration. Set by the CLI's
// registerGlobalFlags before RunScan; HTTPFormAuthenticator reads
// these on each attempt. / 包级配置。在 RunScan 前由 CLI 的
// registerGlobalFlags 设置；HTTPFormAuthenticator 每次 attempt 读取。
//
// This avoids plumbing per-target config through the Authenticator
// interface (which has no target/port-aware fields) and keeps the
// package decoupled from types.Config. / 这避免通过 Authenticator 接
// 口（无 target/port 字段）传入 per-target 配置，并保持本包独立于
// types.Config。
var (
	HTTPFormURL      string // full URL, e.g. "http://10.0.0.1/login"
	HTTPFormFields  string // "user=$user$,pass=$pass$,csrf=$(none)"
	HTTPFormSuccess string // substring present in success body (may be empty)
	HTTPFormFailure string // substring present in failure body (default "invalid")
	HTTPFormRedirect string // path substring of success redirect Location (optional)
)

// HTTPFormAuthenticator probes a single HTTP login form. The form
// URL + fields + success/failure markers are package-level config
// set by CLI flags. / HTTPFormAuthenticator 探测单个 HTTP 登录表单。
// 表单 URL + 字段 + 成功/失败标识由 CLI flag 设置为包级配置。
type HTTPFormAuthenticator struct{}

// NewHTTPFormAuthenticator returns a default-configured HTTP form
// authenticator. / NewHTTPFormAuthenticator 返回默认配置的 HTTP
// form 认证器。
func NewHTTPFormAuthenticator() *HTTPFormAuthenticator {
	return &HTTPFormAuthenticator{}
}

func init() { credential.Register(NewHTTPFormAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *HTTPFormAuthenticator) Name() string { return "httpform" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts
// 实现 credential.Authenticator。
// 80 (http) / 443 (https) are the conventional defaults; the
// actual target is dictated by --http-form-url, not the port.
// / 80（http）/ 443（https）是常规默认；实际目标由 --http-form-url
// 决定，不看本字段。
func (a *HTTPFormAuthenticator) DefaultPorts() []int { return []int{80, 443} }

// Authenticate implements credential.Authenticator.
// / Authenticate 实现 credential.Authenticator。
//
// When --http-form-url is empty the authenticator returns (nil, nil)
// for every cred — it is genuinely a no-op until the operator wires
// the form up. / --http-form-url 为空时，本对每个凭据返回 (nil,
// nil)——是真正的 no-op，等操作员配置。
func (a *HTTPFormAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if HTTPFormURL == "" {
		return nil, nil
	}
	if len(creds) == 0 {
		return nil, nil
	}
	failureMarker := HTTPFormFailure
	if failureMarker == "" {
		failureMarker = "invalid"
	}
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, c.User, c.Pass, timeout, failureMarker)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one POST with the candidate cred and inspects the
// response. / attempt 用候选凭据跑一次 POST 并检查响应。
func (a *HTTPFormAuthenticator) attempt(ctx context.Context, user, pass string, timeout time.Duration, failureMarker string) (bool, error) {
	form := url.Values{}
	for _, kv := range splitFields(HTTPFormFields) {
		key, val := kv[0], kv[1]
		val = strings.ReplaceAll(val, "$user$", user)
		val = strings.ReplaceAll(val, "$pass$", pass)
		form.Set(key, val)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, HTTPFormURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "fg-qimen/0.4")
	client := &http.Client{
		Timeout: timeout,
		// Don't follow redirects — a 302 to /dashboard is the hit
		// signal we want raw. / 不跟随跳转——跳到 /dashboard 的 302
		// 是我们要的命中信号。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// Read up to 8 KiB of the body for substring matching. Most login
	// responses fit comfortably; we don't need the full body to
	// detect "invalid"/"failed" markers.
	// / 读最多 8 KiB 正文做子串匹配。大多数登录响应足够；我们不需
	// 要全文来检测 "invalid"/"failed"。
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	// 1. Redirect-based success. / 1. 基于重定向的成功判定。
	if HTTPFormRedirect != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, HTTPFormRedirect) {
			return true, nil
		}
	}
	// 2. Substring-based detection. / 2. 基于子串的检测。
	if HTTPFormSuccess != "" && strings.Contains(body, HTTPFormSuccess) {
		return true, nil
	}
	// 3. Failure marker present? → miss. / 3. 含失败标识？→ miss。
	if failureMarker != "" && strings.Contains(body, failureMarker) {
		return false, nil
	}
	// 4. No failure marker AND no success marker AND status is 200
	// → ambiguous; treat as miss (we won't false-positive). / 4. 无
	// 失败标识且无成功标识且状态码 200 → 歧义，按 miss 处理（避免
	// 误报）。
	if resp.StatusCode == http.StatusOK {
		return false, nil
	}
	// 5. 401/403 = definitely miss. / 5. 401/403 = 明确 miss。
	return false, nil
}

// splitFields splits a "k1=v1,k2=v2" spec into [[k,v],...] tuples.
// / splitFields 把 "k1=v1,k2=v2" 拆成 [[k,v],...] 二元组。
func splitFields(spec string) [][2]string {
	if spec == "" {
		return nil
	}
	parts := strings.Split(spec, ",")
	out := make([][2]string, 0, len(parts))
	for _, p := range parts {
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			continue
		}
		out = append(out, [2]string{
			strings.TrimSpace(p[:eq]),
			strings.TrimSpace(p[eq+1:]),
		})
	}
	return out
}