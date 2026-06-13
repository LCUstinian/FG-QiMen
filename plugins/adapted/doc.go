// Package adapted contains service Identify plugins. Each plugin
// implements the unified plugins.Plugin interface and registers
// itself via init(). Attribution for any reused upstream code lives
// in README.md (the per-file headers only summarize the
// modification history).
//
// Package adapted 包含服务识别插件。每个插件实现统一的
// plugins.Plugin 接口并通过 init() 自动注册。重用上游代码的归属
// 集中在 README.md（每个文件头部只简述修改历史）。
//
// v0.1 status:
//   - ssh.go: hand-written from scratch.
//   - http.go: hand-written, minimal HTTP probe for web services.
//   - webtitle/: HTTP fingerprinting framework — protocol detect +
//     redirect follow + title + favicon (mmh3 + MD5) +
//     FingerprintHub 3139 JSON rules + ~300 hardcoded regex rules.
//   - All other service plugins: hand-written Identify via raw
//     protocol or stdlib / driver.
//
// v0.1 状态：
//   - ssh.go：从零手写。
//   - http.go：手写，最小 HTTP 探测用于 Web 服务。
//   - webtitle/：HTTP 指纹框架——协议检测 + 重定向跟随 + 标题 +
//     favicon (mmh3 + MD5) + FingerprintHub 3139 JSON 规则 +
//     约 300 条硬编码正则规则。
//   - 其余服务插件：手写 Identify，用原生协议或标准库 / 驱动。
package adapted

import (
	// webtitle registers via its own init().
	// webtitle 通过自己的 init() 注册。
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/webtitle"
	// Simple service plugins (Identify-only in v0.1).
	// / 简单服务插件（v0.1 仅识别）。
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/redis"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/mongodb"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/postgresql"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/mssql"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/smb"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/smtp"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/snmp"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/ldap"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/memcached"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/elasticsearch"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/rdp"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/vnc"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/telnet"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/oracle"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/winrm"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/pop3"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/imap"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/snmp"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/rsync"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/docker"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/rabbitmq"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/modbus"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/ipmi"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/bacnet"
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted/nfs"
)
