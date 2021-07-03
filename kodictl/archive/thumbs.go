package archive

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

var (
	thumbPath     = "/x/.config/kodictl/thumb.png"
	thumbTmp      = "/x/.config/kodictl/tmp.png"
	thumbInterval = 600 * time.Second
)

func NewThumb() *Thumb {
	t := Thumb{}
	t.start()
	return &t
}

type Thumb struct {
	numClients     chan int
	newVid         chan struct{}
	playing        chan bool
	updateNow      chan struct{}
	makeThumbs     bool
	currentNum     int
	currentPlaying bool
}

func (t *Thumb) start() {
	t.numClients = make(chan int, 8)
	t.playing = make(chan bool, 8)
	t.updateNow = make(chan struct{}, 8)
	go t.stateWatcher()
	go t.updater()
}
func (t *Thumb) stateWatcher() {
	for {
		select {
		case n := <-t.numClients:
			if t.currentNum == 0 && n > 0 && core.playing {
				t.makeThumbs = true
				t.updateNow <- struct{}{}
			}
			if n == 0 {
				t.makeThumbs = false
			}
			t.currentNum = n
		case playing := <-t.playing:
			if t.currentNum > 0 {
				if playing && !t.currentPlaying {
					t.makeThumbs = true
					t.updateNow <- struct{}{}
				}
				if !playing && t.currentPlaying {
					t.makeThumbs = false
				}
			}
			t.currentPlaying = playing
		case <-t.newVid:
			if t.currentNum > 0 {
				t.updateNow <- struct{}{}
			}
		}
	}
}
func (t *Thumb) updater() {
	var lastTime string
	makeThumb := func() {
		p("making thumb")
		when := fmt.Sprintf("%d", core.position.time)
		if when == lastTime {
			return
		} else {
			lastTime = when
		}
		cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-ss",
			when, "-i", core.current.path, "-s", "960x540", "-vframes", "1", "-y", thumbTmp)
		e := cmd.Run()
		chk(e)
		e = os.Rename(thumbTmp, thumbPath)
		chk(e)
		core.notify.pushUpdate("thumb", map[string]interface{}{
			"url": fmt.Sprintf("%sthumb.png?sec=%s", core.uri, when),
		})
	}

	for {
		if t.makeThumbs {
			select {
			case <-t.updateNow:
				makeThumb()
			case <-time.After(thumbInterval):
				makeThumb()
			}
		} else {
			<-t.updateNow
			makeThumb()
		}
	}
}
