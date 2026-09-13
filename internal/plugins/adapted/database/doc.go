// Package database is the database category for service Identify
// plugins. Subdirectories like /redis, /mysql, etc. are
// self-registering plugins (each has its own init() that calls
// plugins.Register).
//
// This doc.go blank-imports every subpackage so a binary that imports
// only internal/plugins/adapted (via cmd/root.go) registers the whole
// category. History: the comment here used to claim the parent
// adapted package imported the subdirs directly — it didn't (it only
// imports the category packages), and this file was an empty
// placeholder, so none of these plugins ever ran in production until
// TestAggregationImportsAllSubpackages caught it.
//
// 包 database 是 adapted 下的 database 类目包。子目录（/redis、/mysql
// 等）是自注册插件（各自 init() 调 plugins.Register）。
//
// 本 doc.go blank-import 全部子包，使只 import
// internal/plugins/adapted 的二进制（经 cmd/root.go）注册整个类目。
// 历史：本文件注释曾声称上层 adapted 包直接 import 各子目录——实际
// 没有（它只 import 类目包），而本文件是空占位，导致这些插件在
// TestAggregationImportsAllSubpackages 抓出前从未在生产运行。
package database

import (
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/elasticsearch"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/memcached"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/mongodb"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/mssql"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/mysql"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/oracle"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/postgresql"
	_ "github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/database/redis"
)
