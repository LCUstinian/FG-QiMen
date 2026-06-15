# Third-Party Notices & License Texts

This file lists third-party software used by FG-QiMen and preserves
the license texts and attribution required by their respective
licenses. Generated for v0.2.1 (build: `v0.2.0`).

If you fork or re-distribute this binary, BSD-3-Clause and
Apache-2.0 dependencies require you to reproduce their copyright
notices and license texts; this file is the canonical
attribution bundle.

The list below is sourced from `go.mod` (Go 1.26 toolchain, no
replace directives) and the Go module cache. A future
automated generation step is tracked in the README under
"## License attribution" (TODO: switch to `go-licenses` or
similar to keep this file in lockstep with the actual
dependency closure).

# 上游代码归属与许可证文本

本文件列出 FG-QiMen 使用的第三方软件，并按各自许可证要求保留
许可证文本与归属。本文件为 v0.2.1 版本（构建：v0.2.0）。

如需 fork 或再分发本二进制，BSD-3-Clause 与 Apache-2.0 依赖
要求复刻版权声明与许可证文本；本文件即规范归属包。

下方列表源自 `go.mod`（Go 1.26 工具链，无 replace 指令）与
Go module 缓存。README 中"## License attribution"小节追
踪了未来用 `go-licenses` 等工具自动生成的 TODO。

---

## 1. The Go Authors (Go Standard Library)

- **License**: BSD-3-Clause
- **Used by**: all Go programs at compile time
- **License text**: https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:LICENSE

---

## 2. shadow1ng/fscan (MIT) — internal/plugins/adapted/* (fscan-derived)

Some of the per-protocol authenticator files under
`internal/plugins/adapted/database/{mysql,mssql,oracle,postgresql,redis,mongodb,memcached,elasticsearch}.go`,
`internal/plugins/adapted/email/{imap,pop3}.go`,
`internal/plugins/adapted/filestorage/{nfs,smb,rsync}.go`,
`internal/plugins/adapted/messaging/rabbitmq.go`,
`internal/plugins/adapted/network/{bacnet,docker,ldap,modbus,snmp,socks5}.go`,
and `internal/plugins/adapted/remote/{ssh,ftp,telnet,vnc,winrm,ipmi}.go`
are derived from shadow1ng/fscan (Copyright (c) 2021 shadow1ng,
MIT License).

- **Upstream**: https://github.com/shadow1ng/fscan
- **License**: MIT — https://opensource.org/licenses/MIT

> Permission is hereby granted, free of charge, to any person
> obtaining a copy of this software and associated documentation
> files (the "Software"), to deal in the Software without
> restriction, including without limitation the rights to use,
> copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to furnish persons to whom the
> Software is furnished to do so, subject to the following
> conditions:
>
> The above copyright notice and this permission notice shall be
> included in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
> EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
> OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
> NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
> HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
> WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
> FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
> OTHER DEALINGS IN THE SOFTWARE.

---

## 3. cn-kali-team / FingerprintHub web-fingerprint data (MIT)

The embedded web fingerprint rules in
`internal/plugins/adapted/web/webtitle/fingerprint/web_fingerprint_v4.json`
are derived from the FingerprintHub dataset.

- **Upstream**: https://github.com/0x727/FingerprintHub
- **License**: MIT
- **Attribution**: per-rule `author` field inside the JSON

---

## 4. Direct Go dependencies (from go.mod)

The following direct dependencies are listed with their SPDX
license identifier. Full license texts for each are below.

