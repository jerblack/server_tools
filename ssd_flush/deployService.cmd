cd "C:\Users\jeremy\Google Drive\go\src\server_tools\ssd_flush"
wsl scp ssd_flush.* 192.168.0.5:~
wsl ssh 192.168.0.5 "sudo mv ssd_flush.* /lib/systemd/system/"
wsl ssh 192.168.0.5 "sudo systemctl enable ssd_flush.timer"
