// Package migrations 只负责把 SQL 迁移文件编译进二进制。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
