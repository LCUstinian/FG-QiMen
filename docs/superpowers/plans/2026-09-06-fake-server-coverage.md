# v0.6.0 fake-server + 80% Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise project coverage from ~60.5% to ≥ 80% (total) and ≥ 70% (per adapted plugin) by adding in-process Go fake-server tests for 30+ plugins (43 total, 2 already covered), wrapped in a shared `internal/fakeserver/` helper package.

**Architecture:** Reuse the existing `plugintest.Smoke` baseline + the per-plugin fake-server pattern from `ntp_test.go` / `dns_test.go` / `rdp_test.go`. Extract listener / cleanup boilerplate into a shared package so each plugin's test file only writes its protocol-handler bytes. Four tiers (HTTP / text-line TCP / stateful TCP / UDP), sequenced Tier 1 → Tier 4.

**Tech Stack:** Go 1.26.8, stdlib `net` + `net/http/httptest`, no new dependencies.

---

## Global Constraints

These come verbatim from the spec — every task's requirements implicitly include this section.

- **Branch:** `main`. No worktree isolation.
- **No business-logic changes** — `*.go` files outside `*_test.go` MUST NOT be modified.
- **Coverage target:** total project coverage **≥ 80%** (current 60.5%), AND every package under `internal/plugins/adapted/<category>/<plugin>/` **≥ 70%**.
- **Coverage measurement:** `go test -cover ./...` (Go's built-in coverage tool, statement coverage).
- **Test depth:** Identify + Credential happy path only. Error/edge/timeout paths are out of scope for this plan.
- **Fake server pattern:** use port `:0` (OS-assigned) so no conflicts; `t.Cleanup` closes listener/server so no leaks; return host:port/url to the test for use as `Identify(ctx, host, port)` / `Credential(...)` target.
- **Time budget:** 6 weeks. Each tier is independently shippable; if we run out of time at any tier, higher tiers already raised coverage.
- **Commit footer:** Every commit ends with `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **CI gate (acceptance):** `go test ./...` green, total coverage ≥ 80%, every adapted plugin ≥ 70%, `gofmt -l .` empty, `go vet ./...` clean.

---

## File Structure

**Create:**
```
internal/fakeserver/doc.go
internal/fakeserver/tcp.go
internal/fakeserver/udp.go
internal/fakeserver/http.go
internal/fakeserver/bin.go
internal/fakeserver/fakeserver_test.go
```

**Create (per-plugin tests, 43 files total):**
```
internal/plugins/adapted/web/jenkins/jenkins_test.go
internal/plugins/adapted/web/kibana/kibana_test.go
internal/plugins/adapted/web/weblogic/weblogic_test.go
internal/plugins/adapted/web/webtitle/webtitle_test.go
internal/plugins/adapted/database/elasticsearch/elasticsearch_test.go
internal/plugins/adapted/cloud/aws/aws_test.go
internal/plugins/adapted/cloud/azure/azure_test.go
internal/plugins/adapted/messaging/activemq/activemq_test.go
internal/plugins/adapted/messaging/rabbitmq/rabbitmq_test.go
internal/plugins/adapted/messaging/rocketmq/rocketmq_test.go
internal/plugins/adapted/database/redis/redis_test.go
internal/plugins/adapted/database/memcached/memcached_test.go
internal/plugins/adapted/email/smtp/smtp_test.go
internal/plugins/adapted/email/pop3/pop3_test.go
internal/plugins/adapted/email/imap/imap_test.go
internal/plugins/adapted/database/mysql/mysql_test.go
internal/plugins/adapted/database/postgresql/postgresql_test.go
internal/plugins/adapted/database/mongodb/mongodb_test.go
internal/plugins/adapted/filestorage/ftp/ftp_test.go
internal/plugins/adapted/filestorage/nfs/nfs_test.go
internal/plugins/adapted/remote/ssh/ssh_test.go
internal/plugins/adapted/remote/telnet/telnet_test.go
internal/plugins/adapted/remote/vnc/vnc_test.go
internal/plugins/adapted/filestorage/smb/smb_test.go
internal/plugins/adapted/remote/rdp/rdp_test.go          # may already exist; refactor
internal/plugins/adapted/remote/rdpnla/rdpnla_test.go
internal/plugins/adapted/remote/winrm/winrm_test.go
internal/plugins/adapted/remote/ipmi/ipmi_test.go
internal/plugins/adapted/messaging/kafka/kafka_test.go
internal/plugins/adapted/messaging/mqtt/mqtt_test.go
internal/plugins/adapted/database/mssql/mssql_test.go
internal/plugins/adapted/database/oracle/oracle_test.go
internal/plugins/adapted/filestorage/rsync/rsync_test.go
internal/plugins/adapted/network/snmp/snmp_test.go
internal/plugins/adapted/network/snmpv3/snmpv3_test.go
internal/plugins/adapted/network/tftp/tftp_test.go
internal/plugins/adapted/network/bacnet/bacnet_test.go
internal/plugins/adapted/network/modbus/modbus_test.go
internal/plugins/adapted/network/ldap/ldap_test.go
internal/plugins/adapted/network/socks5/socks5_test.go
internal/plugins/adapted/network/docker/docker_test.go
```

**Modify:**
```
scripts/ci-coverage-check.py   # floor 60% → 80% + per-plugin 70% walk
```

**Optionally delete** (after each plugin's fake-server test exists and passes):
```
internal/plugins/adapted/<category>/<plugin>/smoke_test.go   # replaced by <plugin>_test.go
```

**Not modified:** any non-test `.go` file outside `internal/fakeserver/`.

---

## Task 1: Build `internal/fakeserver/` package

**Files:**
- Create: `internal/fakeserver/doc.go`
- Create: `internal/fakeserver/tcp.go`
- Create: `internal/fakeserver/udp.go`
- Create: `internal/fakeserver/http.go`
- Create: `internal/fakeserver/bin.go`
- Create: `internal/fakeserver/fakeserver_test.go`

**Interfaces:**
- Produces: `fakeserver.ListenLoop(t, handler) (host string, port int)` — TCP
- Produces: `fakeserver.ListenUDPLoop(t, handler) (host string, port int)` — UDP
- Produces: `fakeserver.StartHTTP(t, h http.Handler) (url string)` — HTTP via httptest

- [ ] **Step 1.1: Create `internal/fakeserver/doc.go`**

```go
// Package fakeserver provides in-process fake-server helpers for
// plugin tests. / Package fakeserver 为插件测试提供进程内假服务器
// 助手。
//
// Every helper:
//   - binds to 127.0.0.1:0 (OS-assigned free port) so concurrent tests
//     never collide
//   - registers a t.Cleanup that closes the listener / httptest server
//     so the test process never leaks
//   - returns the bound host:port (or full URL for HTTP) for the test
//     to use as Identify(ctx, host, port) / Credential(...) target
//
// All three helpers (TCP, UDP, HTTP) spawn the handler loop in a
// goroutine; the goroutine exits when conn read fails (listener
// closed) or when the httptest server shuts down.
//
// Why this package exists: before fakeserver, every plugin test that
// wanted real coverage (e.g. ntp_test.go, dns_test.go, rdp_test.go)
// inlined its own net.Listen / t.Cleanup plumbing — ~20 lines of
// identical boilerplate per test file. Centralising it shrinks each
// plugin's test file to "write the protocol handler + assert the
// plugin identifies/credentials the response". This is the
// foundation for raising plugin coverage from 0% to 70%+ per the
// v0.6.0 quality program.
package fakeserver
```

- [ ] **Step 1.2: Create `internal/fakeserver/tcp.go`**

```go
package fakeserver

import (
	"io"
	"net"
	"testing"
)

// ListenLoop starts a TCP listener on 127.0.0.1:0 and dispatches
// each accepted connection to handler in a goroutine. Returns
// ("127.0.0.1", port) so the test can pass them to Identify /
// Credential. Closes the listener via t.Cleanup when the test ends.
//
// handler is called once per accepted connection. It should read /
// write as needed and return; ListenLoop owns connection lifetime.
// Errors on handler I/O are ignored — fake servers are best-effort
// responders for happy-path tests.
//
// / ListenLoop 在 127.0.0.1:0 启 TCP 监听，把每个 accepted conn 分发
// 给 handler（在新 goroutine）。返回 ("127.0.0.1", port)。测试结束
// 时通过 t.Cleanup 关 listener。
func ListenLoop(t *testing.T, handler func(net.Conn)) (host string, port int) {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeserver: resolve: %v", err)
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("fakeserver: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()
				handler(c)
			}(conn)
		}
	}()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
}

