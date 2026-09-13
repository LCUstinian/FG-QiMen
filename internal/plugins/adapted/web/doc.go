// Package adapted (directory web/) is the web category for service
// Identify plugins: the basic http banner grabber lives in this
// package itself (http.go), while the subdirectories (webtitle,
// jenkins, kibana, weblogic) are self-registering plugins (each has
// its own init() that calls plugins.Register).
//
// This doc.go blank-imports every subpackage so a binary that imports
// only internal/plugins/adapted (via cmd/root.go) registers the whole
// category. History: until 2026-09-13 only webtitle was wired here
// (from http.go, itself a late fix) — jenkins, kibana and weblogic
// never ran in production; caught by
// TestAggregationImportsAllSubpackages in the parent package.
//
// 包 adapted（目录 web/）是 adapted 下的 web 类目包：基础 http
// banner 抓取器就在本包内（http.go），子目录（webtitle、jenkins、
// kibana、weblogic）是自注册插件（各自 init() 调 plugins.Register）。
//
// 本 doc.go blank-import 全部子包，使只 import
// internal/plugins/adapted 的二进制（经 cmd/root.go）注册整个类目。
// 历史：2026-09-13 之前只有 webtitle 被接线（在 http.go 里，本身是
// 晚期修复）——jenkins、kibana、weblogic 从未在生产运行；由父包的
// TestAggregationImportsAllSubpackages 抓出。
package adapted

import (
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/jenkins"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/kibana"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/weblogic"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle"
)
