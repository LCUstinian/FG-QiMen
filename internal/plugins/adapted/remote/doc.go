// Package remote is the remote-access category for service Identify
// plugins. Subdirectories (/ipmi, /rdp, /rdpnla, /ssh, /telnet, /vnc,
// /winrm) are self-registering plugins (each has its own init() that
// calls plugins.Register).
//
// This doc.go blank-imports every subpackage so a binary that imports
// only internal/plugins/adapted (via cmd/root.go) registers the whole
// category. History: the category was empty of imports and its
// plugins never ran in production — caught by
// TestAggregationImportsAllSubpackages in the parent package.
//
// 包 remote 是 adapted 下的远程访问类目包。子目录（/ipmi、/rdp、
// /rdpnla、/ssh、/telnet、/vnc、/winrm）是自注册插件（各自 init()
// 调 plugins.Register）。
//
// 本 doc.go blank-import 全部子包，使只 import
// internal/plugins/adapted 的二进制（经 cmd/root.go）注册整个类目。
// 历史：本类目此前无任何 import，插件从未在生产运行——由父包的
// TestAggregationImportsAllSubpackages 抓出。
package remote

import (
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/ipmi"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/rdp"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/rdpnla"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/ssh"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/telnet"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/vnc"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/remote/winrm"
)
