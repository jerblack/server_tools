package main

import (
	"encoding/json"
	"fmt"
	"github.com/vishen/go-chromecast/application"
	"os"
	"regexp"
	"time"
)

type NpTime float64

func (n NpTime) MarshalJSON() ([]byte, error) {
	nInt := int(n)
	hr := nInt / (60 * 60)
	x := nInt % (60 * 60)
	min := x / 60
	sec := x % 60

	t := fmt.Sprintf("%02d:%02d:%02d", hr, min, sec)
	return json.Marshal(t)
}

type NowPlaying struct {
	AppLoaded       string  `json:"app_loaded"`
	Date            string  `json:"date"`
	Id              string  `json:"id"`
	Playlist        string  `json:"playlist"`
	Title           string  `json:"title"`
	Paused          bool    `json:"paused"`
	Position        NpTime  `json:"position"`
	Duration        NpTime  `json:"duration"`
	Percent         float64 `json:"percent"`
	Volume          int     `json:"volume"`
	Muted           bool    `json:"muted"`
	filename        string
	app             *application.Application
	stopPollingChan chan struct{}
	lastUpdate      string
}

func (np *NowPlaying) empty() {
	np.AppLoaded = "Off"
	np.Paused = true
	np.Position = 0
	np.Duration = 0
	np.Percent = 0
	np.Volume = 0
	np.Muted = false
	np.Id = ""
	np.Title = ""
	np.Date = ""
	np.Playlist = ""
}

func (np *NowPlaying) getUpdate() {
	defer func() {
		r := recover()
		if r != nil {
			p("error occurred during app.update: %s", r)
		}
	}()
	e := np.app.Update()

	if e != nil {
		np.empty()
		return
	}
	castApp, castMedia, castVol := np.app.Status()

	if castApp == nil {
		np.AppLoaded = "Idle"
	} else {
		np.AppLoaded = castApp.DisplayName
	}
	if castMedia != nil {
		np.Paused = castMedia.PlayerState == "PAUSED"
		np.Position = NpTime(castMedia.CurrentTime)
		np.Duration = NpTime(castMedia.Media.Duration)
		if np.Duration != 0 {
			np.Percent = float64(np.Position / np.Duration * 100)
		} else {
			np.Percent = 0
		}
	} else {
		np.Paused = true
		np.Position = 0
		np.Duration = 0
		np.Percent = 0
	}
	if castVol != nil {
		np.Volume = int(castVol.Level * 100)
		np.Muted = castVol.Muted
	} else {
		np.Volume = 0
		np.Muted = false
	}
}
func (np *NowPlaying) sendUpdate() {
	b, e := json.Marshal(np)
	if e != nil {
		p("error from: %+v", np)
		chk(e)
	} else {
		update := string(b)
		if update != np.lastUpdate {
			notify.pushUpdate("now_playing", update)
			np.lastUpdate = update
		}
	}
}
func (np *NowPlaying) startPolling(app *application.Application) {
	np.app = app
	np.stopPollingChan = make(chan struct{}, 1)
	for {
		select {
		case <-time.After(1 * time.Second):
			np.getUpdate()
			np.sendUpdate()
		case <-np.stopPollingChan:
			return
		}
	}
}
func (np *NowPlaying) stopPolling() {
	if np.stopPollingChan != nil {
		np.stopPollingChan <- struct{}{}
	}
}

type Title struct {
	Date, Title, Id string
}

func parseTitle(title string) *Title {
	var t Title
	var compRegEx = regexp.MustCompile(`(?P<date>\d{8})-(?P<title>.+) \[(?P<id>.+)\]`)
	match := compRegEx.FindStringSubmatch(title)

	titleParts := make(map[string]string)
	for i, name := range compRegEx.SubexpNames() {
		if i > 0 && i <= len(match) {
			titleParts[name] = match[i]
		}
	}
	d := titleParts["date"]
	d = fmt.Sprintf("%s-%s-%s", d[0:4], d[4:6], d[6:])
	t.Date = d
	id := titleParts["id"]
	t.Id = fmt.Sprintf("<span onclick='window.open(\"https://www.youtube.com/watch?v=%s\");'>%s</span>", id, id)
	t.Title = titleParts["title"]
	return &t
}

func deleteCurrent() {
	target := cast.np.filename
	cast.next()
	if fileExists(target) {
		time.Sleep(3 * time.Second)
		e := os.Remove(target)
		chk(e)
	}
}
