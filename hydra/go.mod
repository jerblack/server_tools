module github.com/jerblack/server_tools/hydra

go 1.17

require (
	github.com/jerblack/go-libdeluge v0.5.5-0.20210422142137-f8aa57e57d6a
	github.com/jerblack/server_tools/base v0.0.0-20210603055124-7c7ca4553ee8
	github.com/jerblack/server_tools/base.db v0.0.0-20210606104326-1e88cd37b6a2
)

require (
	github.com/gdm85/go-rencode v0.1.6 // indirect
	github.com/go-gomail/gomail v0.0.0-20160411212932-81ebce5c23df // indirect
	github.com/mattn/go-runewidth v0.0.10 // indirect
	github.com/mattn/go-sqlite3 v1.14.7 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/schollz/progressbar/v3 v3.8.0 // indirect
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83 // indirect
	golang.org/x/sys v0.0.0-20210223095934-7937bea0104d // indirect
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
)

replace (
	github.com/jerblack/server_tools/base => ../base
	github.com/jerblack/server_tools/base.db => ../base.db
)
