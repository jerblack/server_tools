package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	delugeclient "github.com/gdm85/go-libdeluge"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	procFolder    = "/x/_proc"
	preProcFolder = "/x/_pre_proc"

	convertFolder = "/x/_convert"
	recycleFolder = "/x/.config/_recycle"

	torFolder      = "/x/.config/_tor_new"
	privTorFolder  = "/x/.config/_tor_new_priv"
	pubTorFolder   = "/x/.config/_tor_new_pub"
	privDoneFolder = "/x/_tor_done_priv"
	pubDoneFolder  = "/x/_tor_done_pub"
	seedMarker     = ".grabbed"

	torFileInterval = 30
	convertInterval = 60
	delugeInterval  = 60
	privTrackers    = []string{
		"stackoverflow.tech",
		"bgp.technology",
		"empirehost.me",
		"torrentleech.org",
		"tleechreload.org",
	}
	pubDeluge, privDeluge *Deluge
	user, pw              string
	//go:embed deluge
	delugeFile string
	torFile    TorFile
	proc       Proc
)

func main() {
	p("starting hydra")
	getDelugeAuth()
	getDelugeClients()
	go torFile.start()
	go proc.muxConvert()

	for {
		time.Sleep(time.Duration(delugeInterval) * time.Second)
		found1 := pubDeluge.start()
		found2 := privDeluge.start()
		if found1 || found2 {
			proc.extractPreProc()
			proc.muxPreProc()
			mvTree(preProcFolder, procFolder, true)
		}
	}
}

func getDelugeAuth() {
	delugeFile = strings.TrimSpace(delugeFile)
	authArr := strings.Split(delugeFile, "\n")
	user = strings.TrimSpace(authArr[0])
	pw = strings.TrimSpace(authArr[1])
}

type DelugeTorrent struct {
	name, id, relPath string
	files             []string
	deluge            *Deluge
}

func (dt *DelugeTorrent) pause() error {
	p("pausing torrent %s", dt.name)
	return dt.deluge.client.PauseTorrents(dt.id)
}
func (dt *DelugeTorrent) remove() (bool, error) {
	p("removing torrent %s", dt.name)
	return dt.deluge.client.RemoveTorrent(dt.id, false)
}
func (dt *DelugeTorrent) move() {
	p("moving %d files from torrent %s", len(dt.files), dt.name)
	if dt.relPath == "" {
		for _, f := range dt.files {
			dst := strings.Replace(f, dt.deluge.doneFolder, preProcFolder, 1)
			e := os.Rename(f, dst)
			chk(e)
		}
	} else {
		src := filepath.Join(dt.deluge.doneFolder, dt.relPath)
		dst := filepath.Join(preProcFolder, dt.relPath)
		mvTree(src, dst, true)
	}
}

type Deluge struct {
	client                     *delugeclient.Client
	torFiles, doneFolder, kind string
	deleteDone                 bool
	torrents                   []DelugeTorrent
}

func (d *Deluge) start() bool {
	p("start %s torrent check", d.kind)
	if d.deleteDone {
		d.getTorrents()
		for _, dt := range d.torrents {
			p("torrent finished: %s", dt.name)
			e := dt.pause()
			if e != nil {
				p(e.Error())
				continue
			}
			_, e = dt.remove()
			chk(e)
			dt.move()
		}
	} else {
		d.linkSeeds()
	}
	return len(d.torrents) > 0
}
func (d *Deluge) getTorrents() {
	d.torrents = []DelugeTorrent{}
	tors, e := d.client.TorrentsStatus(delugeclient.StateUnspecified, nil)
	chk(e)
	for k, v := range tors {
		var dt DelugeTorrent
		dt.id = k
		dt.name = v.Name
		dt.deluge = d
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
				dt.files = files

				_p := v.Files[0].Path
				if strings.Contains(_p, "/") {
					dt.relPath = strings.SplitN(_p, "/", 2)[0]
				}
			}
			d.torrents = append(d.torrents, dt)
		}
	}
}
func (d *Deluge) linkSeeds() {
	allFiles := make(map[string][]string)
	allFolders := make(map[string][]string)

	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		path, _ := filepath.Split(p)
		if info.IsDir() {
			allFiles[p+"/"] = make([]string, 0)
			allFolders[p+"/"] = make([]string, 0)
			allFolders[path] = append(allFolders[path], p)
		} else {
			allFiles[path] = append(allFiles[path], p)
		}
		return nil
	}
	err := filepath.Walk(d.doneFolder, walk)
	chkFatal(err)

	for k, v := range allFiles {
		if len(v) == 0 && len(allFolders[k]) == 0 {
			// check for and delete empty folder
			if k != d.doneFolder+"/" {
				p("deleting empty folder: %s\n", k)
				err = os.Remove(k)
				chkFatal(err)
			}
		} else {
			for _, f := range v {
				if strings.HasSuffix(f, seedMarker) {
					// delete marker with no marked file
					markedFile := strings.TrimSuffix(f, seedMarker)
					if !hasString(allFiles[k], markedFile) {
						p("deleting orphan marker: %s\n", f)
						err = os.Remove(f)
						chkFatal(err)
					}
				} else {
					// link file with no marker, create marker
					marker := f + seedMarker
					if !hasString(allFiles[k], marker) {
						relPath := strings.TrimPrefix(f, d.doneFolder)
						preProcPath := filepath.Join(preProcFolder, relPath)
						preProcRel, _ := filepath.Split(preProcPath)
						p("linking new seed to pre-proc folder: %s\n", f)
						err = os.MkdirAll(preProcRel, 0777)
						chkFatal(err)
						err = os.Link(f, preProcPath)
						chkFatal(err)
						m, err := os.Create(marker)
						chkFatal(err)
						err = m.Close()
						chkFatal(err)
					}
				}
			}
		}
	}
}

