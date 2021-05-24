set GOOS=linux
cd "C:\Users\jeremy\Google Drive\go\src\server_tools\ssd_flush"
go build -ldflags="-s -w"
wsl scp ssd_flush 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo systemctl stop ssd_flush.service"
wsl ssh 192.168.0.5 "sudo mv ssd_flush /usr/local/bin/"
