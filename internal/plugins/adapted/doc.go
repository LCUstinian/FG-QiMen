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
// Plugins are organized by domain for easy navigation:
//   - database/    : PG / MySQL / MSSQL / Oracle / MongoDB / ES / Redis / Memcached
//   - remote/      : SSH / Telnet / VNC / WinRM / IPMI / RDP
//   - messaging/   : RabbitMQ
//   - filestorage/ : NFS / SMB / Rsync
//   - email/       : SMTP / POP3 / IMAP
//   - network/     : SNMP / SOCKS5 / LDAP / Modbus / BACnet / Docker
//   - web/         : http + webtitle (HTTP fingerprinting framework)
//
// To register all built-in plugins, cmd/root.go blank-imports each
// category package. Each category's individual plugin subdirs
// register themselves via their own init().
//
// 插件按域分组便于导航：/ web/ 含 http + webtitle（HTTP 指纹框架）。
// 注册所有内置插件：cmd/root.go blank-import 每个 category 包，
// 各 category 的子目录通过自己的 init() 注册。
package adapted

import (
	// Each category's subdirs self-register via init().
	// 各 category 子目录通过自己的 init() 注册。
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/email"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/filestorage"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/messaging"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web"
)
