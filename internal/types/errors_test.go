// errors_test.go — round-trip tests for the CodedError system.
//
// errors_test.go — CodedError 系统的往返测试。
package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodedError_ErrorString(t *testing.T) {
	e := CodeInvalidTarget.New("bad CIDR: 999.0.0.0/33", "use a.b.c.d/n with n in 0..32")
	got := e.Error()
	if !strings.HasPrefix(got, "[E001]") {
		t.Errorf("Error() = %q, want prefix [E001]", got)
	}
	if !strings.Contains(got, "bad CIDR: 999.0.0.0/33") {
		t.Errorf("Error() missing message: %q", got)
	}
	if !strings.Contains(got, "Hint:") {
		t.Errorf("Error() missing hint marker: %q", got)
	}
}

func TestCodedError_NoHint(t *testing.T) {
	e := CodeInternal.New("bug", "")
	if strings.Contains(e.Error(), "Hint:") {
		t.Errorf("no-hint error leaked Hint marker: %q", e.Error())
	}
}

func TestCodedError_Newf(t *testing.T) {
	e := CodeBboltDecryptFailed.Newf("set FG_QIMEN_PROJECT_KEY or remove --project-key flag",
		"decrypt failed for %d records", 7)
	if !strings.Contains(e.Error(), "7 records") {
		t.Errorf("Newf format failed: %q", e.Error())
	}
	if !strings.Contains(e.Error(), "FG_QIMEN_PROJECT_KEY") {
		t.Errorf("Newf hint missing: %q", e.Error())
	}
}

func TestCodedError_MarshalJSON(t *testing.T) {
	e := CodeBboltDecryptFailed.Newf("check your key", "wrong key for %s", "myproject")
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	// Verify the field shape (order-independent via map decode).
	// 验证字段形态（用 map decode 顺序无关）。
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["code"] != "E103" {
		t.Errorf("code field = %v, want E103", got["code"])
	}
	if !strings.Contains(got["message"].(string), "myproject") {
		t.Errorf("message missing myproject: %v", got["message"])
	}
	if !strings.Contains(got["hint"].(string), "check your key") {
		t.Errorf("hint wrong: %v", got["hint"])
	}
}

func TestCodedError_CodeUniqueness(t *testing.T) {
	// Sanity: the test author changed a code by accident and the
	// suite failed once because the user-facing stable code moved.
	// This test guards against renumbering in future edits.
	//
	// 健全性检查：作者曾因误改 code 编号导致用户面稳定码被改，
	// 本测试防止未来编辑时重编号。
	want := map[Code]string{
		CodeInvalidTarget:      "E001",
		CodeInvalidPort:        "E002",
		CodeInvalidCred:        "E003",
		CodeMissingTarget:      "E004",
		CodeUnknownMode:        "E005",
		CodeInvalidTimeout:     "E006",
		CodeConflictingFlag:    "E007",
		CodeProjectNameInvalid: "E101",
		CodeBboltOpenFailed:    "E102",
		CodeBboltDecryptFailed: "E103",
		CodeBboltCorrupted:     "E104",
		CodeProjectPathEscape:  "E105",
		CodeOutputPathEscape:   "E106",
		CodeProxyInvalid:       "E201",
		CodeProxyDialFailed:    "E202",
		CodeIfaceInvalid:       "E203",
		CodeResolveFailed:      "E204",
		CodeAllPluginsFailed:   "E301",
		CodePluginDisabled:     "E302",
		CodeNoOpenPorts:        "E303",
		CodeCredentialNone:     "E304",
		CodeTimeoutGlobal:      "E305",
		CodeInternal:           "E999",
	}
	for code, wantStr := range want {
		if string(code) != wantStr {
			t.Errorf("code %q changed; want %q (codes are stable)", string(code), wantStr)
		}
	}
}
