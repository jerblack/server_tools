cd "C:\Users\jeremy\Google Drive\go\src\server_tools\snapraid_mgr"
wsl CGO_ENABLED=1 /usr/local/go/bin/go build -ldflags="-s -w"
wsl scp snapraid_mgr 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop snapraid_mgr.service && sudo mv snapraid_mgr /usr/local/bin"
