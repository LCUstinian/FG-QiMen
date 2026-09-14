// server.go — programmable TCP fake server decoupled from *testing.T.
// Unlike ListenLoop (test-helper contract with t.Cleanup), Server owns
// its lifecycle (Start/Close) so the bench harness can run it inside
// tool processes. Two injection modes serve the A1 bench judge:
//
//   - ModeNormal:    wait ReadDelay±Jitter, then hand the conn to the
//     handler — models network RTT + service think-time.
//   - ModeBlackhole: never answer; drain until the peer gives up —
//     models an OPEN but silent service (the kernel
//     completes the handshake, so the scanner reports
//     open with no banner → the IdentNone bucket and a
//     real active-probe cost). True unreachable hosts
//     (pre-handshake drop) cannot be simulated on the
//     loopback and are out of scope here.
//
// / server.go —— 可编程 TCP 假服务，与 *testing.T 解耦。ListenLoop 是
// 测试助手契约（t.Cleanup），Server 自持生命周期（Start/Close），供
// bench 编排在工具进程内运行。两种注入模式服务 A1 基准裁判：
//
//   - ModeNormal:    等待 ReadDelay±Jitter 后交给 handler —— 模拟
//     网络 RTT + 服务处理耗时。
//   - ModeBlackhole: 永不应答；排水到对端放弃 —— 模拟"开放但沉默"
//     的服务（内核完成握手，扫描器报 open 且无
//     banner → 落入 IdentNone 桶并产生真实的主动探
//     针成本）。握手前丢包的真不可达主机在回环上无
//     法模拟，不在本实现范围内。
package fakeserver

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// ServeMode selects how accepted connections are treated.
// / ServeMode 选择被接受连接的处理方式。
type ServeMode int

const (
	// ModeNormal answers after the injected delay via the handler.
	// / ModeNormal 在注入延迟后由 handler 应答。
	ModeNormal ServeMode = iota

	// ModeBlackhole never answers: it drains the connection until the
	// peer disconnects. The kernel completes the handshake, so probes
	// against this mode read as OPEN-but-silent — the deadline still
	// burns on the read side, exercising the IdentNone path.
	// / ModeBlackhole 永不应答：排水连接直到对端断开。内核完成握手，
	// 对该模式的 probe 表现为 OPEN-but-silent——读侧照常烧满超时，
	// 锻炼 IdentNone 路径。
	ModeBlackhole
)

// Options configures a Server.
// / Options 配置一个 Server。
type Options struct {
	// Mode is ModeNormal by default. / Mode 默认 ModeNormal。
	Mode ServeMode

	// ReadDelay is injected before the handler runs (ModeNormal only).
	// Zero keeps the loopback honest — no artificial latency.
	// / ReadDelay 在 handler 运行前注入（仅 ModeNormal）。零值保持
	// 回环诚实——不人为加延迟。
	ReadDelay time.Duration

	// Jitter randomises ReadDelay by ±Jitter (uniform). Zero disables.
	// / Jitter 对 ReadDelay 做 ±Jitter 均匀扰动。零值关闭。
	Jitter time.Duration
}

// Server is a TCP listener with injection semantics. Create with
// NewTCP, read Addr() for the ephemeral port, Close() when done.
// Safe for concurrent Close.
// / Server 是带注入语义的 TCP 监听器。用 NewTCP 创建，Addr() 读临时
// 端口，结束时 Close()。Close 并发安全。
type Server struct {
	ln      net.Listener
	opts    Options
	handler func(net.Conn)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// NewTCP starts listening on 127.0.0.1:0 and serving connections per
// opts. handler is invoked for ModeNormal connections after the
// injected delay; it owns the connection until it returns.
// / NewTCP 在 127.0.0.1:0 监听并按 opts 服务连接。handler 在注入延
// 迟后被 ModeNormal 连接调用；连接生命周期归 handler。
func NewTCP(opts Options, handler func(net.Conn)) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		ln:      ln,
		opts:    opts,
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
	s.wg.Add(1)
	go s.serveLoop()
	return s, nil
}

// Addr returns the listening address (host:port).
// / Addr 返回监听地址（host:port）。
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Port returns the ephemeral port number.
// / Port 返回临时端口号。
func (s *Server) Port() int {
	if ta, ok := s.ln.Addr().(*net.TCPAddr); ok {
		return ta.Port
	}
	return 0
}

// Close stops the listener and waits for in-flight handlers.
// / Close 停止监听并等待在途 handler。
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.closeErr = s.ln.Close()
		s.wg.Wait()
	})
	return s.closeErr
}

func (s *Server) serveLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return // graceful shutdown / 优雅关停
			default:
			}
			return // listener broken / 监听器失效
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			s.handle(c)
		}(conn)
	}
}

func (s *Server) handle(c net.Conn) {
	switch s.opts.Mode {
	case ModeBlackhole:
		// Never write. Drain until the peer disconnects (its probe
		// deadline expired and it closed) or the server shuts down.
		// / 永不写。排水直到对端断开（其 probe 超时到期后 close）
		// 或服务端关停。
		_, _ = io.Copy(io.Discard, c)
	default:
		if d := s.opts.ReadDelay; d > 0 {
			time.Sleep(s.jittered(d))
		}
		if s.handler != nil {
			s.handler(c)
		}
	}
}

// jittered returns d±Jitter (uniform). Negative results clamp to 0 —
// a "negative delay" is meaningless and time.Sleep would panic.
// / jittered 返回 d±Jitter（均匀分布）。负值钳到 0——"负延迟"无意
// 义且 time.Sleep 会 panic。
func (s *Server) jittered(d time.Duration) time.Duration {
	j := s.opts.Jitter
	if j <= 0 {
		return d
	}
	out := d + time.Duration(randBelow(int64(2*j))) - j
	if out < 0 {
		out = 0
	}
	return out
}

// randBelow returns a uniform int64 in [0, n) drawn from crypto/rand
// (G404: gosec rejects math/rand even for non-security jitter). On
// entropy failure it degrades to 0 — jitter collapses to the plain
// delay and the benchmark stays honest.
// / randBelow 用 crypto/rand 返回 [0, n) 内的均匀 int64（G404：gosec
// 连非安全用途的 math/rand 也拒绝）。熵源失败时退化为 0——抖动塌缩
// 为裸延迟，基准保持诚实。
func randBelow(n int64) int64 {
	if n <= 1 {
		return 0
	}
	lim := ^uint64(0) - ^uint64(0)%uint64(n) // largest multiple of n / n 的最大倍数
	var b [8]byte
	for {
		if _, err := crand.Read(b[:]); err != nil {
			return 0
		}
		if v := binary.LittleEndian.Uint64(b[:]); v < lim {
			return int64(v % uint64(n))
		}
	}
}
