package main

import (
	"context"
	"github.com/vishen/go-chromecast/application"
	"github.com/vishen/go-chromecast/dns"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	uuid       = "93fe0c0a723e8381d61ca4de663fdaf6"
	device     = "Chromecast Ultra"
	deviceName = "Living Room Chromecast"
)

func NewCast() *Cast {
	var c Cast
	c.getApp()
	c.np = &NowPlaying{}
	c.stopChan = make(chan struct{}, 1)
	c.nextChan = make(chan struct{}, 1)
	return &c
}

type Cast struct {
	app      *application.Application
	np       *NowPlaying
	stopChan chan struct{}
	nextChan chan struct{}
	stopped  bool
}

func (c *Cast) getApp() {
	opts := []application.ApplicationOption{
		application.WithDebug(false),
		application.WithCacheDisabled(true),
	}
	app := application.NewApplication(opts...)
	e := app.Start(ccIp, ccPort)
	chkFatal(e)
	c.app = app
}

func (c *Cast) playPause() {
	if c.np.Paused {
		c.play()
	} else {
		c.pause()
	}
}
func (c *Cast) play() {
	e := c.app.Unpause()
	chk(e)
	c.np.startPolling(c.app)
}
func (c *Cast) pause() {
	e := c.app.Pause()
	chk(e)
}
func (c *Cast) stop() {
	c.stopped = true
	if c.stopChan != nil {
		c.stopChan <- struct{}{}
	}
	c.np.stopPolling()
	e := c.app.Stop()
	chk(e)
	c.np.empty()
	c.np.sendUpdate()

}
func (c *Cast) next() {
	// stop playback of current video, loop will automatically move to next
	e := c.app.StopMedia()
	chk(e)
	c.app.MediaFinished()
}
func (c *Cast) prev() {
	c.seek(0)

}
func (c *Cast) skipForward() {
	e := c.app.Skip()
	chk(e)
}
func (c *Cast) seekPercent(pct float64) {
	if c.np.Duration == 0 {
		return
	}
	position := pct / 100 * float64(c.np.Duration)
	e := c.app.SeekToTime(float32(position))
	chk(e)
}
func (c *Cast) seek(sec float64) {
	e := c.app.SeekToTime(float32(sec))
	chk(e)
}
func (c *Cast) setVolume(v float64) {
	// range from 0 to 1
	e := c.app.SetVolume(float32(v))
	chk(e)
}
func (c *Cast) mute(m bool) {
	e := c.app.SetMuted(m)
	chk(e)
}
func (c *Cast) playPlaylist0(pl *Playlist) {
	if pl != nil {
		p("playing playlist: %s", pl.Name)
		files, e := os.ReadDir(pl.Folder)
		if e != nil {
			chk(e)
			return
		}
		reMp4 := regexp.MustCompile(`(?i).*\.mp4$`)
		var media []string
		for _, file := range files {
			if !file.IsDir() && reMp4.MatchString(file.Name()) {
				media = append(media, file.Name())
			}
		}
		c.np.Playlist = pl.Name
		go func() {
			c.np.stopPolling()
			c.np.startPolling(c.app)
		}()
		defer c.np.stopPolling()

		p("playing %d files in folder %s", len(media), pl.Folder)
		playChan := make(chan string, 1)
		sendNext := make(chan struct{}, 1)

		go func() {
			for i, file := range media {
				p("[%d/%d] now playing -> %s", i+1, len(media), file)
				playChan <- file
				select {
				case <-sendNext:
				case <-c.nextChan:
				}

			}
		}()

		for {
			select {
			case file := <-playChan:
				t := parseTitle(file)
				c.np.Date = t.Date
				c.np.Id = t.Id
				c.np.Title = t.Title
				c.np.filename = filepath.Join(pl.Folder, file)
				go func() {
					e := c.app.Load(c.np.filename,
						"video/mp4", false, false, false)
					chk(e)
					sendNext <- struct{}{}
				}()
			case <-c.stopChan:
				return
			}
		}

	}
}
func (c *Cast) playPlaylist(pl *Playlist) {
	if pl != nil {
		c.stopped = false
		p("playing playlist: %s", pl.Name)
		files, e := os.ReadDir(pl.Folder)
		if e != nil {
			chk(e)
			return
		}
		watched := db.getWatched(pl)
		reMp4 := regexp.MustCompile(`(?i).*\.mp4$`)
		var media []string
		for _, file := range files {
			if !file.IsDir() && reMp4.MatchString(file.Name()) {
				if !hasString(watched, file.Name()) {
					media = append(media, file.Name())
				}
			}
		}
		if len(media) == 0 {
			db.clearWatched(pl)
			c.playPlaylist(pl)
			return
		}

		c.np.Playlist = pl.Name
		go func() {
			c.np.stopPolling()
			c.np.startPolling(c.app)
		}()
		defer c.np.stopPolling()

		p("playing %d files in folder %s", len(media), pl.Folder)
		for i, file := range media {
			p("[%d/%d] now playing -> %s", i+1, len(media), file)
			t := parseTitle(file)
			c.np.Date = t.Date
			c.np.Id = t.Id
			c.np.Title = t.Title
			c.np.filename = filepath.Join(pl.Folder, file)
			e := c.app.Load(c.np.filename,
				"video/mp4", false, false, false)
			chk(e)
			if c.stopped {
				return
			} else {
				go db.addWatched(pl, file)
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func hasString(arr []string, s string) bool {
	for _, a := range arr {
		if s == a {
			return true
		}
	}
	return false
}

func findAll() {
	dnsTimeoutSeconds := 5
	var iface *net.Interface
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(dnsTimeoutSeconds))
	defer cancel()
	castEntryChan, err := dns.DiscoverCastDNSEntries(ctx, iface)
	if err != nil {
		p("unable to discover chromecast devices")
		p(err.Error())
		return
	}
	i := 1
	for d := range castEntryChan {
		p("device=%q device_name=%q address=\"%s:%d\" uuid=%q", i, d.Device, d.DeviceName, d.AddrV4, d.Port, d.UUID)
		i++
	}
	if i == 1 {
		p("no cast devices found on network\n")
	}
	return
}
