package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

var (
	notificationMethods = []string{
		"Player.OnAVChange",
		"Player.OnAVStart",
		"Player.OnPause",
		"Player.OnPlay",
		"Player.OnPropertyChanged",
		"Player.OnResume",
		"Player.OnSeek",
		"Player.OnSpeedChanged",
		"Player.OnStop",
	}
	playMethods = []string{"Player.OnAVChange", "Player.OnAVStart", "Player.OnPlay",
		"Player.OnResume", "Player.OnSeek", "Player.OnSpeedChanged"}
	stopMethods = []string{"Player.OnPause", "Player.OnStop"}
)

type KodiEvent struct {
	Item struct {
		Title string `json:"title"`
	} `json:"item"`
	Player struct {
		Time struct {
			Hours   int `json:"hours"`
			Minutes int `json:"minutes"`
			Seconds int `json:"seconds"`
		} `json:"time"`
	} `json:"player"`
	method     string
	path       string
	playedPath string
	file       string
	title      string
	date       string
	id         string
	secs       int32
}

// kodi event only reports filename, not path
// getPath looks in all playlist folders for file and returns path of found file
// found file paths are cached in cachedPaths
func (ev *KodiEvent) getPath() {
	path, ok := pathCache[ev.file]
	if ok {
		ev.path = path.file
		ev.playedPath = path.played
	} else {
		for _, pl := range playlists {
			src := filepath.Join(pl.LocalPath, ev.file)
			if fileExists(src) {
				pc := cachedPaths{
					file:   src,
					played: filepath.Join(pl.PlayedPath, ev.file),
				}
				pathCache[ev.file] = &pc
				ev.path = pc.file
				ev.playedPath = pc.played
			}
		}
	}
	if ev.path == "" {
		p("video file not found for %s", ev.file)
		os.Exit(1)
	}
}
func (ev *KodiEvent) parse(d interface{}) {
	b, e := json.Marshal(d)
	chk(e)
	e = json.Unmarshal(b, ev)
	chk(e)

	t := parseTitle(ev.Item.Title)

	ev.file = ev.Item.Title
	ev.title = t.Title
	ev.date = t.Date
	ev.id = t.Id
	ev.secs = int32((ev.Player.Time.Hours * 60 * 60) + (ev.Player.Time.Minutes * 60) + ev.Player.Time.Seconds)
	ev.getPath()
}
