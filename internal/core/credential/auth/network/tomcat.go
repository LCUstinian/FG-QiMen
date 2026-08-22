// Package protocols: Tomcat Manager credential authenticator.
// Package protocols：Tomcat Manager 凭据认证器。
//
// Strategy: send a GET /manager/html with the candidate cred in
// HTTP Basic Authorization header. Tomcat replies 401 if the cred
// is wrong, 200 (or 302 to /manager/html) on success.
// / 策略：发 GET /manager/html 带候选凭据的 HTTP Basic Authorization
// 头。Tomcat 在凭据错时回 401，成功回 200（或 302 跳到 /manager/html）。
//
// HARD RULE: on a hit we return. We do NOT deploy any WAR, list
// applications, or invoke any manager endpoint. / 硬性原则：命中即
// 返回。不部署任何 WAR、不列应用、不调任何 manager 端点。
package network

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// TomcatAuthenticator authenticates against Tomcat Manager via
// HTTP Basic. / TomcatAuthenticator 通过 HTTP Basic 对 Tomcat
// Manager 认证。
type TomcatAuthenticator struct{}

// NewTomcatAuthenticator returns a default-configured Tomcat
// authenticator. / NewTomcatAuthenticator 返回默认配置的 Tomcat 认证器。
func NewTomcatAuthenticator() *TomcatAuthenticator { return &TomcatAuthenticator{} }

func init() { credential.Register(NewTomcatAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *TomcatAuthenticator) Name() string { return "tomcat" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts
// 实现 credential.Authenticator。
// 8080 (default http), 8443 (default https), 80 / 443 are also
// common when Tomcat is fronted by nginx/apache. / 8080（默认
// http）、8443（默认 https）、80/443 在 nginx/apache 反代时也常见。
func (a *TomcatAuthenticator) DefaultPorts() []int { return []int{8080, 8443, 80, 443} }

// Authenticate implements credential.Authenticator.
// / Authenticate 实现 credential.Authenticator。
func (a *TomcatAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	scheme := "http"
	if port == 443 || port == 8443 {
		scheme = "https"
	}
	target := fmt.Sprintf("%s://%s:%d/manager/html", scheme, host, port)
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, target, c.User, c.Pass, timeout)
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

// attempt runs one GET /manager/html with the candidate cred in
// HTTP Basic. / attempt 用候选凭据的 HTTP Basic 发一次 GET
// /manager/html。
func (a *TomcatAuthenticator) attempt(ctx context.Context, target, user, pass string, timeout time.Duration) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("User-Agent", "fg-qimen/0.4")
	client := &http.Client{
		Timeout: timeout,
		// Don't follow redirects — a 302 to /manager/html is a hit
		// signal we want to see raw. / 不跟随跳转 — 跳到
		// /manager/html 的 302 是命中信号，我们要看原始状态码。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 200 (or 302) = auth ok; 401 = auth failed; anything else = not
	// a Tomcat manager (e.g. 403 if manager is locked down, 404).
	// / 200（或 302）= 鉴权成功；401 = 鉴权失败；其他 = 不是
	// Tomcat manager（如 403 manager 已锁定、404 不存在）。
	switch resp.StatusCode {
	case http.StatusOK, http.StatusFound:
		return true, nil
	default:
		return false, nil
	}
}

// init registers the Tomcat authenticator. / init 注册 Tomcat 认证器。
// (The init() at the top of the file already registers it; this
// block is intentionally absent to avoid duplicate-init compile
// errors.) /（文件顶部已有 init 注册；本块故意省略以避免 init 重
// 复导致的编译错误。）