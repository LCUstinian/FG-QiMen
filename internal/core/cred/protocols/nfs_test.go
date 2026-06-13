// nfs_test.go — unit test for the NFS credential authenticator.
// / NFS 凭据认证器的单元测试。
//
// NFS v4 is credential-less in the spec (uses AUTH_SYS / AUTH_NULL
// by default). Real-world NFS often runs behind Kerberos (RPCSEC_GSS)
// which we don't support in v0.1. v0.1: just probe the port responds
// to an empty RPC call. / NFS v4 规范无凭据（默认用 AUTH_SYS / AUTH_NULL）。
// 真实 NFS 经常跑在 Kerberos (RPCSEC_GSS) 后，v0.1 不支持。v0.1：只探
// 端口是否响应空 RPC 调用。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestNFSAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewNFSAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 2049, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestNFSAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewNFSAuthenticator()
	creds := []cred.Cred{{User: "root", Pass: "secret", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 2049, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestNFSAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewNFSAuthenticator()
	creds := []cred.Cred{{User: "root", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
