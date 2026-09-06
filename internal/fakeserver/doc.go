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
