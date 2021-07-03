package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	pathCache map[string]*cachedPaths
)

type cachedPaths struct {
	file, played string
}

func NewKodi() *Kodi {
	pathCache = make(map[string]*cachedPaths)
	k := Kodi{}
	c, e := NewClient(kodiIp, nil)
	chkFatal(e)

	k.client = c
	k.events = make(chan *KodiEvent, 128)
	return &k
}

type Kodi struct {
	client    *Client
	events    chan *KodiEvent
	lastEvent *KodiEvent
}

func (k *Kodi) _action(action string) {
	method := `Input.ExecuteAction`
	params := map[string]string{"action": action}
	e := k.client.Notify(method, params)
	chk(e)
}
func (k *Kodi) playPause() { k._action("playpause") }
func (k *Kodi) play()      { k._action("play") }
func (k *Kodi) pause()     { k._action("pause") }
func (k *Kodi) next() {
	k._action("skipnext")
}
func (k *Kodi) prev() {
	k._action("skipprevious")
}
func (k *Kodi) back()        { k._action("back") }
func (k *Kodi) info()        { k._action("info") }
func (k *Kodi) menu()        { k._action("menu") }
func (k *Kodi) rightClick()  { k._action("rightclick") }
func (k *Kodi) contextMenu() { k._action("contextmenu") }
func (k *Kodi) enter()       { k._action("enter") }
func (k *Kodi) up()          { k._action("up") }
func (k *Kodi) down()        { k._action("down") }
func (k *Kodi) left()        { k._action("left") }
func (k *Kodi) right()       { k._action("right") }
func (k *Kodi) selectBtn()   { k._action("select") }
func (k *Kodi) stop()        { k._action("stop") }
func (k *Kodi) _method(method string, params interface{}) {
	e := k.client.Notify(method, params)
	chk(e)
}
func (k *Kodi) home() { k._method("Input.Home", nil) }
func (k *Kodi) seekTime(seconds int) {
	var hours, minutes int
	if seconds > 59 {
		minutes = seconds / 60
		seconds = seconds % 60
	}
	if minutes > 59 {
		hours = minutes / 60
		minutes = minutes % 60
	}

	k._method("Player.Seek", map[string]interface{}{
		"playerid": 1,
		"value": map[string]interface{}{
			"time": map[string]int{
				"seconds": seconds,
				"minutes": minutes,
				"hours":   hours,
			},
		},
	})
}
func (k *Kodi) seekPercent(percent float64) {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	k._method("Player.Seek", map[string]interface{}{
		"playerid": 1,
		"value": map[string]float64{
			"percentage": percent,
		},
	})
}
func (k *Kodi) playPlaylist(pl *Playlist) {
	params := map[string]interface{}{
		"item": map[string]interface{}{
			"directory": pl.KodiPath,
		},
	}
	e := k.client.Notify("Player.Open", params)
	chk(e)
}
func (k *Kodi) getPlayerItem() (map[string]interface{}, error) {
	params := map[string]interface{}{
		"playerid":   1,
		"properties": []string{"title", "file"},
	}
	rsp, e := k.client.Call("Player.GetItem", params)
	if e != nil {
		fmt.Println(e)
		return nil, e
	}

	return rsp.(map[string]interface{}), nil
}
func (k *Kodi) getPlayerProperties() (map[string]interface{}, error) {
	params := map[string]interface{}{
		"playerid":   1,
		"properties": []string{"speed", "playlistid", "percentage", "time", "totaltime"},
	}
	rsp, e := k.client.Call("Player.GetProperties", params)
	if e != nil {
		fmt.Println(e)
		return nil, e
	}

	return rsp.(map[string]interface{}), nil
}

func (k *Kodi) delete() {
	if k.lastEvent != nil {
		target := k.lastEvent
		try := 0
		k.next()
		for try < 10 {
			time.Sleep(1 * time.Second)
			var e error
			if fileExists(target.path) {
				e = os.Remove(target.path)
				chk(e)
				if e != nil {
					p("deleted file %s", target.path)
					break
				}
				try += 1
				p("delete file failed. retrying %d/%d", try+1, 10)
			} else {
				p("can't delete. file does not exist: %s", target.path)
				return
			}
		}
		if fileExists(target.path) {
			p("delete file failed after 10 tries. giving up: %s")
		}
	}
}

func (k *Kodi) handleEvents() {
	handlerFn := func(m string, d interface{}) {
		var ev KodiEvent
		ev.parse(d)
		ev.method = m
		k.events <- &ev
	}
	for _, method := range notificationMethods {
		k.client.Handle(method, handlerFn)
	}

	moved := make([]string, 0)

	for ev := range k.events {
		if isAny(ev.method, playMethods...) {
			np.Playing = true
		}
		if isAny(ev.method, stopMethods...) {
			np.Playing = false
		}
		np.Title = ev.title
		np.Date = ev.date
		np.Id = ev.id
		if ev.secs != 0 {
			np.Position = NpTime(ev.secs)
		}
		np.send()

		if k.lastEvent == nil {
			k.lastEvent = ev
		} else {
			if k.lastEvent.file != ev.file {
				if !hasString(moved, k.lastEvent.path) {
					p("video changed. moving previous video to .played sub-folder")
					e := os.MkdirAll(filepath.Dir(k.lastEvent.playedPath), 0775)
					chkFatal(e)
					p("moving watched video to %s", k.lastEvent.playedPath)
					e = os.Rename(k.lastEvent.path, k.lastEvent.playedPath)
					chkFatal(e)
					moved = append(moved, k.lastEvent.path)
				}
				k.lastEvent = nil
				np = NewNowPlaying()
			}
		}
	}
}
