// ntp_test.go — unit tests for the NTP Identify plugin. / NTP 识
// 别插件的单元测试。
package ntp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestNTP_Hit verifies a stratum-2 server is identified. /
// 验证 stratum-2 server 被识别。
func TestNTP_Hit(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		resp := make([]byte, 48)
		// LI=0, VN=4, Mode=4 (server) = 0b00100100 = 0x24.
		// / LI=0, VN=4, Mode=4 (server) = 0b00100100 = 0x24。
		resp[0] = 0x24
		// Stratum 2 — primary reference server reachable from the
		// client. / Stratum 2 — 客户端可达的二级参考源。
		resp[1] = 2
		return resp
	})
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on NTP server")
	}
	if hit.Service != "ntp" {
		t.Errorf("Service = %q, want ntp", hit.Service)
	}
	if hit.Banner != "NTP (stratum 2)" {
		t.Errorf("Banner = %q, want \"NTP (stratum 2)\"", hit.Banner)
	}
}

// TestNTP_NotNTP verifies a non-NTP server (e.g. echo port)
// returns nil. / 验证非 NTP 服务（echo）返回 nil。
func TestNTP_NotNTP(t *testing.T) {
	// Listen on a TCP port that does nothing. UDP dial succeeds
	// but the response will not be NTP-shaped. / 监听一个无操
	// 作的 TCP 端口。UDP 拨号成功但响应不是 NTP 格式。
	_, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Reply with garbage. / 回垃圾数据。
		_, _ = c.Write([]byte("not ntp"))
	})
	// NTP's UDP dial to a TCP-only port will fail. We expect nil
	// (dial failure returns nil from RawUDPIdentify). / NTP 的
	// UDP 拨号到仅 TCP 的端口会失败。我们期望 nil（拨号失败
	// 让 RawUDPIdentify 返回 nil）。
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	hit := auth.Identify(ctx, "127.0.0.1", port)
	if hit != nil {
		t.Errorf("expected nil for TCP-only port, got %+v", hit)
	}
}