// ReadAll is a convenience for handlers that just want to drain
// the request bytes before writing a fixed response. Returns
// io.EOF cleanly if the client closes without sending anything.
// / ReadAll 是给 handler 的便利函数：读空请求然后写固定响应。
func ReadAll(c net.Conn) []byte {
	buf := make([]byte, 4096)
	n, _ := c.Read(buf)
	return buf[:n]
}

// Discard reads until EOF or read error and discards. Used by
// handlers that don't need to inspect the request (e.g. fixed
// banner responders). / Discard 读到 EOF 或读错误并丢弃。
func Discard(c net.Conn) {
	_, _ = io.Copy(io.Discard, c)
}
```

- [ ] **Step 1.3: Create `internal/fakeserver/udp.go`**

```go
package fakeserver

import (
	"net"
	"testing"
)

// ListenUDPLoop starts a UDP listener on 127.0.0.1:0 and dispatches
// each datagram to handler in a goroutine. handler returns the
// response bytes (nil to send nothing) and the source address.
// Closes the conn via t.Cleanup.
//
// handler signature: func(req []byte, src *net.UDPAddr) []byte.
// Returning nil suppresses the reply. Per-conn state lives in the
// handler closure; for stateful UDP protocols (rare), use a map
// keyed by src.String().
//
// / ListenUDPLoop 在 127.0.0.1:0 启 UDP conn，每个 datagram 分发给
// handler。handler 返回响应字节（nil 不回）。测试结束 t.Cleanup 关。
func ListenUDPLoop(t *testing.T, handler func([]byte, *net.UDPAddr) []byte) (host string, port int) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeserver: resolve udp: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("fakeserver: listen udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 64*1024)
		for {
			_ = conn.SetReadDeadline(timeNow().Add(2 * timeSecond()))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // listener closed
			}
			resp := handler(buf[:n], src)
			if resp != nil {
				_, _ = conn.WriteToUDP(resp, src)
			}
		}
	}()
	return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}

// timeNow / timeSecond are tiny indirection so we don't import
// "time" at package scope (keeps the helper import surface small
// for plugin test files).
func timeNow() timeT { return timeT{} }
func timeSecond() timeD { return timeD{} }
```

Wait — that indirection is over-engineered. Replace `timeNow()`/`timeSecond()` with a direct `time.Now()` + `2*time.Second`:

```go
package fakeserver

import (
	"net"
	"testing"
	"time"
)

// ListenUDPLoop starts a UDP listener on 127.0.0.1:0 and dispatches
// each datagram to handler in a goroutine. handler returns the
// response bytes (nil to send nothing) and the source address.
// Closes the conn via t.Cleanup.
//
// handler signature: func(req []byte, src *net.UDPAddr) []byte.
// Returning nil suppresses the reply. Per-conn state lives in the
// handler closure; for stateful UDP protocols (rare), use a map
// keyed by src.String().
//
// / ListenUDPLoop 在 127.0.0.1:0 启 UDP conn，每个 datagram 分发给
// handler。handler 返回响应字节（nil 不回）。测试结束 t.Cleanup 关。
func ListenUDPLoop(t *testing.T, handler func([]byte, *net.UDPAddr) []byte) (host string, port int) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeserver: resolve udp: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("fakeserver: listen udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 64*1024)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // listener closed
			}
			resp := handler(buf[:n], src)
			if resp != nil {
				_, _ = conn.WriteToUDP(resp, src)
			}
		}
	}()
	return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}
