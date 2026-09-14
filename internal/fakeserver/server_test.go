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

// TestServerLoadDelayScalesWithConcurrency is the A3 signal-source
// check: LoadDelay must make per-conn latency grow with the number of
// concurrently-served connections — the "service degrades under load"
// model the AIMD sweep depends on. One connection serves in roughly
// LoadDelay; eight concurrent connections each take ≈ 8×LoadDelay, so
// the slowest must exceed 4×LoadDelay (well clear of scheduler noise
// while still proportional).
// / TestServerLoadDelayScalesWithConcurrency 是 A3 信号源检查：
// LoadDelay 必须让单连接时延随并发在服务连接数增长——AIMD 扫描依
// 赖的"服务随负载退化"模型。单连接约 LoadDelay 完成；8 并发各需
// ≈8×LoadDelay，最慢者必须超过 4×LoadDelay（远离调度噪声且保持比
// 例关系）。
func TestServerLoadDelayScalesWithConcurrency(t *testing.T) {
	const per = 20 * time.Millisecond
	srv, err := NewTCP(Options{LoadDelay: per}, func(c net.Conn) {
		_, _ = c.Write([]byte("hi\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	measure := func(conns int) time.Duration {
		start := time.Now()
		done := make(chan error, conns)
		for i := 0; i < conns; i++ {
			go func() {
				c, err := net.Dial("tcp", srv.Addr())
				if err != nil {
					done <- err
					return
				}
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 8)
				if _, err := c.Read(buf); err != nil {
					done <- err
					return
				}
				done <- nil
			}()
		}
		for i := 0; i < conns; i++ {
			if err := <-done; err != nil {
				t.Fatalf("conn %d: %v", i, err)
			}
		}
		// All conns answered: the elapsed time IS the slowest conn.
		// / 全部应答：耗时即最慢连接。
		return time.Since(start)
	}

	solo := measure(1)
	eight := measure(8)
	if solo < per {
		t.Fatalf("single conn served in %v, want >= LoadDelay %v", solo, per)
	}
	// 8 concurrent each queue behind ≈8 active slots: worst conn must
	// be well beyond the single-conn floor, but the exact multiple is
	// scheduler-dependent — 4× is the loose proportionality bound.
	// / 8 并发各自排在 ≈8 个 active 槽后：最慢连接必须远超单连接下
	// 限，但精确倍数取决于调度——4× 是宽松的比例下界。
	if eight < 4*per {
		t.Fatalf("8-concurrent worst %v, want >= 4x LoadDelay (%v) — LoadDelay is not load-sensitive", eight, 4*per)
	}
}
