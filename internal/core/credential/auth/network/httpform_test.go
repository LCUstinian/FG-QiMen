// httpform_test.go — HTTP form authenticator tests.
// httpform_test.go — HTTP form 认证器测试。
package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// testCtx is a fresh context per test — avoids nil deref when the
// authenticator checks ctx.Err() / NewRequestWithContext. /
// testCtx 是每次测试的新 context——避免认证器检查 ctx.Err() /
// NewRequestWithContext 时 nil 解引用。
func testCtx() context.Context { return context.Background() }

// TestSplitFields: splitFields parses "k=v,k=v" tuples.
// / splitFields 解析 "k=v,k=v" 二元组。
func TestSplitFields(t *testing.T) {
	got := splitFields("user=$user$,pass=$pass$,csrf=$(none)")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0][0] != "user" || got[0][1] != "$user$" {
		t.Errorf("got[0] = %v, want [user $user$]", got[0])
	}
	if got[1][0] != "pass" || got[1][1] != "$pass$" {
		t.Errorf("got[1] = %v, want [pass $pass$]", got[1])
	}
}

// TestSplitFieldsEmpty: empty spec returns nil. / 空 spec 返回 nil。
func TestSplitFieldsEmpty(t *testing.T) {
	if got := splitFields(""); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestSplitFieldsNoEquals: tokens without '=' are skipped.
// / 没有 '=' 的 token 被跳过。
func TestSplitFieldsNoEquals(t *testing.T) {
	got := splitFields("a=1,bogus,c=3")
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (bogus skipped)", len(got))
	}
}

// TestHTTPFormNoOpWhenURLEmpty: empty URL returns nil immediately.
// / URL 为空时立即返回 nil。
func TestHTTPFormNoOpWhenURLEmpty(t *testing.T) {
	prev := HTTPFormURL
	defer func() { HTTPFormURL = prev }()
	HTTPFormURL = ""

	a := NewHTTPFormAuthenticator()
	hit, err := a.Authenticate(testCtx(), "x", 80, []credential.Cred{
		{User: "u", Pass: "p"},
	}, 1000000000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hit != nil {
		t.Errorf("got hit %v, want nil (URL empty = no-op)", hit)
	}
}

// TestHTTPFormSuccessBySubstring: success substring match → hit.
// / 成功子串匹配 → hit。
func TestHTTPFormSuccessBySubstring(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("user") == "admin" && r.PostForm.Get("pass") == "hunter2" {
			w.Write([]byte("Welcome to dashboard"))
			return
		}
		w.Write([]byte("invalid credentials"))
	}))
	defer srv.Close()

	prevURL, prevFields, prevSuccess, prevFail := HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure
	defer func() {
		HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure = prevURL, prevFields, prevSuccess, prevFail
	}()
	HTTPFormURL = srv.URL + "/login"
	HTTPFormFields = "user=$user$,pass=$pass$"
	HTTPFormSuccess = "Welcome to dashboard"
	HTTPFormFailure = "invalid"

	a := NewHTTPFormAuthenticator()
	hit, err := a.Authenticate(testCtx(), "x", 80, []credential.Cred{
		{User: "admin", Pass: "hunter2"},
		{User: "root", Pass: "toor"},
	}, 1000000000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit on first cred")
	}
	if hit.Cred.User != "admin" || hit.Cred.Pass != "hunter2" {
		t.Errorf("hit.Cred = %v, want admin/hunter2", hit.Cred)
	}
}

// TestHTTPFormMissByFailureMarker: failure substring present → miss.
// / 失败子串存在 → miss。
func TestHTTPFormMissByFailureMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid credentials, please try again"))
	}))
	defer srv.Close()

	prevURL, prevFields, prevSuccess, prevFail := HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure
	defer func() {
		HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure = prevURL, prevFields, prevSuccess, prevFail
	}()
	HTTPFormURL = srv.URL + "/login"
	HTTPFormFields = "user=$user$,pass=$pass$"
	HTTPFormSuccess = "Welcome"
	HTTPFormFailure = "invalid"

	a := NewHTTPFormAuthenticator()
	hit, err := a.Authenticate(testCtx(), "x", 80, []credential.Cred{
		{User: "u", Pass: "p"},
	}, 1000000000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hit != nil {
		t.Errorf("got hit %v, want nil (failure substring present)", hit)
	}
}

// TestHTTPFormSuccessByRedirect: 302 with matching Location → hit.
// / 302 带匹配 Location → hit。
func TestHTTPFormSuccessByRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	}))
	defer srv.Close()

	prevURL, prevFields, prevSuccess, prevRedirect := HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormRedirect
	defer func() {
		HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormRedirect = prevURL, prevFields, prevSuccess, prevRedirect
	}()
	HTTPFormURL = srv.URL + "/login"
	HTTPFormFields = "user=$user$,pass=$pass$"
	HTTPFormSuccess = ""
	HTTPFormRedirect = "/dashboard"

	a := NewHTTPFormAuthenticator()
	hit, err := a.Authenticate(testCtx(), "x", 80, []credential.Cred{
		{User: "u", Pass: "p"},
	}, 1000000000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit via 302 redirect")
	}
}

// TestHTTPFormAmbiguousStatus200Miss: 200 with no markers → miss
// (avoid false positives). / 200 无任何标识 → miss（避免误报）。
func TestHTTPFormAmbiguousStatus200Miss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	prevURL, prevFields, prevSuccess, prevFail := HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure
	defer func() {
		HTTPFormURL, HTTPFormFields, HTTPFormSuccess, HTTPFormFailure = prevURL, prevFields, prevSuccess, prevFail
	}()
	HTTPFormURL = srv.URL + "/login"
	HTTPFormFields = "user=$user$,pass=$pass$"
	HTTPFormSuccess = "Welcome"
	HTTPFormFailure = "invalid"

	a := NewHTTPFormAuthenticator()
	hit, _ := a.Authenticate(testCtx(), "x", 80, []credential.Cred{{User: "u", Pass: "p"}}, 1000000000)
	if hit != nil {
		t.Errorf("ambiguous 200 should be miss, got %v", hit)
	}
}

// TestHTTPFormRejectsNonPasswordCred: only password creds are
// attempted; other methods are skipped. / 仅尝试 password 凭据；
// 其他 method 跳过。
func TestHTTPFormRejectsNonPasswordCred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for non-password cred")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	prevURL, prevFields := HTTPFormURL, HTTPFormFields
	defer func() { HTTPFormURL, HTTPFormFields = prevURL, prevFields }()
	HTTPFormURL = srv.URL + "/login"
	HTTPFormFields = "user=$user$,pass=$pass$"

	a := NewHTTPFormAuthenticator()
	hit, _ := a.Authenticate(testCtx(), "x", 80, []credential.Cred{
		{User: "u", Pass: "k", Method: credential.AuthKey},
	}, 1000000000)
	if hit != nil {
		t.Errorf("got hit %v, want nil (AuthKey cred should be skipped)", hit)
	}
}

// assert compile-time interface conformance. / 编译期接口符合性。
var _ credential.Authenticator = (*HTTPFormAuthenticator)(nil)
var _ = strings.Contains