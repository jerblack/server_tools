package main

import (
	"bufio"
	"bytes"
	. "github.com/jerblack/server_tools/base"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	downloadArchive = "/x/.config/yt/downloads.yt"
	rclonePath      = "google:go/src/server_tools/yt/"
	rcloneConfig    = "/x/.config/yt/rclone.conf"
	rclone          = "rclone"
	config          = "/x/.config/yt/ytdl.conf"
	playlists       = map[string]string{
		"Paul Dinning":    "https://www.youtube.com/playlist?list=UUPJXfmxMYAoH02CFudZxmgg",
		"Handsome Nature": "https://www.youtube.com/playlist?list=UUJLIwYrmwgwbTzgmB5yVc7Q",
	}
	ytDl           = "youtube-dl"
	donePl, doneDl chan struct{}
	vidIds         chan string
	timeout        = 1 * time.Hour
	archiveCache   []string
)

func getIdsFromPlaylist(pl string) {
	// https://www.yellowduck.be/posts/reading-command-output-line-by-line/
	cmd := exec.Command(ytDl, "--get-id", pl)
	r, _ := cmd.StdoutPipe()
	done := make(chan struct{})
	scanner := bufio.NewScanner(r)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			vidIds <- strings.Trim(line, "\n\r")
		}
		done <- struct{}{}
	}()
	err := cmd.Start()
	ChkFatal(err)
	<-done
	err = cmd.Wait()
	Chk(err)
	donePl <- struct{}{}
}

func downloadVids() {
	//https://stackoverflow.com/a/11886829/2934704

	inCache := func(id string) bool {
		for _, a := range archiveCache {
			if id == a {
				return true
			}
		}
		return false
	}

	for id := range vidIds {
		p("downloadVids received id: %s", id)
		if inCache(id) {
			p("video with id % was already downloaded. skipping", id)
			continue
		}
		done := make(chan error, 1)

		cmd := exec.Command(ytDl, "--config-location", config, id)
		var stdBuffer bytes.Buffer
		mw := io.MultiWriter(os.Stdout, &stdBuffer)
		cmd.Stdout = mw
		cmd.Stderr = mw
		e := cmd.Start()
		ChkFatal(e)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case <-time.After(timeout):
			e := cmd.Process.Kill()
			ChkFatal(e)
			p("youtube-dl dl of %s timed out. skipping", id)
		case err := <-done:
			if err != nil {
				p("youtube-dl dl of %s finished with error = %v", id, err)
			} else {
				p("youtube-dl dl of %s finished successfully", id)
			}
		}
	}
	doneDl <- struct{}{}
}

func cacheDlArchive() {
	b, e := os.ReadFile(downloadArchive)
	if e != nil {
		p("error during cache of download archive: %s", e.Error())
		return
	}

	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimPrefix(line, "youtube ")
		line = strings.Trim(line, "\n\r")
		archiveCache = append(archiveCache, line)
	}
}

func main() {
	cacheDlArchive()
	donePl = make(chan struct{}, 1)
	doneDl = make(chan struct{}, 1)
	vidIds = make(chan string, 4096)

	for title, playlist := range playlists {
		p("downloading new videos in '%s' playlist", title)
		go getIdsFromPlaylist(playlist)

	}
	go downloadVids()

	for i := 0; i < len(playlists); i++ {
		<-donePl
	}
	close(vidIds)
	<-doneDl
	p("backing up download archive to google drive")
	e := run(rclone, "--config", rcloneConfig, "copy", downloadArchive, rclonePath)
	chk(e)
}

var (
	p   = P
	chk = Chk
	run = Run
)
