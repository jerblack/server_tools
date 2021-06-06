module github.com/jerblack/server_tools/base.db

go 1.16

replace github.com/jerblack/server_tools/base => ../base

require (
	github.com/jerblack/server_tools/base v0.0.0-00010101000000-000000000000
	github.com/mattn/go-sqlite3 v1.14.7
)
