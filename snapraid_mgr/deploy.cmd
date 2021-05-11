set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\snapraid_mgr"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/snapraid_mgr/snapraid_mgr 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop snapraid_mgr.service && sudo mv snapraid_mgr /usr/bin"
