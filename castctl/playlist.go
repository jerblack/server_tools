package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	playlists      map[string]*Playlist
	playlistFolder = "/z/_cat_video/"
)

type Playlist struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	Folder       string `json:"folder"`
	PlayedFolder string `json:"played_folder"`
}

func (pl *Playlist) moveFile(file string) {
	src := filepath.Join(pl.Folder, file)
	dst := filepath.Join(pl.PlayedFolder, file)
	time.Sleep(3 * time.Second)
	e := os.Rename(src, dst)
	chk(e)
}

// getPlaylists enumerates all subfolders in main folder and creates map of Playlist entries
// map key -> lower case subfolder name
// Folder -> path from perspective of server
// PlayedFolder -> local path to played subfolder
// Name -> subfolder name with spaces replaced with underscores
func getPlaylists() {
	playlists = make(map[string]*Playlist)
	entries, e := os.ReadDir(playlistFolder)
	chkFatal(e)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		folder := filepath.Join(playlistFolder, entry.Name())
		id := strings.ToLower(entry.Name())
		pl := Playlist{
			Id:           id,
			Folder:       folder,
			PlayedFolder: filepath.Join(folder, ".played"),
			Name:         strings.ReplaceAll(entry.Name(), "_", " "),
		}
		playlists[id] = &pl
	}
}
