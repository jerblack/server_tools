package main

import (
	"encoding/base64"
	"fmt"
	delugeclient "github.com/gdm85/go-libdeluge"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	user          = "jeremy"
	pw            = "random"
	host          = "192.168.0.99"
	port          = 5050
	pubDoneFolder = "/x/_tor_done_pub"
	torrent       = "Nobody.2021.2160p.AMZN.WEB-DL.DDP5.1.HDR.HEVC-EVO [IPT].torrent"
)

func getDelugeClient() *delugeclient.Client {
	//var logger *log.Logger
	settings := delugeclient.Settings{
		Hostname: host,
		Port:     uint(port),
		Login:    user,
		Password: pw,
	}
	return delugeclient.NewV1(settings)
}

type DelugeTorrent struct {
	name, id string
	files    []string
}

//deluge-console "connect 127.0.0.1:5051; info -v;"

func getTorrents(dc *delugeclient.Client) *[]DelugeTorrent {
	var torrents []DelugeTorrent
	tors, e := dc.TorrentsStatus(delugeclient.StateUnspecified, nil)
	chk(e)
	for k, v := range tors {
		var d DelugeTorrent
		d.id = k
		d.name = v.Name
		//p(v.Name)
		//p("%v %v %v %v %v %v", v.IsSeed, v.IsFinished, v.State == "Seeding", v.Progress == 100, v.State, v.Progress)
		if v.IsSeed && v.IsFinished && v.State == "Seeding" && v.Progress == 100 {
			path := filepath.Join(pubDoneFolder, v.Files[0].Path)
			//p(path)
			_, e = os.Stat(path)
			if e == nil {
				var files []string
				for _, f := range v.Files {
					files = append(files, filepath.Join(pubDoneFolder, f.Path))
				}
				d.files = files
			}
			torrents = append(torrents, d)
		}
	}
	return &torrents
}

func addTorrent(dc *delugeclient.Client) {
	t, e := os.ReadFile(torrent)
	chkFatal(e)
	encoded := base64.StdEncoding.EncodeToString(t)
	hash, e := dc.AddTorrentFile(torrent, encoded, nil)
	chkFatal(e)
	p(hash)

}

func main() {
	deluge := getDelugeClient()
	deluge.Connect()
	defer deluge.Close()
	addTorrent(deluge)
	//t := getTorrents(deluge)
	//for _, _t := range *t {
	//	fmt.Printf("%+v\n", _t)
	//}

}
func p(s string, i ...interface{}) {
	now := time.Now()
	t := strings.ToLower(strings.TrimRight(now.Format("3.04PM"), "M"))
	notice := fmt.Sprintf("%s | %s", t, fmt.Sprintf(s, i...))
	fmt.Println(notice)
}
func chk(err error) {
	if err != nil {
		fmt.Println("----------------------")
		fmt.Println(err)
		fmt.Println("----------------------")
	}
}
func chkFatal(err error) {
	if err != nil {
		fmt.Println("----------------------")
		panic(err)
	}
}