| Module | License | Path / Purpose |
|---|---|---|
| `go.etcd.io/bbolt` | BSD-3-Clause | bbolt DB for project-mode state |
| `golang.org/x/crypto` | BSD-3-Clause | SSH, crypto primitives |
| `golang.org/x/term` | BSD-3-Clause | TTY detection |
| `golang.org/x/sys` | BSD-3-Clause | syscall shims |
| `golang.org/x/text` | BSD-3-Clause | text encoders |
| `golang.org/x/exp` | BSD-3-Clause | experimental helpers |
| `golang.org/x/net` | BSD-3-Clause | network primitives |
| `golang.org/x/sync` | BSD-3-Clause | sync helpers (errgroup) |
| `github.com/spf13/cobra` | BSD-3-Clause | CLI framework |
| `github.com/spf13/pflag` | BSD-3-Clause | flag parsing |
| `github.com/google/uuid` | BSD-3-Clause | UUID generation |
| `filippo.io/edwards25519` | BSD-3-Clause | ed25519 primitives |
| `github.com/go-asn1-ber/asn1-ber` | BSD-2-Clause | LDAP ASN.1/BER |
| `github.com/gosnmp/gosnmp` | BSD-2-Clause | SNMP probes |
| `github.com/pkg/browser` | BSD-2-Clause | open browser (smtp / oauth) |
| `github.com/golang-sql/civil` | Apache-2.0 | civil date types |
| `github.com/golang-sql/sqlexp` | Apache-2.0 | SQL extensions |
| `github.com/microsoft/go-mssqldb` | MIT | MSSQL driver |
| `github.com/go-sql-driver/mysql` | MPL-2.0 | MySQL driver |
| `github.com/jlaffaye/ftp` | ISC | FTP client |
| `github.com/Azure/go-ntlmssp` | MIT | NTLM/Negotiate auth |
| `github.com/sijms/go-ora/v2` | MIT | Oracle driver |
| `github.com/mitchellh/go-vnc` | MIT | VNC client |
| `github.com/go-ldap/ldap/v3` | MIT | LDAP client |
| `github.com/lucasb-eyer/go-colorful` | MIT | colour conversions |
| `github.com/rivo/uniseg` | MIT | text segmentation |
| `github.com/xo/terminfo` | MIT | terminfo parser |
| `github.com/muesli/ansi` | MIT | ANSI helpers |
| `github.com/muesli/cancelreader` | MIT | async console reader |
| `github.com/muesli/termenv` | MIT | terminal env detection |
| `github.com/charmbracelet/bubbletea` | MIT | TUI framework |
| `github.com/charmbracelet/lipgloss` | MIT | TUI styling |
| `github.com/golang-jwt/jwt/v5` | MIT | JWT for OAuth flows |
| `github.com/erikgeiser/coninput` | MIT | Windows console input |
| `github.com/geoffgarside/ber` | MIT | BER encoding (LDAP) |
| `github.com/aymanbagabas/go-osc52/v2` | MIT | OSC52 clipboard |
| `github.com/kylelemons/godebug` | Apache-2.0 | structured debug |

---

### 4.1 BSD-3-Clause (canonical text)

> Redistribution and use in source and binary forms, with or
> without modification, are permitted provided that the following
> conditions are met:
>
> 1. Redistributions of source code must retain the above
>    copyright notice, this list of conditions and the following
>    disclaimer.
> 2. Redistributions in binary form must reproduce the above
>    copyright notice, this list of conditions and the following
>    disclaimer in the documentation and/or other materials
>    provided with the distribution.
> 3. Neither the name of the copyright holder nor the names of
>    its contributors may be used to endorse or promote products
>    derived from this software without specific prior written
>    permission.
>
> THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
> CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES,
> INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
> MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
> DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR
> CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
> SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
> NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
> LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
> HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
> CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR
> OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE,
> EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### 4.2 BSD-2-Clause (canonical text)

> Redistribution and use in source and binary forms, with or
> without modification, are permitted provided that the following
> conditions are met:
>
> 1. Redistributions of source code must retain the above
>    copyright notice, this list of conditions and the following
>    disclaimer.
> 2. Redistributions in binary form must reproduce the above
>    copyright notice, this list of conditions and the following
>    disclaimer in the documentation and/or other materials
>    provided with the distribution.
>
> THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
> CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES,
> INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
> MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
> DISCLAIMED.

### 4.3 Apache-2.0 (canonical text)

> Licensed under the Apache License, Version 2.0 (the
> "License"); you may not use this file except in compliance
> with the License. You may obtain a copy of the License at
>
>     http://www.apache.org/licenses/LICENSE-2.0
>
> Unless required by applicable law or agreed to in writing,
> software distributed under the License is distributed on an
> "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
> either express or implied. See the License for the specific
> language governing permissions and limitations under the
> License.

### 4.4 MIT (canonical text)

> Permission is hereby granted, free of charge, to any person
> obtaining a copy of this software and associated documentation
> files (the "Software"), to deal in the Software without
> restriction, including without limitation the rights to use,
> copy, modify, merge, publish, distribute, sublicense, and/or
> sell copies of the Software, and to furnish persons to whom
> the Software is furnished to do so, subject to the following
> conditions:
>
> The above copyright notice and this permission notice shall be
> included in all copies or substantial portions of the
> Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY
> KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
> WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR
> PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS
> OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
> OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
> OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
> SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

### 4.5 ISC (canonical text)

> Permission to use, copy, modify, and/or distribute this
> software for any purpose with or without fee is hereby
> granted, provided that the above copyright notice and this
> permission notice appear in all copies.
>
> THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS
> ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL
> IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO
> EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
> INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
> WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS,
> WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER
> TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE
> USE OR PERFORMANCE OF THIS SOFTWARE.

### 4.6 MPL-2.0 (canonical text — abbreviated)

The MySQL driver (`github.com/go-sql-driver/mysql`) is dual-licensed
under MPL-2.0 OR a separate commercial license. The MPL-2.0
notice (omitted here for brevity, see upstream) allows
combination with non-MPL code; FG-QiMen's MySQL usage is
client-side and does not modify the driver. For the full text
see https://www.mozilla.org/en-US/MPL/2.0/.

---

## 5. FG-QiMen itself

- **License**: see `LICENSE` (MIT)
- **Copyright**: (c) 2026 LCUstinian
- **Path**: this repository's `LICENSE` file
