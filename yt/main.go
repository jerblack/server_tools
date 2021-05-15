package main

import . "github.com/jerblack/server_tools/base"

var (
	downloadArchive = "/home/jeremy/.config/yt/downloads.yt"
	rclonePath      = "google:go/src/server_tools/yt/"
	rcloneConfig    = "/home/jeremy/.config/rclone/rclone.conf"
	rclone          = "/usr/bin/rclone"
	config          = "/home/jeremy/.config/yt/ytdl.conf"
	playlists       = map[string]string{
		"Paul Dinning":    "https://www.youtube.com/playlist?list=UUPJXfmxMYAoH02CFudZxmgg",
		"Handsome Nature": "https://www.youtube.com/playlist?list=UUJLIwYrmwgwbTzgmB5yVc7Q",
	}
	ytDl = "/usr/bin/youtube-dl"
)

func main() {
	for title, playlist := range playlists {
		p("downloading new videos in '%s' playlist", title)
		e := run(ytDl, "--config-location", config, playlist)
		chk(e)
	}
	p("backing up download archive to google drive")
	e := run(rclone, "--config", rcloneConfig, "copy", downloadArchive, rclonePath)
	chk(e)
}

var (
	p   = P
	chk = Chk
	run = Run
)
