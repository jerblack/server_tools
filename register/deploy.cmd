cd "C:\Users\jeremy\Google Drive\go\src\server_tools\register"
wsl CGO_ENABLED=1 /usr/local/go/bin/go build -ldflags="-s -w"
wsl scp register 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop register"
wsl ssh 192.168.0.5 "sudo mv register /usr/local/bin"
wsl ssh 192.168.0.5 "sudo systemctl start register"