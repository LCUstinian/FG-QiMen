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
