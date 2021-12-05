set GOOS=linux
set CGO_ENABLED=1
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\hydra"
go build -ldflags="-s -w"
wsl scp hydra server:~
wsl ssh server "sudo systemctl stop hydra"
wsl ssh server "sudo mv hydra /usr/local/bin"