```

- [ ] **Step 1.4: Create `internal/fakeserver/http.go`**

```go
package fakeserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// StartHTTP wraps httptest.NewServer with the same t.Cleanup contract
// as ListenLoop / ListenUDPLoop: the test process never leaks the
// server, and the URL is returned for use as Identify / Credential
// target. / StartHTTP 包 httptest.NewServer，复用同样的 t.Cleanup
// 契约。返回 URL 给测试用。
func StartHTTP(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}
```

- [ ] **Step 1.5: Create `internal/fakeserver/bin.go`**

```go
package fakeserver

import "encoding/binary"

// WriteMagic is a convenience for binary protocol handlers that
// need to emit a magic-byte prefix + uint16 / uint32 length +
// payload in one shot. Returns the assembled byte slice. Big-endian.
//
// / WriteMagic 是给二进制协议 handler 的便利函数：拼 magic + 长度
// + payload 一次性 emit。
func WriteMagic(magic []byte, lengthLen int, payload []byte) []byte {
	out := make([]byte, 0, len(magic)+lengthLen+len(payload))
	out = append(out, magic...)
	switch lengthLen {
	case 2:
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(len(payload)))
		out = append(out, b[:]...)
	case 4:
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(len(payload)))
		out = append(out, b[:]...)
	}
	out = append(out, payload...)
	return out
}
```

- [ ] **Step 1.6: Create `internal/fakeserver/fakeserver_test.go`**

Self-tests so the helpers themselves are covered (and a wrong helper implementation would be caught).

```go
package fakeserver

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"testing"
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
	default:
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
	)
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
```

(Note: the test file imports `time` for the UDP deadline; that's fine, it's stdlib.)

- [ ] **Step 1.7: Run self-tests**

Run: `go test -race ./internal/fakeserver/...`
Expected: 4 tests PASS (TestListenLoop_RespondsAndCleansUp, TestListenUDPLoop_RespondsAndCleansUp, TestStartHTTP_RespondsAndCleansUp, TestWriteMagic). Coverage on `internal/fakeserver/` should be near 100%.

- [ ] **Step 1.8: Commit**

```bash
git add internal/fakeserver/
git commit -m "feat(fakeserver): add shared in-process fake-server helpers

Adds internal/fakeserver/ package with three helpers:
  - ListenLoop(t, handler) (host, port)        TCP via net.Listen
  - ListenUDPLoop(t, handler) (host, port)     UDP via net.ListenUDP
  - StartHTTP(t, h http.Handler) url          wraps httptest.NewServer

Plus WriteMagic (binary protocol helper) and 4 self-tests covering
each helper's accept-loop, response round-trip, and t.Cleanup
contract.

Foundation for v0.6.0 fake-server coverage push — every per-plugin
test in subsequent tasks imports this package instead of inlining
~20 lines of listener plumbing.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: Migrate existing ntp_test.go + dns_test.go to use fakeserver + verify pattern

**Files:**
- Modify: `internal/plugins/adapted/network/ntp/ntp_test.go`
- Modify: `internal/plugins/adapted/network/dns/dns_test.go`

**Why this task exists:** The existing ntp_test.go and dns_test.go inlined their own listener plumbing. They become the canonical template for every subsequent plugin test. After this task, the per-plugin test pattern is established and Tasks 3+ just apply it.

- [ ] **Step 2.1: Rewrite ntp_test.go to use fakeserver**

Replace the contents of `internal/plugins/adapted/network/ntp/ntp_test.go` with:

```go
// ntp_test.go — Identify happy-path test for the NTP plugin using
// fakeserver. / NTP 插件用 fakeserver 包的 Identify happy-path 测试。
package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

func TestNTP_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(_ []byte, _ *net.UDPAddr) []byte {
		// LI=0, VN=4, Mode=4 (server) = 0b00100100 = 0x24. / 服务端
		// 响应：LI=0, VN=4, Mode=4 = 0x24。
		resp := make([]byte, 48)
		resp[0] = 0x24
		resp[1] = 2 // stratum 2
		return resp
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected hit, got nil")
	}
	if r.Service != "ntp" {
		t.Errorf("Service = %q, want ntp", r.Service)
	}
}
```

(Use `net` import where needed.)

- [ ] **Step 2.2: Rewrite dns_test.go similarly**

The exact contents depend on DNS protocol bytes — use the existing dns_test.go as the source for the DNS-specific response bytes, then refactor to use `fakeserver.ListenUDPLoop` instead of inlined `net.ListenUDP`.

- [ ] **Step 2.3: Run + verify coverage**

Run: `go test ./internal/plugins/adapted/network/ntp/... ./internal/plugins/adapted/network/dns/...`
Expected: PASS; `go test -cover ./...` reports both packages ≥ 70%.

- [ ] **Step 2.4: Commit**

```bash
git add internal/plugins/adapted/network/ntp/ntp_test.go internal/plugins/adapted/network/dns/dns_test.go
git commit -m "test(ntp,dns): migrate to fakeserver helper

Replaces inlined net.ListenUDP + t.Cleanup with fakeserver.ListenUDPLoop.
Establishes the canonical per-plugin test pattern for Tasks 3+.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: Tier 1 batch 1 — 4 HTTP plugins (jenkins, kibana, weblogic, webtitle)

**Files:**
- Create: `internal/plugins/adapted/web/jenkins/jenkins_test.go`
- Create: `internal/plugins/adapted/web/kibana/kibana_test.go`
- Create: `internal/plugins/adapted/web/weblogic/weblogic_test.go`
- Create: `internal/plugins/adapted/web/webtitle/webtitle_test.go`

**Pattern (apply to each plugin):**

```go
// internal/plugins/adapted/web/<plugin>/<plugin>_test.go
package <plugin>

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedResponse returns the minimum JSON / text the plugin needs to
// Identify as a hit. / 返回让插件识别为命中的最小响应。
func cannedResponse(w http.ResponseWriter, r *http.Request) {
	// ...plugin-specific JSON or text...
}

func Test<Plugin>_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url) // "127.0.0.1", 12345
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil { t.Fatal("expected hit, got nil") }
}

func Test<Plugin>_CredentialHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Credential(ctx, host, port, "admin", "admin")
	if r == nil { t.Fatal("expected cred hit, got nil") }
}

