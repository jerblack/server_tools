set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\ssd_flush"
go build -ldflags="-s -w"
scp ssd_flush server.home:~
ssh server.home "sudo systemctl stop ssd_flush.service"
ssh server.home "sudo mv ssd_flush /usr/local/bin/"
ssh server.home "sudo chmod +x /usr/local/bin/ssd_flush"