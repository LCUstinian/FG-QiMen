package fakeserver

import (
	"net"
	"testing"
	"time"
)

func TestServerNormalDelay(t *testing.T) {
	const delay = 100 * time.Millisecond
	srv, err := NewTCP(Options{ReadDelay: delay}, func(c net.Conn) {
		_, _ = c.Write([]byte("hi\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	start := time.Now()
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buf := make([]byte, 8)
	if _, err := conn.Read(buf); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < delay {
		t.Fatalf("response arrived in %v, want >= injected delay %v", elapsed, delay)
	}
}

func TestServerNoDelayByDefault(t *testing.T) {
	srv, err := NewTCP(Options{}, func(c net.Conn) {
		_, _ = c.Write([]byte("hi\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("zero-delay response failed: %v", err)
	}
}

func TestServerBlackholeSilence(t *testing.T) {
	srv, err := NewTCP(Options{Mode: ModeBlackhole}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// The server must not answer; a short read deadline should expire.
	// / 服务端不应答；短读超时应当到期。
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 8)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("blackhole answered with %d bytes, want silence", n)
	}
}

func TestServerCloseStopsListener(t *testing.T) {
	srv, err := NewTCP(Options{}, func(c net.Conn) {})
	if err != nil {
		t.Fatal(err)
	}
	addr := srv.Addr()
	if err := srv.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Idempotent close. / Close 幂等。
	if err := srv.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := net.Dial("tcp", addr); err == nil {
		t.Fatal("dial succeeded after Close, want refusal")
	}
}
