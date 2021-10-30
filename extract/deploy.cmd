set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\extract"
go build -ldflags="-s -w"
scp extract 192.168.0.5:~
ssh 192.168.0.5 sudo mv extract /usr/local/bin
ssh 192.168.0.5 sudo chmod +x /usr/local/bin/extract
