set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\mux"
go build -ldflags="-s -w"
wsl scp /mnt/c/Users/jeremy/Google\ Drive/go/src/server_tools/mux/mux server.home:~
wsl ssh server.home sudo mv mux /usr/local/bin