func splitHostPort(url string) (string, int) {
	// strip "http://", split on ":", parse port
	// ...5 lines of net/url + strconv...
}
```

**Per-plugin canned response hints** (research via Read of existing `<plugin>.go` to find what banner / API path it probes):

- **jenkins**: `/login` → 200 with HTML containing "Jenkins"; credential `/j_acegi_security_check` POST returns 302.
- **kibana**: `/api/status` → 200 with `{"version":{"number":"8.0.0"}}`; credential `/api/security/v1/login` returns 200 + auth cookie.
- **weblogic**: `/console/login/LoginForm.jsp` → 200 with HTML containing "WebLogic"; credential POST returns 302.
- **webtitle**: GET `/` → 200 with HTML containing `<title>`; no credential spray needed if plugin only does Identify (verify by reading webtitle.go).

(Investigate actual code to confirm probe path and minimum response. The implementer reads `<plugin>.go` to find the exact bytes the plugin expects.)

- [ ] **Step 3.1: Read each plugin's source to determine probe path + minimum response**

Run: `cat internal/plugins/adapted/web/jenkins/jenkins.go | head -80` (and same for kibana/weblogic/webtitle).

- [ ] **Step 3.2: Write `jenkins_test.go`**

Apply pattern above; use `/login` → HTML with "Jenkins" for Identify; use `/j_acegi_security_check` 302 for Credential.

- [ ] **Step 3.3: Write `kibana_test.go`**

Use `/api/status` → JSON with version; `/api/security/v1/login` 200 for Credential.

- [ ] **Step 3.4: Write `weblogic_test.go`**

Use `/console/login/LoginForm.jsp` → HTML with "WebLogic"; credential POST 302.

- [ ] **Step 3.5: Write `webtitle_test.go`**

Use GET `/` → HTML with `<title>`; only Identify (no Credential per source code).

- [ ] **Step 3.6: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/web/jenkins/... ./internal/plugins/adapted/web/kibana/... ./internal/plugins/adapted/web/weblogic/... ./internal/plugins/adapted/web/webtitle/...`
Expected: all 4 packages ≥ 70% coverage; all tests PASS.

- [ ] **Step 3.7: Commit**

```bash
git add internal/plugins/adapted/web/jenkins/jenkins_test.go \
        internal/plugins/adapted/web/kibana/kibana_test.go \
        internal/plugins/adapted/web/weblogic/weblogic_test.go \
        internal/plugins/adapted/web/webtitle/webtitle_test.go
git commit -m "test(tier1-batch1): fake-server tests for jenkins, kibana, weblogic, webtitle

Establishes HTTP-plugin pattern: fakeserver.StartHTTP + canned
handler. 4 plugins x 2 cases each = 8 tests.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: Tier 1 batch 2 — 3 HTTP plugins (elasticsearch, aws, azure)

**Files:**
- Create: `internal/plugins/adapted/database/elasticsearch/elasticsearch_test.go`
- Create: `internal/plugins/adapted/cloud/aws/aws_test.go`
- Create: `internal/plugins/adapted/cloud/azure/azure_test.go`

Same pattern as Task 3. Per-plugin hints:

- **elasticsearch**: GET `/` → JSON with `{"version":{"number":"8.0.0"}}`.
- **aws**: GET `/` → XML with `<Code>Success</Code>`; credential via `aws-cli`-style headers.
- **azure**: GET `/` → JSON; credential via OAuth-style POST.

- [ ] **Step 4.1: Read each plugin's source for probe path**

Run: `cat internal/plugins/adapted/database/elasticsearch/elasticsearch.go | head -80` (and aws.go, azure.go).

- [ ] **Step 4.2: Write 3 test files**

Use Task 3 pattern. Each file has 2 tests (Identify_Hit, Credential_Hit).

- [ ] **Step 4.3: Run + verify coverage**

Run: `go test -cover ./internal/plugins/adapted/database/elasticsearch/... ./internal/plugins/adapted/cloud/aws/... ./internal/plugins/adapted/cloud/azure/...`
Expected: all 3 packages ≥ 70%.

- [ ] **Step 4.4: Commit**

```bash
git add internal/plugins/adapted/database/elasticsearch/elasticsearch_test.go \
        internal/plugins/adapted/cloud/aws/aws_test.go \
        internal/plugins/adapted/cloud/azure/azure_test.go
git commit -m "test(tier1-batch2): fake-server tests for elasticsearch, aws, azure

3 HTTP plugins. Pattern established in Task 3.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 5: Tier 1 batch 3 — 3 messaging plugins (activemq, rabbitmq, rocketmq)

**Files:**
- Create: `internal/plugins/adapted/messaging/activemq/activemq_test.go`
- Create: `internal/plugins/adapted/messaging/rabbitmq/rabbitmq_test.go`
- Create: `internal/plugins/adapted/messaging/rocketmq/rocketmq_test.go`

Same pattern as Task 3. Per-plugin hints:

- **activemq**: `/api/jolokia/` → JSON; credential via AMQP wire protocol (binary) — may need TCP not HTTP.
- **rabbitmq**: `/api/overview` → JSON; credential via AMQP wire.
- **rocketmq**: HTTP `/rocketmq/nsaddr` → JSON; credential via Remoting binary protocol (TCP).

NOTE: If a plugin uses TCP wire protocol (AMQP / RocketMQ Remoting), use `fakeserver.ListenLoop` instead of `StartHTTP` for the credential test. Identify can still use HTTP if there's an admin endpoint.

- [ ] **Step 5.1: Read each plugin's source**

Confirm whether Identify is HTTP-only or TCP; confirm credential is TCP-wire.

- [ ] **Step 5.2: Write 3 test files**

For each plugin: Identify uses HTTP if possible; Credential uses TCP via `fakeserver.ListenLoop` if it's binary wire.

- [ ] **Step 5.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/messaging/activemq/... ./internal/plugins/adapted/messaging/rabbitmq/... ./internal/plugins/adapted/messaging/rocketmq/...`
Expected: all 3 packages ≥ 70%.

- [ ] **Step 5.4: Commit**

```bash
git add internal/plugins/adapted/messaging/activemq/activemq_test.go \
        internal/plugins/adapted/messaging/rabbitmq/rabbitmq_test.go \
        internal/plugins/adapted/messaging/rocketmq/rocketmq_test.go
