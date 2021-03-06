package main

import (
	"bufio"
	"bytes"
	"fmt"
	. "github.com/jerblack/base"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var (
	downloadArchive = "/config/downloads.yt"
	rclonePath      = "google:go/src/server_tools/yt/"
	rcloneConfig    = "/config/rclone.conf"
	rclone          = "/usr/bin/rclone"
	config          = "/config/ytdl.conf"
	playlists       = map[string]string{
		"Paul Dinning":    "https://www.youtube.com/playlist?list=UUPJXfmxMYAoH02CFudZxmgg",
		"Handsome Nature": "https://www.youtube.com/playlist?list=UUJLIwYrmwgwbTzgmB5yVc7Q",
	}
	ytDl            = "/usr/bin/youtube-dl"
	numRecentVideos = 40
	ytTimeout       = 1 * time.Hour
	donePl, doneDl  chan struct{}
	vidIds          chan string
	archiveCache    []string
)

func run(bin string, args ...string) error {
	done := make(chan error, 1)
	cmd := exec.Command(bin, args...)
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
	case err := <-done:
		if err != nil {
			p("%s finished with error = %v", bin, err)
		} else {
			p("%s finished successfully", bin)
		}
		return err
	}
}
func runWithTimeout(bin string, args []string, timeout time.Duration) error {
	done := make(chan error, 1)
	cmd := exec.Command(bin, args...)
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
		return fmt.Errorf("%s timed out after %f seconds", bin, timeout.Seconds())
	case err := <-done:
		if err != nil {
			p("%s finished with error = %v", bin, err)
		} else {
			p("%s finished successfully", bin)
		}
		return err
	}
}

func updateTools() {
	e := run("sudo", ytDl, "-U")
	if e != nil {
		os.Exit(1)
	}
	e = run("sudo", rclone, "selfupdate")
	if e != nil {
		os.Exit(1)
	}
}

func getIdsFromPlaylist(pl string) {
	// https://www.yellowduck.be/posts/reading-command-output-line-by-line/
	cmd := exec.Command(ytDl, "--playlist-end", strconv.Itoa(numRecentVideos), "--get-id", pl)
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
			p("video already downloaded. skipping %s", id)
			continue
		}
		e := run(ytDl, "--config-location", config, fmt.Sprintf("https://www.youtube.com/watch?v=%s", id))
		Chk(e)
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
	updateTools()
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
)