func getDelugeClients() {
	pubDeluge = &Deluge{
		client: delugeclient.NewV1(delugeclient.Settings{
			Port: 5051, Login: user, Password: pw,
		}),
		torFiles:   pubTorFolder,
		doneFolder: pubDoneFolder,
		deleteDone: true,
		kind:       "public",
	}
	privDeluge = &Deluge{
		client: delugeclient.NewV1(delugeclient.Settings{
			Port: 5050, Login: user, Password: pw,
		}),
		torFiles:   privTorFolder,
		doneFolder: privDoneFolder,
		deleteDone: true,
		kind:       "private",
	}
}

type TorFile struct{}

func (tf *TorFile) start() {
	p("torFile started and monitoring %s", torFolder)
	p("private torrent path: %s", privTorFolder)
	p("public torrent path: %s", pubTorFolder)
	for {
		tf.getFiles()
		time.Sleep(time.Duration(torFileInterval) * time.Second)
	}
}
func (tf *TorFile) getFiles() {
	var magnets, torrents []string
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if strings.HasSuffix(p, ".torrent") {
				torrents = append(torrents, p)
			}
			if strings.HasSuffix(p, ".magnet") {
				magnets = append(magnets, p)
			}
		}
		return nil
	}
	err := filepath.Walk(torFolder, walk)
	chkFatal(err)
	for _, magnet := range magnets {
		tf.magnet(magnet)
	}
	for _, torrent := range torrents {
		tf.torrent(torrent)
	}
}
func (tf *TorFile) magnet(magnetPath string) {
	e := pubDeluge.client.Connect()
	chk(e)
	defer pubDeluge.client.Close()
	p("adding magnet file: %s", magnetPath)
	f, e := os.ReadFile(magnetPath)
	chkFatal(e)
	mag := string(f)
	hash, e := pubDeluge.client.AddTorrentMagnet(mag, nil)
	chk(e)
	if e != nil {
		p("failed to add magnet file: %s")
	} else {
		p("add magnet file successful: %s", hash)
		rec := strings.Replace(magnetPath, torFolder, recycleFolder, 1)
		rec = getAltPath(rec)
		e = os.Rename(magnetPath, rec)
		chkFatal(e)
	}

}
func (tf *TorFile) torrent(torrentPath string) {
	p("adding torrent file: %s", torrentPath)
	file, err := os.Open(torrentPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	srcFolder := pubTorFolder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, priv := range privTrackers {
			if strings.Contains(line, priv) {
				srcFolder = privTorFolder
			}
		}
	}
	tor := strings.Replace(torrentPath, torFolder, srcFolder, 1)
	tor = getAltPath(tor)
	e := os.Rename(torrentPath, tor)
	chkFatal(e)
	p("moved to %s", tor)
}

type Proc struct{}

func (pr *Proc) extractPreProc() {
	if ok, e := isDirEmpty(preProcFolder); e == nil && !ok {
		p("running extract in %s", preProcFolder)
		err := run("/usr/bin/extract", preProcFolder)
		chk(err)
	}
}
func (pr *Proc) muxPreProc() {
	if ok, e := isDirEmpty(preProcFolder); e == nil && !ok {
		p("running mux in %s", preProcFolder)
		err := run("/usr/bin/mux", "-r", "-p", preProcFolder, "-mc", convertFolder)
		chk(err)
	}
}
func (pr *Proc) muxConvert() {
	for {
		time.Sleep(time.Duration(convertInterval) * time.Second)
		if ok, e := isDirEmpty(convertFolder); e == nil && !ok {
			p("running mux in %s", convertFolder)
			err := run("/usr/bin/mux", "-r", "-p", convertFolder, "-mf", procFolder)
			chk(err)
		}
	}

}

