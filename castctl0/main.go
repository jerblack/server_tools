package main

import (
	"github.com/jerblack/server_tools/base"
)

const (
	ccIp   = "192.168.0.223" // "Living Room Chromecast"
	ccPort = 8009
	haUri  = "http://192.168.0.30:1880/endpoint/"
)

var (
	catFolders = []string{
		"/z/_cat_video/Paul_Dinning/",
		"/z/_cat_video/Handsome_Nature/",
	}
	cast   *Cast
	notify *Notify
	db     *PlayedDb
)

func main() {
	db = NewDb()
	notify = NewNotify()
	cast = NewCast()
	getPlaylists()
	startWeb()
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	fileExists = base.FileExists
)
