set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\yt"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/yt/yt 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop yt.service && sudo mv yt /usr/bin"
