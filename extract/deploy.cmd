set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\extract"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/extract/extract 192.168.0.5:~
wsl ssh 192.168.0.5 sudo mv extract /usr/local/bin
