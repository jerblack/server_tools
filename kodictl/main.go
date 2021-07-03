package main

import (
	"fmt"
	"github.com/jerblack/server_tools/base"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	kodiIp         = "192.168.0.226:9090"
	haUri          = "http://192.168.0.30:1880/endpoint/"
	listenIp       = "0.0.0.0"
	playlistFolder = "/z/_cat_video/"
)

var (
	ip, uri    string
	playlists  map[string]*Playlist
	kodi       *Kodi
	np         *NowPlaying
	notify     *Notify
	kodiEvents chan *KodiEvent
)

type Playlist struct {
	LocalPath  string `json:"local_path"`
	KodiPath   string `json:"kodi_path"`
	PlayedPath string `json:"played_path"`
	Name       string `json:"name"`
}

func getUri() {
	uri = fmt.Sprintf("https://kodictl/")
}

// getPlaylists enumerates all subfolders in main folder and creates map of Playlist entries
// map key -> lower case subfolder name
// KodiPath -> smb path to folder
// LocalPath -> path from perspective of server
// PlayedPath -> local path to played subfolder
// Name -> subfolder name with spaces replaced with underscores
func getPlaylists() {
	playlists = make(map[string]*Playlist)
	entries, e := os.ReadDir(playlistFolder)
	chkFatal(e)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(playlistFolder, entry.Name())
		pl := Playlist{
			LocalPath:  path,
			PlayedPath: filepath.Join(path, ".played"),
			KodiPath:   "smb://192.168.0.5" + strings.ReplaceAll(path, `\`, `/`),
			Name:       strings.ReplaceAll(entry.Name(), "_", " "),
		}
		playlists[strings.ToLower(entry.Name())] = &pl
	}
}

func printTimer() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ticker.C:
			fmt.Printf("\r%f%% | position %d secs | duration %d secs", np.Percent, np.Position, np.Duration)
		}
	}
}

func main() {
	p("starting kodictl")
	getUri()
	getPlaylists()
	notify = NewNotify()
	kodi = NewKodi()
	np = NewNowPlaying()

	go kodi.handleEvents()
	go startWeb()
	//go printTimer()
	select {}
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	isAny      = base.IsAny
	fileExists = base.FileExists
)

func hasString(arr []string, str string) bool {
	for _, s := range arr {
		if s == str {
			return true
		}
	}
	return false
}
