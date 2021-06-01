set GOOS=linux
set CGO_ENABLED=1
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\hydra"
wsl CGO_ENABLED=1 /usr/local/go/bin/go build -ldflags="-s -w"
wsl scp hydra 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop hydra"
wsl ssh 192.168.0.5 "sudo mv hydra /usr/local/bin"
wsl ssh 192.168.0.5 "sudo systemctl start hydra"
