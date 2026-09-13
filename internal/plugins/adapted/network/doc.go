// Package network is the network category for service Identify
// plugins. Subdirectories (/bacnet, /dns, /docker, /ldap, /modbus,
// /ntp, /snmp, /snmpv3, /socks5, /tftp) are self-registering plugins
// (each has its own init() that calls plugins.Register).
//
// This doc.go blank-imports every subpackage so a binary that imports
// only internal/plugins/adapted (via cmd/root.go) registers the whole
// category. History: the category was empty of imports and its
// plugins never ran in production — caught by
// TestAggregationImportsAllSubpackages in the parent package.
//
// 包 network 是 adapted 下的网络类目包。子目录（/bacnet、/dns、
// /docker、/ldap、/modbus、/ntp、/snmp、/snmpv3、/socks5、/tftp）是
// 自注册插件（各自 init() 调 plugins.Register）。
//
// 本 doc.go blank-import 全部子包，使只 import
// internal/plugins/adapted 的二进制（经 cmd/root.go）注册整个类目。
// 历史：本类目此前无任何 import，插件从未在生产运行——由父包的
// TestAggregationImportsAllSubpackages 抓出。
package network

import (
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/bacnet"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/dns"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/docker"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/ldap"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/modbus"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/ntp"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/snmp"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/snmpv3"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/socks5"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/network/tftp"
)