func mvTree(src, dst string, removeEmpties bool) {
	p("moving tree %s to %s", src, dst)
	var files []string
	var folders []string
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(p, "_UNPACK_") || strings.Contains(p, "_FAILED_") {
			return nil
		}
		if info.IsDir() {
			folders = append(folders, p)
		} else {
			files = append(files, p)
		}
		return nil
	}
	err := filepath.Walk(src, walk)
	chkFatal(err)

	for _, f := range folders {
		newFolder := strings.Replace(f, src, dst, 1)
		err := os.MkdirAll(newFolder, 0777)
		chkFatal(err)
	}
	for _, f := range files {
		dstFile := strings.Replace(f, src, dst, 1)
		if _, err := os.Stat(dstFile); err == nil {
			// file exists
			recycled := strings.Replace(f, src, recycleFolder, 1)
			err := os.MkdirAll(filepath.Dir(recycled), 0777)
			chkFatal(err)
			if isUpgrade(f, dstFile) {
				p("%s is an upgrade, replacing and recycling %s", f, dstFile)
				renErr := os.Rename(dstFile, recycled)
				chkFatal(renErr)
				renErr = os.Rename(f, dstFile)
				chkFatal(renErr)
			} else {
				p("recycling %s, it is not an upgrade for %s", f, dstFile)
				renErr := os.Rename(f, recycled)
				chkFatal(renErr)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			// file not exist
			p("moving new file to %s", dstFile)
			renErr := os.Rename(f, dstFile)
			chkFatal(renErr)
		} else {
			// problem checking if exists
			chkFatal(err)
		}
	}
	if removeEmpties {
		fn := func(i, j int) bool {
			// reverse sort
			return len(folders[j]) < len(folders[i])
		}
		sort.Slice(folders, fn)
		for _, f := range folders {
			if filepath.Clean(f) == filepath.Clean(src) {
				continue
			}
			if empty, err := isDirEmpty(f); err == nil && empty {
				e := os.Remove(f)
				chk(e)
			}
		}
	}
}
func isUpgrade(new, old string) bool {
	if strings.HasSuffix(strings.ToLower(new), ".mkv") {
		args := []string{"-verror", "-select_streams", "v:0", "show_entries", "stream=width", "-of", "csv=p=0"}
		newOut, e0 := exec.Command("/usr/bin/ffprobe", append(args, new)...).Output()
		newRes, e1 := strconv.Atoi(string(newOut))
		oldOut, e2 := exec.Command("/usr/bin/ffprobe", append(args, old)...).Output()
		oldRes, e3 := strconv.Atoi(string(oldOut))
		if e0 == nil && e1 == nil && e2 == nil && e3 == nil {
			if newRes > oldRes {
				return true
			}
			if oldRes > newRes {
				return false
			}
		}
	}

	newStat, e4 := os.Stat(new)
	oldStat, e5 := os.Stat(old)
	if e4 == nil && e5 != nil {
		return true
	}
	if e5 == nil && e4 != nil {
		return false
	}
	if e4 != nil && e5 != nil {
		return false
	}
	if newStat != nil && oldStat != nil {
		return newStat.Size() > oldStat.Size()
	}
	return false
}
func isDirEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// read in ONLY one file
	_, err = f.Readdir(1)

	// if file is EOF the dir is empty.
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
func getAltPath(path string) string {
	i := 1
	newPath := path
	for {
		_, e := os.Stat(newPath)
		if errors.Is(e, os.ErrNotExist) {
			return newPath
		}
		newPath = fmt.Sprintf("%s.%d", path, i)
		i += 1
	}

}

func p(s string, i ...interface{}) {
	now := time.Now()
	t := strings.ToLower(strings.TrimRight(now.Format("3.04PM"), "M"))
	notice := fmt.Sprintf("%s | %s", t, fmt.Sprintf(s, i...))
	fmt.Println(notice)
}
func chkFatal(err error) {
	if err != nil {
		fmt.Println("----------------------")
		panic(err)
	}
}
func chk(err error) {
	if err != nil {
		fmt.Println("----------------------")
		fmt.Println(err)
		fmt.Println("----------------------")
	}
}
func hasString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)

	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &stdBuffer)

	cmd.Stdout = mw
	cmd.Stderr = mw

	return cmd.Run()

}
