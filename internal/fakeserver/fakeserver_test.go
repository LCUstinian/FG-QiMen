package fakeserver

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestListenLoop_RespondsAndCleansUp(t *testing.T) {
	got := make(chan []byte, 1)
	host, port := ListenLoop(t, func(c net.Conn) {
		buf := make([]byte, 1024)
		n, _ := c.Read(buf)
		_, _ = c.Write([]byte("hi: " + string(buf[:n])))
		_ = c.Close()
		got <- buf[:n]
	})
	conn, err := net.Dial("tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(resp) != "hi: ping" {
		t.Errorf("got %q, want %q", resp, "hi: ping")
	}
	select {
	case req := <-got:
		if string(req) != "ping" {
			t.Errorf("handler got %q, want %q", req, "ping")
		}
	case <-time.After(time.Second):
		t.Error("handler never called")
	}
}

func TestListenUDPLoop_RespondsAndCleansUp(t *testing.T) {
	host, port := ListenUDPLoop(t, func(req []byte, _ *net.UDPAddr) []byte {
		out := make([]byte, len(req)+1)
		copy(out, "!")
		copy(out[1:], req)
		return out
	})
	c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(host), Port: port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 16)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != "!hi" {
		t.Errorf("got %q, want %q", buf[:n], "!hi")
	}
}

func TestStartHTTP_RespondsAndCleansUp(t *testing.T) {
	url := StartHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello " + r.URL.Path))
	}))
	resp, err := http.Get(url + "/world")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello /world" {
		t.Errorf("got %q, want %q", body, "hello /world")
	}
}

func TestWriteMagic(t *testing.T) {
	got := WriteMagic([]byte{0xAB, 0xCD}, 2, []byte("xy"))
	want := []byte{0xAB, 0xCD, 0x00, 0x02, 'x', 'y'}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
	got = WriteMagic([]byte{0x01}, 4, []byte{0x02})
	want = []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0x02}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestReadAllAndDiscard(t *testing.T) {
	// Pair server + client with in-memory pipe so the helpers
	// can run against real net.Conn semantics (Read returns EOF
	// when the writer side closes).
	host, port := ListenLoop(t, func(c net.Conn) {
		got := ReadAll(c)
		if string(got) != "abc" {
			t.Errorf("ReadAll got %q, want %q", got, "abc")
		}
		Discard(c) // drains any trailing bytes (none here, should return quickly)
		_, _ = c.Write([]byte("ok"))
	})
	conn, err := net.Dial("tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("abc")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// CloseWrite signals EOF to the server's Read without tearing
	// down the read side, so we can still receive the response.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(resp) != "ok" {
		t.Errorf("got %q, want %q", resp, "ok")
	}
}

// itoa is a tiny strconv-free int-to-string for the host:port
// helper to avoid pulling strconv into test code for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
