package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type NpTime int32

func (n NpTime) MarshalJSON() ([]byte, error) {
	hr := n / (60 * 60)
	x := n % (60 * 60)
	min := x / 60
	sec := x % 60

	t := fmt.Sprintf("%02d:%02d:%02d", hr, min, sec)
	return json.Marshal(t)
}

type NowPlaying struct {
	Playing    bool    `json:"playing"`
	Playlist   string  `json:"playlist"`
	Title      string  `json:"title"`
	Date       string  `json:"date"`
	Id         string  `json:"id"`
	Percent    float64 `json:"percent"`
	Position   NpTime  `json:"position"`
	Duration   NpTime  `json:"duration"`
	stopTicker chan bool
}

// send now playing information to connected web clients through event stream
func (np *NowPlaying) send() {
	b, e := json.Marshal(np)
	if e != nil {
		p("error from: %+v", np)
		chk(e)
	} else {
		notify.pushUpdate("now_playing", string(b))
	}
}

func (np *NowPlaying) startTimer() {
	np.stopTicker = make(chan bool)
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-np.stopTicker:
			ticker.Stop()
			return
		case <-ticker.C:
			if !np.Playing {
				continue
			}
			np.Position += 1
			np.Percent = float64(np.Position) / float64(np.Duration) * 100
			//if np.Position % 4 == 0 {
			np.send()
			//}
		}
	}
}
func (np *NowPlaying) stopTimer() {
	np.stopTicker <- true
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
	t.Date = titleParts["date"]
	t.Id = titleParts["id"]
	t.Title = titleParts["title"]
	return &t
}

type KodiNowPlaying struct {
	Speed      int     `json:"speed"`
	Percentage float64 `json:"percentage"`
	Time       struct {
		Hours   int32 `json:"hours"`
		Minutes int32 `json:"minutes"`
		Seconds int32 `json:"seconds"`
	} `json:"time"`
	TotalTime struct {
		Hours   int32 `json:"hours"`
		Minutes int32 `json:"minutes"`
		Seconds int32 `json:"seconds"`
	} `json:"totaltime"`
	Item struct {
		File  string `json:"file"`
		Label string `json:"label"`
	} `json:"item"`
}

func NewNowPlaying() *NowPlaying {
	if np != nil && np.stopTicker != nil {
		np.stopTimer()
	}
	var knp KodiNowPlaying
	np := NowPlaying{}
	kt, e := kodi.getPlayerItem()
	chk(e)
	b, _ := json.Marshal(kt)
	e = json.Unmarshal(b, &knp)
	chk(e)
	kp, e := kodi.getPlayerProperties()
	chk(e)
	b, _ = json.Marshal(kp)
	e = json.Unmarshal(b, &knp)
	chk(e)

	np.Playing = knp.Speed == 1
	t := parseTitle(knp.Item.Label)
	np.Title = t.Title
	np.Date = t.Date
	np.Id = t.Id
	for _, pl := range playlists {
		if strings.HasPrefix(knp.Item.File, pl.KodiPath) {
			np.Playlist = pl.Name
		}
	}
	np.Percent = knp.Percentage * 100
	np.Position = NpTime((knp.Time.Hours * 60 * 60) + (knp.Time.Minutes * 60) + knp.Time.Seconds)
	np.Duration = NpTime((knp.TotalTime.Hours * 60 * 60) + (knp.TotalTime.Minutes * 60) + knp.TotalTime.Seconds)

	np.send()
	go np.startTimer()
	return &np

}
