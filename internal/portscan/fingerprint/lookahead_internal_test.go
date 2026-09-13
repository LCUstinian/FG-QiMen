// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Tests for the lookahead→RE2 header-loop rewrite (lookahead.go).
// Shape tests verify the mechanical replacement; equivalence tests
// verify the rewritten pattern behaves like the PCRE original on the
// banner shapes that matter (skip-to-header, stop-at-blank-line).
//
// lookahead→RE2 头块跳行改写的测试（lookahead.go）。形状测试验证
// 机械替换；等价性测试验证改写后的 pattern 在关键 banner 形态上
// （跳到目标头、空行处停）与 PCRE 原始行为一致。
package fingerprint

import (
	"regexp"
	"testing"
)

func TestRewriteLookaheadHeaderLoop_Shape(t *testing.T) {
	in := `^HTTP/1\.0 \d\d\d (?:[^\r\n]*\r\n(?!\r\n))*?Server: Cherokee/([-.\w]+)\r\n`
	want := `^HTTP/1\.0 \d\d\d (?:[^\r\n]+\r\n)*?Server: Cherokee/([-.\w]+)\r\n`
	if got := rewriteLookaheadHeaderLoop(in); got != want {
		t.Fatalf("rewrite mismatch:\n got: %s\nwant: %s", got, want)
	}

	plain := `^SSH-2\.0-OpenSSH_([\w.]+)`
	if got := rewriteLookaheadHeaderLoop(plain); got != plain {
		t.Fatalf("pattern without the construct must pass through unchanged, got: %s", got)
	}
}

// TestRewriteLookaheadHeaderLoop_Equivalence pins the semantics of the
// rewritten loop against the PCRE original's behaviour on real banner
// shapes. / TestRewriteLookaheadHeaderLoop_Equivalence 在真实 banner
// 形态上把改写后 loop 的语义钉死到 PCRE 原始行为。
func TestRewriteLookaheadHeaderLoop_Equivalence(t *testing.T) {
	// Cherokee rule 6800, verbatim from the embedded probe file.
	// / Cherokee 规则 6800，逐字来自嵌入探针文件。
	pattern := `^HTTP/1\.0 \d\d\d (?:[^\r\n]*\r\n(?!\r\n))*?Server: Cherokee/([-.\w]+)\r\n`
	re := regexp.MustCompile(patternToGoRegex(rewriteLookaheadHeaderLoop(pattern)))

	// 1. Server as a later header line: loop must skip the earlier
	//    lines and land the capture. / Server 在后面的头行：循环跳过
	//    前面的行并完成捕获。
	normal := bytesToLatin1String([]byte("HTTP/1.0 200 OK\r\nDate: Mon, 1 Jan 2026\r\nServer: Cherokee/0.99.30\r\n\r\n"))
	got := re.FindStringSubmatch(normal)
	if got == nil || got[1] != "0.99.30" {
		t.Fatalf("later-header case must match with version, got=%v", got)
	}

	// 2. Guard equivalence: Server AFTER the blank line (in the body)
	//    must NOT match — the original guard stops the loop at the
	//    blank line and so must the rewrite. / 守卫等价：空行之后
	//    （体部）的 Server 不得命中——原始守卫在空行处终止循环，
	//    改写版必须同样终止。
	afterBlank := bytesToLatin1String([]byte("HTTP/1.0 200 OK\r\n\r\nServer: Cherokee/0.99.30\r\n"))
	if re.MatchString(afterBlank) {
		t.Fatal("Server header past the blank line must not match (guard equivalence)")
	}

	// 3. No Server header at all: no match, and no runaway scan. /
	//    完全没有 Server 头：不命中，也不允许失控扫描。
	noHeader := bytesToLatin1String([]byte("HTTP/1.0 200 OK\r\nContent-Type: text/html\r\n\r\n<html>Server: Cherokee/0.99.30</html>"))
	if re.MatchString(noHeader) {
		t.Fatal("Server in the body must not match")
	}

	// 4. Server as the FIRST header: the lazy loop takes zero
	//    iterations. / Server 是第一个头：惰性循环零次迭代。
	first := bytesToLatin1String([]byte("HTTP/1.0 200 OK\r\nServer: Cherokee/0.99.30\r\n\r\n"))
	if got := re.FindStringSubmatch(first); got == nil || got[1] != "0.99.30" {
		t.Fatalf("first-header case must match with version, got=%v", got)
	}
}
