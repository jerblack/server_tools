module github.com/jerblack/server_tools/register

go 1.16

require (
	github.com/containerd/containerd v1.5.2 // indirect
	github.com/docker/docker v20.10.7+incompatible
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/jerblack/server_tools/base v0.0.0-20210630201650-860fdfa4309b
	github.com/moby/term v0.0.0-20210610120745-9d4ed1856297 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6 // indirect
	google.golang.org/grpc v1.38.0 // indirect
)

replace github.com/jerblack/server_tools/base => ../base