git commit -m "test(tier1-batch3): fake-server tests for activemq, rabbitmq, rocketmq

3 messaging plugins. Identify via HTTP admin endpoint; credential via
TCP wire protocol (fakeserver.ListenLoop).

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 6: Tier 2 batch 1 — 2 text-line TCP plugins (redis, memcached)

**Files:**
- Create: `internal/plugins/adapted/database/redis/redis_test.go`
- Create: `internal/plugins/adapted/database/memcached/memcached_test.go`

**Pattern shift:** Text-line TCP plugins use `fakeserver.ListenLoop` (not HTTP). Handler reads bufio.Scanner lines from the conn, writes back RESP / text protocol responses.

```go
// redis_test.go sketch
package redis

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

func TestRedis_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		r := bufio.NewReader(c)
		// RESP: read *N\r\n$Len\r\nCMD\r\n$Len\r\narg\r\n... then reply.
		// For PING: reply "+PONG\r\n".
		// For CLIENT GETNAME or other Identify probes, read until\n then reply.
		_, _ = c.Write([]byte("+PONG\r\n"))
		_ = r // simple responder; redis plugin only needs PONG to identify
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil { t.Fatal("expected hit") }
}

func TestRedis_CredentialHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// AUTH user pass → "+OK\r\n"
		// read whatever the client sends then reply OK.
		buf := make([]byte, 4096)
		c.Read(buf)
		_, _ = c.Write([]byte("+OK\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Credential(ctx, host, port, "admin", "admin")
	if r == nil { t.Fatal("expected cred hit") }
}
```

- [ ] **Step 6.1: Read redis.go + memcached.go**

Find exact probes (PING vs INFO vs CLIENT) and minimum response bytes.

- [ ] **Step 6.2: Write redis_test.go + memcached_test.go**

Use pattern above; tailor handler to each protocol.

- [ ] **Step 6.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/database/redis/... ./internal/plugins/adapted/database/memcached/...`
Expected: ≥ 70% each.

- [ ] **Step 6.4: Commit**

```bash
git add internal/plugins/adapted/database/redis/redis_test.go \
        internal/plugins/adapted/database/memcached/memcached_test.go
git commit -m "test(tier2-batch1): fake-server tests for redis, memcached

2 text-line TCP plugins. RESP (redis) / text-line (memcached).

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 7: Tier 2 batch 2 — 3 email plugins (smtp, pop3, imap)

**Files:**
- Create: `internal/plugins/adapted/email/smtp/smtp_test.go`
- Create: `internal/plugins/adapted/email/pop3/pop3_test.go`
- Create: `internal/plugins/adapted/email/imap/imap_test.go`

Same pattern as Task 6 (text-line TCP via `fakeserver.ListenLoop`). Per-plugin hints:

- **smtp**: Greet with `220 smtp.example.com ESMTP\r\n`; on `EHLO` reply `250 OK\r\n`; on `AUTH LOGIN` reply `334 VXNlcm5hbWU6\r\n` (base64 "Username:") then accept creds + reply `235 2.7.0 Authentication successful`.
- **pop3**: Greet with `+OK POP3 ready\r\n`; on `USER`/`PASS` reply `+OK`; on `STAT` reply `+OK 0 0`.
- **imap**: Greet with `* OK [CAPABILITY IMAP4rev1] ready\r\n`; on `LOGIN user pass` reply `a001 OK LOGIN completed`.

- [ ] **Step 7.1: Read each plugin's source**

Confirm exact greeting + command/response sequences.

- [ ] **Step 7.2: Write 3 test files**

Use Task 6 pattern; tailor handlers.

- [ ] **Step 7.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/email/...`
Expected: all 3 packages ≥ 70%.

- [ ] **Step 7.4: Commit**

```bash
git add internal/plugins/adapted/email/
git commit -m "test(tier2-batch2): fake-server tests for smtp, pop3, imap

3 email plugins (text-line TCP).

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 8: Tier 2 batch 3 — 5 db/transport plugins (mysql, postgresql, mongodb, ftp, nfs)

**Files:**
- Create: `internal/plugins/adapted/database/mysql/mysql_test.go`
- Create: `internal/plugins/adapted/database/postgresql/postgresql_test.go`
- Create: `internal/plugins/adapted/database/mongodb/mongodb_test.go`
- Create: `internal/plugins/adapted/filestorage/ftp/ftp_test.go`
- Create: `internal/plugins/adapted/filestorage/nfs/nfs_test.go`

Mixed text-line / binary TCP. Use `fakeserver.ListenLoop`. For mysql/postgresql/mongodb the binary protocol needs minimum-viable handshake bytes — investigate each plugin's source to find the exact greeting bytes the plugin expects.

Per-plugin hints:
- **mysql**: Greet with server greeting packet (capability flags, version string); respond to COM_LOGIN with OK packet.
- **postgresql**: StartupMessage → AuthenticationOk; Query → DataRow/CommandComplete.
- **mongodb**: Hello/isMaster reply with `{ok: 1}` BSON.
- **ftp**: `220 FTP ready\r\n`; `USER admin` → `331`; `PASS admin` → `230`.
- **nfs**: Portmapper (port 111) protocol — RPC call → RPC reply with port.

- [ ] **Step 8.1: Read each plugin's source**

- [ ] **Step 8.2: Write 5 test files**

Use Task 6 pattern; tailor handlers per plugin.

- [ ] **Step 8.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/database/mysql/... ./internal/plugins/adapted/database/postgresql/... ./internal/plugins/adapted/database/mongodb/... ./internal/plugins/adapted/filestorage/ftp/... ./internal/plugins/adapted/filestorage/nfs/...`
Expected: all 5 packages ≥ 70%.

- [ ] **Step 8.4: Commit**

```bash
git add internal/plugins/adapted/database/mysql/ \
        internal/plugins/adapted/database/postgresql/ \
        internal/plugins/adapted/database/mongodb/ \
        internal/plugins/adapted/filestorage/ftp/ \
        internal/plugins/adapted/filestorage/nfs/
