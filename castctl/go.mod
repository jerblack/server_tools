module github.com/jerblack/server_tools/castctl

go 1.16

require (
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/go-sql-driver/mysql v1.6.0
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/h2non/filetype v1.1.1 // indirect
	github.com/jerblack/server_tools/base v0.0.0-00010101000000-000000000000
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/vishen/go-chromecast v0.2.9
)

replace github.com/jerblack/server_tools/base => ../base
