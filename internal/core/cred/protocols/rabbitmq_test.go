// rabbitmq_test.go — unit test for the RabbitMQ credential authenticator.
// / RabbitMQ 凭据认证器的单元测试。
//
// Scope: empty creds, non-password method, conn refused. The full
// AMQP 0-9-1 PLAIN handshake (with frame-level parsing of
// Connection.Start, Start-Ok, Connection.Tune) is exercised by the
// v0.1 end-to-end smoke against a real RabbitMQ broker (obs #886).
// In-process AMQP frame mocking is fragile (kernel-level TCP
// timing + frame boundary race conditions on Windows) and not
// worth the ceremony for testing our framing — AMQP 0-9-1 framing
// is the well-tested public spec. / 范围：空 creds、非 password 方法、
// 连接拒绝。完整 AMQP 0-9-1 PLAIN 握手（帧级解析 Connection.Start /
// Start-Ok / Connection.Tune）由 v0.1 端到端 smoke 对真 RabbitMQ
// broker（obs #886）测。进程内 AMQP 帧 mock 脆弱（kernel TCP
// 时序 + 帧边界竞态，Windows 上尤其）且不值得为测我们的帧编解码而
// 复杂——AMQP 0-9-1 帧是公开规范，go-streadway/amqp 等库已是真权威。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestRabbitMQAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewRabbitMQAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5672, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestRabbitMQAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewRabbitMQAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5672, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestRabbitMQAuthenticator_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewRabbitMQAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
