// Package protocols: Docker daemon authenticator.
//
// Strategy: GET /images/json with HTTP Basic auth. 200 = hit, 401 =
// miss. The Docker daemon (/var/run/docker.sock on local) opens
// 2375 (plaintext) and 2376 (TLS). We support both via plain
// http.Client (TLS verification skipped — internal cluster
// convention).
//
// We do NOT issue any privileged call (no container create, no
// image pull, no exec, no volume mount). Just the auth probe.
//
// HARD RULE: on a hit we return. We do NOT enumerate containers,
// pull images, or run anything.
//
// 包 protocols：Docker 守护进程认证器。
// 策略：GET /images/json 加 HTTP Basic auth。200 = 命中，401 = miss。
// Docker 守护进程（本地 /var/run/docker.sock）开 2375（明文）和
// 2376（TLS）。我们通过标准 http.Client 支持两者（跳过 TLS 验证
// ——内网集群惯例）。
//
// 我们不跑任何特权调用（不创建容器、不拉镜像、不 exec、不挂载卷）。
// 只做认证探测。
//
// 硬性原则：命中即返回。不枚举容器、不拉镜像、不跑任何东西。
package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// DockerAuthenticator authenticates against Docker daemon via HTTP
// Basic auth probe. / DockerAuthenticator 通过 HTTP Basic 认证探测
// 对 Docker 守护进程认证。
//
// DefaultPorts returns 2375/2376 (plaintext / TLS Docker daemon).
// / DefaultPorts 返 2375/2376（明文 / TLS Docker daemon）。
type DockerAuthenticator struct{}

// NewDockerAuthenticator returns a default Docker authenticator.
// NewDockerAuthenticator 返回默认配置的 Docker 认证器。
func NewDockerAuthenticator() *DockerAuthenticator { return &DockerAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *DockerAuthenticator) Name() string { return "docker" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *DockerAuthenticator) DefaultPorts() []int {
	return []int{2375, 2376}
}

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit. / Authenticate 实现 credential.Authenticator。
// 按顺序尝试每个 cred；首个命中返回 Hit。
func (a *DockerAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.probe(ctx, addr, c.User, c.Pass, timeout)
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

// probe sends one GET /images/json with Basic auth. Returns
// (true, nil) on a hit, (false, nil) on a miss, (false, err) on
// network failure.
//
// probe 跑一次 GET /images/json 加 Basic auth。命中返 (true, nil)，
// miss 返 (false, nil)，网络错返 (false, err)。
func (a *DockerAuthenticator) probe(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	tr := &http.Transport{
		TLSClientConfig:       transport.TLSConfig(false),
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	// M15: port 2376 is the TLS Docker daemon — use https://. Other
	// ports (2375) stay plaintext http://. / M15：端口 2376 是 TLS
	// Docker daemon——用 https://。其他端口（2375）保持明文 http://。
	scheme := "http"
	if strings.HasSuffix(addr, ":2376") {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/images/json", scheme, addr)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "fg-qimen/0.1")
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 200 = hit. 401 / 403 = miss. / 200 = 命中。401 / 403 = miss。
	return resp.StatusCode == http.StatusOK, nil
}

// init registers the Docker authenticator. / init 注册 Docker 认证器。
func init() {
	credential.Register(NewDockerAuthenticator())
}
