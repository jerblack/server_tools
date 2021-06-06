module github.com/jerblack/server_tools/hydra

go 1.16

require (
	github.com/jerblack/go-libdeluge v0.5.5-0.20210422142137-f8aa57e57d6a
	github.com/jerblack/server_tools/base v0.0.0-20210603055124-7c7ca4553ee8
	github.com/jerblack/server_tools/base.db v0.0.0-20210606104326-1e88cd37b6a2
)

replace (
	github.com/jerblack/server_tools/base => ../base
	github.com/jerblack/server_tools/base.db => ../base.db
)
