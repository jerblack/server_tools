set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\mux"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/mux/mux 192.168.0.5:~
wsl ssh 192.168.0.5 sudo mv mux /usr/bin
