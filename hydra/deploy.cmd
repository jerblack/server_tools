set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\hydra"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/hydra/hydra 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop hydra && sudo mv hydra /usr/bin && sudo systemctl start hydra"