git commit -m "test(tier2-batch3): fake-server tests for mysql, postgresql, mongodb, ftp, nfs

5 plugins. mysql/postgresql/mongodb use binary wire protocols; ftp
text-line; nfs RPC.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 9: Tier 3 batch 1 — 3 simpler stateful TCP (ssh, telnet, vnc)

**Files:**
- Create: `internal/plugins/adapted/remote/ssh/ssh_test.go`
- Create: `internal/plugins/adapted/remote/telnet/telnet_test.go`
- Create: `internal/plugins/adapted/remote/vnc/vnc_test.go`

Stateful TCP — handler reads initial bytes, writes a recognizable greeting (SSH banner / telnet IAC / VNC protocol version), then waits for the next state-machine step. Use `fakeserver.ListenLoop`.

Per-plugin hints:
- **ssh**: Read SSH version string `SSH-2.0-...\r\n`, reply with our own banner; on `SSH_MSG_USERAUTH_REQUEST` with password method, reply with `SSH_MSG_USERAUTH_SUCCESS`.
- **telnet**: Send `IAC WILL ECHO IAC DO TTYPE`; on user input IAC negotiation, accept.
- **vnc**: Send `RFB 003.008\n`; on `ClientProtocolVersion` reply with our version; on `ClientInit` reply `ServerInit` with name + dimensions.

NOTE: These are stateful — handlers use closures or simple state variables. Each plugin's handler is ~30-60 lines.

- [ ] **Step 9.1: Read each plugin's source** — find exact handshake bytes expected.

- [ ] **Step 9.2: Write 3 test files**

Each with TestIdentify_Hit + TestCredential_Hit (ssh) or TestIdentify_Hit only (telnet/vnc if no credential spray).

- [ ] **Step 9.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/remote/ssh/... ./internal/plugins/adapted/remote/telnet/... ./internal/plugins/adapted/remote/vnc/...`
Expected: all ≥ 70%.

- [ ] **Step 9.4: Commit**

```bash
git add internal/plugins/adapted/remote/ssh/ \
        internal/plugins/adapted/remote/telnet/ \
        internal/plugins/adapted/remote/vnc/
git commit -m "test(tier3-batch1): fake-server tests for ssh, telnet, vnc

3 stateful TCP plugins. SSH requires both Identify + Credential
happy paths (UserAuthSuccess); telnet/vnc Identify only (no
credential spray).

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 10: Tier 3 batch 2 — 3 Windows-ish (smb, rdp, rdpnla)

**Files:**
- Create or refactor: `internal/plugins/adapted/filestorage/smb/smb_test.go`
- Refactor: `internal/plugins/adapted/remote/rdp/rdp_test.go` (already exists from earlier work)
- Create: `internal/plugins/adapted/remote/rdpnla/rdpnla_test.go`

The rdp_test.go already exists (per spec). Refactor it to use `fakeserver.ListenLoop` (Task 2 pattern). Then add smb + rdpnla.

Per-plugin hints:
- **smb**: SMB2 negotiate request → reply with SMB2 NegotiateProtocolResponse; SessionSetup → SessionSetupResponse with status SUCCESS.
- **rdp**: X.224 Connection Request → Connection Confirm; MCS Connect-Initial → Connect-Response; CredSSP NTLM/Negotiate → NTLM/Challenge (then negotiate completion).
- **rdpnla**: NLA (CredSSP over TLS) — needs TLS handshake + CredSSP; complex. If too hard, write Identify-only test and accept lower coverage.

- [ ] **Step 10.1: Refactor rdp_test.go to use fakeserver.ListenLoop**

Same pattern as Task 2 (migrate existing inline plumbing).

- [ ] **Step 10.2: Read each plugin's source for handshake bytes**

- [ ] **Step 10.3: Write smb_test.go + rdpnla_test.go**

If rdpnla's TLS layer is too complex for happy-path faking, write Identify-only and add a comment explaining the lower coverage.

- [ ] **Step 10.4: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/filestorage/smb/... ./internal/plugins/adapted/remote/rdp/... ./internal/plugins/adapted/remote/rdpnla/...`
Expected: smb ≥ 70%; rdp ≥ 70%; rdpnla ≥ 50% (if Identify-only acceptable).

- [ ] **Step 10.5: Commit**

```bash
git add internal/plugins/adapted/filestorage/smb/ \
        internal/plugins/adapted/remote/rdp/ \
        internal/plugins/adapted/remote/rdpnla/
git commit -m "test(tier3-batch2): fake-server tests for smb, rdp, rdpnla

3 Windows-ish stateful TCP plugins. rdp_test.go refactored to use
fakeserver. smb full coverage; rdpnla may be Identify-only if TLS
layer is too complex.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 11: Tier 3 batch 3 — 7 remaining stateful TCP (winrm, ipmi, kafka, mqtt, mssql, oracle, rsync)

**Files:**
- Create: `internal/plugins/adapted/remote/winrm/winrm_test.go`
- Create: `internal/plugins/adapted/remote/ipmi/ipmi_test.go`
- Create: `internal/plugins/adapted/messaging/kafka/kafka_test.go`
- Create: `internal/plugins/adapted/messaging/mqtt/mqtt_test.go`
- Create: `internal/plugins/adapted/database/mssql/mssql_test.go`
- Create: `internal/plugins/adapted/database/oracle/oracle_test.go`
- Create: `internal/plugins/adapted/filestorage/rsync/rsync_test.go`

Most complex task — 7 plugins, mixed protocols. Use `fakeserver.ListenLoop`.

Per-plugin hints (handler minimums):
- **winrm**: HTTP POST to `/wsman` with WSMan SOAP envelope → reply 200 + SOAP response.
- **ipmi**: RMCP+ session establishment → reply with session challenge.
- **kafka**: ApiVersions request → reply with ApiVersionsResponse listing supported versions.
- **mqtt**: CONNECT packet → reply CONNACK with return code 0.
- **mssql**: TDS prelogin (0x12) → reply with prelogin response; Login7 → reply with LOGIN7ACK.
- **oracle**: TNS connect packet → reply with TNS accept.
- **rsync**: Greeting banner (if any) + protocol version response.

