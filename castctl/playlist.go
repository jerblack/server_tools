package main

import (
	"os"
	"path/filepath"
	"strings"
)

var (
	playlists      map[string]*Playlist
	playlistFolder = "/z/_cat_video/"
)

type Playlist struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// getPlaylists enumerates all subfolders in main folder and creates map of Playlist entries
// map key -> lower case subfolder name
// Folder -> path from perspective of server
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
			Id:     id,
			Folder: folder,
			Name:   strings.ReplaceAll(entry.Name(), "_", " "),
		}
		playlists[id] = &pl
	}
}
