set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\extract"
go build -ldflags="-s -w"
wsl scp extract server.home:~
wsl ssh server.home sudo mv extract /usr/local/bin
wsl ssh server.home sudo chmod +x /usr/local/bin/extract