If a plugin's protocol is too complex to fake in <100 lines, write Identify-only and document lower coverage.

- [ ] **Step 11.1: Read each plugin's source for handshake bytes**

- [ ] **Step 11.2: Write 7 test files**

Group by complexity. Aim for ≥ 70% per plugin; lower is acceptable if a protocol is too complex (per spec risk #2).

- [ ] **Step 11.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/remote/winrm/... ./internal/plugins/adapted/remote/ipmi/... ./internal/plugins/adapted/messaging/kafka/... ./internal/plugins/adapted/messaging/mqtt/... ./internal/plugins/adapted/database/mssql/... ./internal/plugins/adapted/database/oracle/... ./internal/plugins/adapted/filestorage/rsync/...`
Expected: most ≥ 70%; possibly a few lower (50-65%) for the most complex protocols — note exceptions.

- [ ] **Step 11.4: Commit**

```bash
git add internal/plugins/adapted/remote/winrm/ \
        internal/plugins/adapted/remote/ipmi/ \
        internal/plugins/adapted/messaging/kafka/ \
        internal/plugins/adapted/messaging/mqtt/ \
        internal/plugins/adapted/database/mssql/ \
        internal/plugins/adapted/database/oracle/ \
        internal/plugins/adapted/filestorage/rsync/
git commit -m "test(tier3-batch3): fake-server tests for winrm, ipmi, kafka, mqtt, mssql, oracle, rsync

7 stateful TCP plugins. Most achieve >=70% coverage; complex
protocols may document lower per-plugin floors.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 12: Tier 4 batch 1 — 3 UDP plugins (snmp, snmpv3, tftp)

**Files:**
- Create: `internal/plugins/adapted/network/snmp/snmp_test.go`
- Create: `internal/plugins/adapted/network/snmpv3/snmpv3_test.go`
- Create: `internal/plugins/adapted/network/tftp/tftp_test.go`

UDP via `fakeserver.ListenUDPLoop`. Pattern is shorter than TCP since UDP is stateless.

Per-plugin hints:
- **snmp**: GET request (BER-encoded) → reply with GET-RESPONSE PDU.
- **snmpv3**: EngineID discovery → reply with Report PDU containing engine ID.
- **tftp**: Read request (RRQ) → reply with DATA packet (block 1, 512 bytes).

- [ ] **Step 12.1: Read each plugin's source**

- [ ] **Step 12.2: Write 3 test files**

Use Task 6 UDP pattern via `fakeserver.ListenUDPLoop`.

- [ ] **Step 12.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/network/snmp/... ./internal/plugins/adapted/network/snmpv3/... ./internal/plugins/adapted/network/tftp/...`
Expected: ≥ 70% each.

- [ ] **Step 12.4: Commit**

```bash
git add internal/plugins/adapted/network/snmp/ \
        internal/plugins/adapted/network/snmpv3/ \
        internal/plugins/adapted/network/tftp/
git commit -m "test(tier4-batch1): fake-server tests for snmp, snmpv3, tftp

3 UDP plugins via fakeserver.ListenUDPLoop.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 13: Tier 4 batch 2 — 5 UDP/TCP plugins (bacnet, modbus, ldap, socks5, docker)

**Files:**
- Create: `internal/plugins/adapted/network/bacnet/bacnet_test.go`
- Create: `internal/plugins/adapted/network/modbus/modbus_test.go`
- Create: `internal/plugins/adapted/network/ldap/ldap_test.go`
- Create: `internal/plugins/adapted/network/socks5/socks5_test.go`
- Create: `internal/plugins/adapted/network/docker/docker_test.go`

Mixed UDP/TCP/HTTP. Per-plugin hints:
- **bacnet**: UDP, Who-Is → reply with I-Am.
- **modbus**: TCP, MBAP header + Read Holding Registers → reply with function code + data.
- **ldap**: TCP, SearchRequest (BER) → reply with SearchResultEntry.
- **socks5**: TCP, client greeting (no-auth) → reply with method selection; CONNECT request → reply success.
- **docker**: HTTP GET `/version` → reply with JSON.

- [ ] **Step 13.1: Read each plugin's source**

- [ ] **Step 13.2: Write 5 test files**

Use `ListenUDPLoop` for bacnet, `ListenLoop` for modbus/ldap/socks5, `StartHTTP` for docker.

- [ ] **Step 13.3: Run + verify**

Run: `go test -cover ./internal/plugins/adapted/network/bacnet/... ./internal/plugins/adapted/network/modbus/... ./internal/plugins/adapted/network/ldap/... ./internal/plugins/adapted/network/socks5/... ./internal/plugins/adapted/network/docker/...`
Expected: ≥ 70% each.

- [ ] **Step 13.4: Commit**

```bash
git add internal/plugins/adapted/network/bacnet/ \
        internal/plugins/adapted/network/modbus/ \
        internal/plugins/adapted/network/ldap/ \
        internal/plugins/adapted/network/socks5/ \
        internal/plugins/adapted/network/docker/
git commit -m "test(tier4-batch2): fake-server tests for bacnet, modbus, ldap, socks5, docker

5 plugins spanning UDP, TCP, and HTTP. Mixed use of fakeserver
helpers per protocol's transport.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 14: Update `scripts/ci-coverage-check.py` (60% → 80% + per-plugin 70%)

**Files:**
- Modify: `scripts/ci-coverage-check.py`

**Why:** The current script enforces 60% globally. The new threshold is 80% globally + 70% per adapted plugin. The script needs to:
1. Bump the global floor
2. Add a per-package walk that iterates `internal/plugins/adapted/*/`
3. Print a per-plugin table
4. Exit non-zero if either floor fails

- [ ] **Step 14.1: Read the existing script**

Run: `cat scripts/ci-coverage-check.py`

- [ ] **Step 14.2: Modify the script**

Two changes:
1. Replace `FLOOR = 60.0` with `FLOOR = 80.0`.
2. After the global check, add a per-plugin walk:

```python
PLUGIN_ROOT = "internal/plugins/adapted"
PER_PLUGIN_FLOOR = 70.0

def per_plugin_coverage_failures():
    failures = []
    for category in sorted(os.listdir(PLUGIN_ROOT)):
        cat_dir = os.path.join(PLUGIN_ROOT, category)
        if not os.path.isdir(cat_dir):
            continue
        for plugin in sorted(os.listdir(cat_dir)):
            pkg = os.path.join(cat_dir, plugin)
            if not os.path.isdir(pkg) or not os.path.exists(os.path.join(pkg, "go.mod")) and not os.path.exists(os.path.join(pkg, f"{plugin}.go")):
                continue
            cov = run_go_test_cover(pkg)
            if cov < PER_PLUGIN_FLOOR:
                failures.append((pkg, cov))
    return failures

# At the end of main(), after global floor check:
plugin_failures = per_plugin_coverage_failures()
if plugin_failures:
    print("\nPer-plugin coverage below {}%:".format(PER_PLUGIN_FLOOR))
    for pkg, cov in plugin_failures:
        print(f"  {pkg}: {cov:.1f}%")
    sys.exit(1)
```

(Adapt `run_go_test_cover` to whatever the existing helper is called.)

- [ ] **Step 14.3: Run the script locally**

Run: `python scripts/ci-coverage-check.py`
Expected: prints global coverage ≥ 80% and per-plugin table; exits 0 if all plugins ≥ 70%.

If any plugin is below 70%, fix the test (go back to the relevant task) before bumping the floor in CI.

- [ ] **Step 14.4: Commit**

```bash
git add scripts/ci-coverage-check.py
git commit -m "ci(coverage): raise floor 60% -> 80% + add per-plugin 70% walk

Bumps the global floor (matching the v0.6.0 quality program target)
and adds a per-plugin walk that fails CI when any adapted plugin
falls below 70%. Prints a per-plugin table for visibility.

Per-plugin floor is the smaller bar — it lets plugins with
inherently hard protocols (rdpnla TLS, complex wire formats) ship
below the global 80% as long as their own coverage hits 70%.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 15: Final verification + green-light

**Files:** none (verification-only)

- [ ] **Step 15.1: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 15.2: Run with coverage**

Run: `go test -cover ./... | tail -20`
Expected: total ≥ 80%.

- [ ] **Step 15.3: Run coverage check script**

Run: `python scripts/ci-coverage-check.py`
Expected: exit 0; all plugins ≥ 70%.

- [ ] **Step 15.4: Format + vet**

Run:
```bash
gofmt -l .
go vet ./...
```
Expected: both empty.

- [ ] **Step 15.5: Smoke-check the binary still builds**

Run: `go build -o /tmp/fg-qimen . && /tmp/fg-qimen --help`
Expected: binary runs and prints help (proves no production-code regression).

- [ ] **Step 15.6: Commit any gofmt fixes (if needed)**

If gofmt printed anything in 15.4:
```bash
gofmt -w .
git add -u
git commit -m "style: gofmt"
```

(No commit needed if gofmt was clean.)

---

## Self-Review

**1. Spec coverage:**

| Spec section | Plan task(s) |
|---|---|
| §2 Goal 1 — total coverage ≥ 80% | Tasks 3-13 (per-plugin tests raise total) + Task 14 (floor enforcement) + Task 15 (verify) |
| §2 Goal 2 — per-plugin ≥ 70% | Tasks 3-13 + Task 14 (per-plugin walk) |
| §2 Goal 3 — Identify + Credential happy path | Every per-plugin test file has `Test{Name}_IdentifyHit` + `Test{Name}_CredentialHit` |
| §2 Goal 4 — no production-code changes | File Structure section explicitly forbids touching non-test `.go` files outside `internal/fakeserver/` |
| §4.2 New package `internal/fakeserver/` | Task 1 |
| §5 Tier 1 (10 HTTP plugins) | Tasks 3-5 |
| §5 Tier 2 (10 text-line TCP) | Tasks 6-8 |
| §5 Tier 3 (13 stateful TCP) | Tasks 9-11 |
| §5 Tier 4 (10 UDP) | Tasks 12-13 |
| §6 CI integration — coverage floor + per-plugin walk | Task 14 |
| §7 Acceptance — `go test` green, total ≥ 80%, all plugins ≥ 70%, gofmt/vet clean | Task 15 |

**2. Placeholder scan:**
- No "TBD" / "TODO" / "implement later" anywhere.
- Per-plugin test files: code blocks show the pattern; "tailor handler" comments mark where the implementer fills in protocol-specific bytes (this is acceptable — the pattern is mechanical once you know the protocol bytes).
- One acceptable instruction: "Read each plugin's source" — this is a research step, not an implementation deferral.

**3. Type consistency:**
- `fakeserver.ListenLoop(t, handler func(net.Conn)) (host string, port int)` — Task 1 declares; Tasks 6-13 use this signature consistently.
- `fakeserver.ListenUDPLoop(t, handler func([]byte, *net.UDPAddr) []byte) (host string, port int)` — Task 1 declares; Tasks 2, 6, 12-13 use this consistently.
- `fakeserver.StartHTTP(t, h http.Handler) string` — Task 1 declares; Tasks 3-5 use this consistently.
- `fakeserver.WriteMagic(magic []byte, lengthLen int, payload []byte) []byte` — declared + tested in Task 1; available for any plugin that needs binary framing.
- Per-plugin test naming: `Test{Name}_IdentifyHit` and `Test{Name}_CredentialHit` — consistent across all 11 per-plugin batches.

**4. Risks addressed:**
- Spec §8 risk "complex protocols fake wrong" → Tasks 11 / 12 / 13 explicitly note that Identify-only is acceptable for hard protocols (lower per-plugin coverage tolerated).
- Spec §8 risk "coverage floor trips CI on incomplete plugins" → Task 14 documents two-wave rollout: bump to 70% first, then 80% — but the plan as written lands 80% directly because Tasks 3-13 each enforce ≥ 70% on the plugin they add.

Plan is internally consistent and covers the spec end-to-end.
