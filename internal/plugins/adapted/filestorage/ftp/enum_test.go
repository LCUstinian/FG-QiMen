// enum_test.go — read-only FTP enumeration tests (需求B.2) against a
// minimal in-process FTP fake: greeting → USER/PASS → EPSV data conn →
// LIST with unix-style listing → 226. Locks in the walk semantics
// (depth, budget truncation, denied login = no record) and the pure
// helpers.
//
// enum_test.go — 只读 FTP 遍历测试（需求B.2），对最小进程内 FTP 假服
// 务器：greeting → USER/PASS → EPSV 数据连接 → LIST（unix 风格列表）
// → 226。锁定遍历语义（深度、预算截断、拒绝登录不记录）与纯函数。
package ftp

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// ftpListing is the unix-style LIST payload served for /root.
// / ftpListing 是根目录的 unix 风格 LIST 载荷。
const ftpListing = "drwxr-xr-x 1 ftp ftp 0 Sep 13 2025 sub\r\n" +
	"-rw-r--r-- 1 ftp ftp 42 Sep 13 2025 readme.txt\r\n"

const ftpSubListing = "-rw-r--r-- 1 ftp ftp 7 Sep 13 2025 deep.txt\r\n"

// serveFTP is the minimal FTP control-channel fake. passOK=false makes
// login fail with 530 (the denied path). / serveFTP 是最小 FTP 控制通
// 道假服务。passOK=false 时登录以 530 失败（拒绝路径）。
func serveFTP(c net.Conn, passOK bool) {
	defer c.Close()
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	defer dataLn.Close()

	reply := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	reply("220 fake ftp ready")

	sc := bufio.NewScanner(c)
	for sc.Scan() {
		verb := strings.ToUpper(strings.Fields(sc.Text() + " ")[0])
		switch verb {
		case "USER":
			reply("331 password required")
		case "PASS":
			if passOK {
				reply("230 logged in")
			} else {
				reply("530 login incorrect")
				return
			}
		case "TYPE", "SYST", "FEAT", "PWD", "CWD":
			reply("200 ok")
		case "EPSV":
			p := dataLn.Addr().(*net.TCPAddr).Port
			reply(fmt.Sprintf("229 Entering Passive Mode (|||%d|)", p))
		case "PASV":
			p := dataLn.Addr().(*net.TCPAddr).Port
			reply(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", p/256, p%256))
		case "LIST", "NLST":
			go func() {
				d, err := dataLn.Accept()
				if err != nil {
					return
				}
				listing := ftpListing
				if strings.Contains(sc.Text(), "/sub") {
					listing = ftpSubListing
				}
				_, _ = d.Write([]byte(listing))
				_ = d.Close()
			}()
			reply("150 opening data connection")
			reply("226 transfer complete")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("200 ok")
		}
	}
}

func TestFtp_Enumerate_Walk(t *testing.T) {
	host, port := newFake(t, true)
	limits := types.EnumLimits{MaxDepth: 2, MaxEntries: 100, HostTimeout: 30 * time.Second}

	t.Run("anonymous walk hits both levels", func(t *testing.T) {
		r := (&Plugin{}).Enumerate(context.Background(), host, port, "", "", limits)
		if r == nil {
			t.Fatal("anonymous walk must produce a record")
		}
		fp, ok := r.Extra.(*types.FTPEnumResult)
		if !ok {
			t.Fatalf("Extra type = %T, want *types.FTPEnumResult", r.Extra)
		}
		if !fp.Anonymous || fp.User != "anonymous" {
			t.Errorf("anonymous probe mislabelled: anonymous=%v user=%q", fp.Anonymous, fp.User)
		}
		if len(fp.Dirs) != 2 {
			t.Fatalf("want 2 dirs (depth 2), got %d: %+v", len(fp.Dirs), fp.Dirs)
		}
		if fp.Dirs[0].Path != "/" || fp.Dirs[1].Path != "/sub" {
			t.Errorf("dir paths = %q, %q; want / and /sub", fp.Dirs[0].Path, fp.Dirs[1].Path)
		}
		if fp.Truncated {
			t.Error("budget was never drained; truncated must be false")
		}
		if r.Service != "ftp" || r.Host != host || r.Port != port {
			t.Errorf("result envelope wrong: %+v", r)
		}
		if !strings.Contains(r.Banner, "dirs=2") {
			t.Errorf("banner = %q, want dirs=2", r.Banner)
		}
	})

	t.Run("weak-credential walk labels the user", func(t *testing.T) {
		r := (&Plugin{}).Enumerate(context.Background(), host, port, "svc", "weak", limits)
		if r == nil {
			t.Fatal("credentialed walk must produce a record")
		}
		fp := r.Extra.(*types.FTPEnumResult)
		if fp.Anonymous || fp.User != "svc" {
			t.Errorf("credentialed walk mislabelled: anonymous=%v user=%q", fp.Anonymous, fp.User)
		}
	})
}

func TestFtp_Enumerate_BudgetTruncates(t *testing.T) {
	host, port := newFake(t, true)
	// MaxEntries=1: the root dir drains the budget on its first entry;
	// the /sub recursion must be cut off and flagged.
	// / MaxEntries=1：根目录第一个条目就耗尽预算；/sub 递归必须被
	// 截断并标记。
	r := (&Plugin{}).Enumerate(context.Background(), host, port, "", "",
		types.EnumLimits{MaxDepth: 2, MaxEntries: 1, HostTimeout: 30 * time.Second})
	if r == nil {
		t.Fatal("walk must still record despite truncation")
	}
	fp := r.Extra.(*types.FTPEnumResult)
	if !fp.Truncated {
		t.Errorf("budget=1 must flag truncated, got %+v", fp)
	}
}

func TestFtp_Enumerate_NegativePaths(t *testing.T) {
	t.Run("login denied records nothing", func(t *testing.T) {
		host, port := newFake(t, false)
		r := (&Plugin{}).Enumerate(context.Background(), host, port, "", "",
			types.EnumLimits{MaxDepth: 2, MaxEntries: 10, HostTimeout: 30 * time.Second})
		if r != nil {
			t.Fatalf("denied login must return nil, got %+v", r)
		}
	})

	t.Run("dial failure records nothing", func(t *testing.T) {
		// Bind then close: the port is free but connection-refused.
		// / 先绑后关：端口空闲但连接被拒。
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		r := (&Plugin{}).Enumerate(context.Background(), "127.0.0.1", port, "", "",
			types.EnumLimits{MaxDepth: 2, MaxEntries: 10, HostTimeout: 5 * time.Second})
		if r != nil {
			t.Fatalf("dial failure must return nil, got %+v", r)
		}
	})

	t.Run("cancelled context records nothing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := (&Plugin{}).Enumerate(ctx, "127.0.0.1", 21, "", "",
			types.EnumLimits{MaxDepth: 2, MaxEntries: 10, HostTimeout: 5 * time.Second})
		if r != nil {
			t.Fatalf("cancelled ctx must return nil, got %+v", r)
		}
	})
}

func TestFtp_ChildPath(t *testing.T) {
	tests := []struct{ parent, name, want string }{
		{"/", "a", "/a"},
		{"", "a", "/a"},
		{"/b", "a", "/b/a"},
		{"/b/", "a", "/b/a"},
	}
	for _, tt := range tests {
		if got := childPath(tt.parent, tt.name); got != tt.want {
			t.Errorf("childPath(%q, %q) = %q, want %q", tt.parent, tt.name, got, tt.want)
		}
	}
}

func TestFtp_IsAnonymousUser(t *testing.T) {
	for _, u := range []string{"anonymous", "ftp"} {
		if !isAnonymousUser(u) {
			t.Errorf("isAnonymousUser(%q) = false, want true", u)
		}
	}
	for _, u := range []string{"svc", "admin", ""} {
		if isAnonymousUser(u) {
			t.Errorf("isAnonymousUser(%q) = true, want false", u)
		}
	}
}

// newFake starts the FTP fake on an ephemeral port.
// / newFake 在临时端口上启动 FTP 假服务。
func newFake(t *testing.T, passOK bool) (string, int) {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFTP(c, passOK)
		}
	}()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
}
