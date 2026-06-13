// Package database is the database category for service Identify
// plugins. Subdirectories like /redis, /mysql, etc. are
// self-registering plugins (each has its own init() that calls
// plugins.Register).
//
// 包 database 是 adapted 下的 database 类目包。子目录（/redis、/mysql
// 等）是自注册插件（各自 init() 调 plugins.Register）。
//
// This doc.go is a placeholder so the directory is a valid Go
// package. The category parent (internal/plugins/adapted/doc.go)
// blank-imports each subdir explicitly to trigger their init().
// / 本 doc.go 是占位，让目录成为合法 Go 包。上层 adapted 包
// 显式 blank-import 每个子目录来触发 init()。
package database
