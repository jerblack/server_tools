package main

import (
	_ "embed"
	"encoding/base64"
	"errors"
	delugeclient "github.com/jerblack/go-libdeluge"
	"github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	possibleConfs = []string{
		"/run/secrets/hydra.conf",
		"/etc/hydra.conf",
	}

	procFolder, preProcFolder, convertFolder, recycleFolder, torFolder, probFolder string

	torFileInterval = 30
	convertInterval = 60
	delugeInterval  = 60

	delugeDaemons map[string]*Deluge
	defaultDeluge *Deluge
	torFile       TorFile

	sabApi, sabIp, sabPort string
)

type DelugeTorrent struct {
	name, id, relPath string
	seedTime          int64
	ratio             float32
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
func (dt *DelugeTorrent) moveFiles() {
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
func (dt *DelugeTorrent) linkFiles() error {
	p("linking %d files from torrent %s", len(dt.files), dt.name)

	for _, src := range dt.files {
		dst := strings.Replace(src, dt.deluge.doneFolder, preProcFolder, 1)
		e := os.MkdirAll(filepath.Dir(dst), 0770)
		if e != nil {
			return e
		}
		if !fileExists(dst) {
			e = os.Link(src, dst)
			if e != nil {
				return e
			}
		}
	}
	return nil
}
func (dt *DelugeTorrent) moveStorage() error {
	p("move torrent to new storage location: %s -> %s", dt.name, dt.deluge.seedFolder)
	return dt.deluge.client.MoveStorage([]string{dt.id}, dt.deluge.seedFolder)
}

type Deluge struct {
	name, ip, user, pass                   string
	doneFolder, seedFolder, downloadFolder string
	trackers                               []string
	port                                   uint
	keepDone                               bool
	keepRatio                              float32
	keepTime                               int64
	client                                 *delugeclient.Client
	stuckDl                                map[string]int
	stuckSeeds                             map[string]int
	finished                               []string
}

func (d *Deluge) open() bool {
	p("opening connection to daemon %s", d.name)
	e := d.client.Connect()
	if e != nil {
		chk(e)
		return false
	}
	return true
}
func (d *Deluge) close() {
	e := d.client.Close()
	chk(e)
}
func (d *Deluge) verifyOpen() bool {
	_, e := d.client.DaemonVersion()
	if e != nil {
		chk(e)
		d.close()
		return d.open()
	}
	return true
}

func (d *Deluge) getFinished() []DelugeTorrent {
	var torrents []DelugeTorrent
	if !d.verifyOpen() {
		return torrents
	}
	tors, e := d.client.TorrentsStatus(delugeclient.StateUnspecified, nil)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chk(e)
	}
	for k, v := range tors {
		var dt DelugeTorrent
		dt.id = k
		dt.name = v.Name
		dt.seedTime = v.SeedingTime
		dt.ratio = v.Ratio
		dt.deluge = d
		//if v.SavePath == d.doneFolder {
		//	p(v.Name)
		//	p(v.SavePath)
		//	p("%t %t %t %t %t", v.IsSeed, v.IsFinished, v.State == "Seeding", v.Progress == 100, v.SavePath == d.doneFolder)
		//}

		if v.IsSeed && v.IsFinished && v.State == "Seeding" && v.Progress == 100 && v.SavePath == d.doneFolder {
			path := filepath.Join(d.doneFolder, v.Files[0].Path)
			_, e = os.Stat(path)
			if e == nil {
				var files []string
				for _, f := range v.Files {
					files = append(files, filepath.Join(d.doneFolder, f.Path))
				}
				dt.files = files

				_p := v.Files[0].Path
				if strings.Contains(_p, "/") {
					dt.relPath = strings.SplitN(_p, "/", 2)[0]
				}
				torrents = append(torrents, dt)
			}
		}
	}
	return torrents
}

func (d *Deluge) getClient() {
	d.client = delugeclient.NewV1(delugeclient.Settings{
		Port: d.port, Login: d.user, Password: d.pass, Hostname: d.ip,
	})
	d.stuckDl = make(map[string]int)
	d.stuckSeeds = make(map[string]int)
	d.open()

}
func (d *Deluge) checkStuckTorrents() {
	//if !d.open() {
	//	return
	//}
	//defer d.close()
	if !d.verifyOpen() {
		return
	}

	tors, e := d.client.TorrentsStatus(delugeclient.StateUnspecified, nil)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chk(e)
	}
	for k, v := range tors {
		if v.State == "Downloading" && v.Progress == 100 {
			n := d.stuckDl[k]
			n = n + 1
			d.stuckDl[k] = n
			if n >= 3 {
				delete(d.stuckDl, k)
				e = d.client.ForceRecheck(k)
				chk(e)
			}
		}
		if v.State == "Seeding" && v.Progress == 100 && v.SavePath == d.downloadFolder {
			n := d.stuckSeeds[k]
			n = n + 1
			d.stuckSeeds[k] = n
			if n >= 3 {
				delete(d.stuckSeeds, k)
				e = d.client.MoveStorage([]string{k}, d.doneFolder)
				chk(e)
			}
		}
	}
}
func (d *Deluge) removeFinishedTorrents() {
	//if !d.open() {
	//	return
	//}
	//defer d.close()
	if !d.verifyOpen() {
		return
	}
	torrents := d.getFinished()

	for _, dt := range torrents {
		p("torrent finished: %s", dt.name)
		e := dt.pause()
		if e != nil {
			p(e.Error())
			continue
		}
		_, e = dt.remove()
		chk(e)
		dt.moveFiles()
	}
	rmEmptyFolders(d.doneFolder)
}
func (d *Deluge) linkFinishedTorrents() {
	var fin []string
	//p("attempting linkFinishedTorrents")
	if !d.verifyOpen() {
		p("failed on verify open to deluge daemon %s", d.name)
		return
	}
	torrents := d.getFinished()
	if len(torrents) > 0 {
		p("found %d finished torrents", len(torrents))
	}
	for _, dt := range torrents {
		if isAny(d.name, d.finished...) {
			fin = append(fin, d.name)
			continue
		}

		p("torrent finished: %s", dt.name)
		e := dt.linkFiles()
		chkFatal(e)
		e = dt.moveStorage()
		chkFatal(e)
		fin = append(fin, d.name)
	}
	d.finished = fin

}

func (d *Deluge) checkFinishedTorrents() {
	if d.keepDone {
		d.linkFinishedTorrents()
	} else {
		d.removeFinishedTorrents()
	}

}
func (d *Deluge) addMagnet(magnetPath string) {
	//if !d.open() {
	//	return
	//}
	//defer d.close()
	if !d.verifyOpen() {
		return
	}
	p("adding magnet file to %s: %s", d.name, magnetPath)
	f, e := os.ReadFile(magnetPath)
	chkFatal(e)
	mag := string(f)
	hash, e := d.client.AddTorrentMagnet(mag, nil)
	chkFatal(e)
	p("add magnet file successful: %s", hash)
	rec := strings.Replace(magnetPath, torFolder, recycleFolder, 1)
	rec = getAltPath(rec)
	e = os.Rename(magnetPath, rec)
	chkFatal(e)
}
func (d *Deluge) addTorrent(torrentPath string) {
	//if !d.open() {
	//	return
	//}
	//defer d.close()
	if !d.verifyOpen() {
		return
	}
	p("adding torrent file to %s: %s", d.name, torrentPath)
	t, e := os.ReadFile(torrentPath)
	chkFatal(e)
	encoded := base64.StdEncoding.EncodeToString(t)
	fName := filepath.Base(torrentPath)
	hash, e := d.client.AddTorrentFile(fName, encoded, nil)
	chkFatal(e)
	p("add torrent file successful: %s", hash)
	rec := strings.Replace(torrentPath, torFolder, recycleFolder, 1)
	rec = getAltPath(rec)
	e = os.Rename(torrentPath, rec)
	chkFatal(e)
}

func parseConfig() {
	var confFile string
	for _, conf := range possibleConfs {
		b, e := os.ReadFile(conf)
		if e == nil {
			confFile = string(b)
			break
		}
	}
	if confFile == "" {
		p("no connected.conf file found in locations: %v", possibleConfs)
		os.Exit(1)
	}

	delugeDaemons = make(map[string]*Deluge)
	_d := Deluge{}
	deluge := &_d

	lines := strings.Split(confFile, "\n")
	reEq := regexp.MustCompile(`\s*=\s*`)
	reBrackets := regexp.MustCompile(`(^\[|]$)`)
	reTrue := regexp.MustCompile(`(?i)^(true|t|yes)$`)
	reSpaces := regexp.MustCompile(`\s+`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		r := strings.NewReplacer("\"", "", "'", "")
		line = r.Replace(line)
		line = reEq.ReplaceAllString(line, "=")

		if strings.HasPrefix(line, "[") {
			name := reBrackets.ReplaceAllString(line, "")
			d := Deluge{
				name: name,
			}
			delugeDaemons[name] = &d
			if defaultDeluge == nil {
				defaultDeluge = &d
			}
			deluge = &d
		} else {
			kv := strings.Split(line, "=")
			k, v := kv[0], kv[1]

			switch k {
			case "pre_proc_folder":
				preProcFolder = v
			case "proc_folder":
				procFolder = v
			case "convert_folder":
				convertFolder = v
			case "recycle_folder":
				recycleFolder = v
			case "torrent_folder":
				torFolder = v
			case "problem_folder":
				probFolder = v
			case "default":
				if reTrue.MatchString(v) {
					defaultDeluge = deluge
				}
			case "ip":
				deluge.ip = v
			case "port":
				i, e := strconv.Atoi(v)
				chkFatal(e)
				deluge.port = uint(i)
			case "user":
				deluge.user = v
			case "pass":
				deluge.pass = v
			case "keep_finished":
				if reTrue.MatchString(v) {
					deluge.keepDone = true
				}
			case "keep_ratio":
				ratio, e := strconv.ParseFloat(v, 32)
				chkFatal(e)
				deluge.keepRatio = float32(ratio)
			case "keep_days":
				days, e := strconv.ParseInt(v, 10, 64)
				chkFatal(e)
				// deluge reports seed_time in seconds
				deluge.keepTime = days * 24 * 60 * 60
			case "download_folder":
				deluge.downloadFolder = v
			case "finished_folder":
				deluge.doneFolder = v
			case "seed_folder":
				deluge.seedFolder = v
			case "trackers":
				v = reSpaces.ReplaceAllString(v, " ")
				for _, t := range strings.Split(v, " ") {
					deluge.trackers = append(deluge.trackers, t)
				}
			case "sab_ip":
				sabIp = v
			case "sab_port":
				sabPort = v
			case "sab_api":
				sabApi = v
			}
		}
	}
}
func getDelugeClients() {
	for _, d := range delugeDaemons {
		d.getClient()
	}
}

type TorFile struct{}

func (tf *TorFile) start() {
	p("torFile started and monitoring %s", torFolder)
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
	b, e := os.ReadFile(magnetPath)
	chkFatal(e)
	mag := string(b)
	for _, dd := range delugeDaemons {
		if containsString(mag, dd.trackers...) {
			dd.addMagnet(magnetPath)
			return
		}
	}
	defaultDeluge.addMagnet(magnetPath)

}
func (tf *TorFile) torrent(torrentPath string) {
	b, e := os.ReadFile(torrentPath)
	chkFatal(e)
	mag := string(b)
	for _, dd := range delugeDaemons {
		if containsString(mag, dd.trackers...) {
			dd.addTorrent(torrentPath)
			return
		}
	}
	defaultDeluge.addTorrent(torrentPath)
}

func extractPreProc() {
	if !isDirEmpty(preProcFolder) {
		p("running extract in %s", preProcFolder)
		err := run("/usr/bin/extract", preProcFolder)
		chk(err)
	}
}
func muxPreProc() {
	if !isDirEmpty(preProcFolder) {
		p("running mux in %s", preProcFolder)
		cmd := []string{"/usr/bin/mux", "-r", "-p", preProcFolder, "-mc", convertFolder}
		if probFolder != "" {
			cmd = append(cmd, "-prob", probFolder)
		}

		err := run(cmd...)
		chk(err)
	}
}
func muxConvert() {
	for {
		time.Sleep(time.Duration(convertInterval) * time.Second)
		if !isDirEmpty(convertFolder) {
			p("running mux in %s", convertFolder)
			cmd := []string{"/usr/bin/mux", "-r", "-p", convertFolder, "-mf", procFolder}
			if probFolder != "" {
				cmd = append(cmd, "-prob", probFolder)
			}
			err := run(cmd...)
			chk(err)
			rmEmptyFolders(convertFolder)
		}
	}
}
func pruneTorrents() {
	for {
		p("checking for torrents to prune")
		for _, d := range delugeDaemons {
			if d.keepDone {
				//if !d.open() {
				//	continue
				//}
				if !d.verifyOpen() {
					continue
				}
				torrents := d.getFinished()
				for _, t := range torrents {
					if t.seedTime > d.keepTime || t.ratio > d.keepRatio {
						p("torrent %s being renamed from %s with ratio %f and seed time of %d days", t.name, d.name, t.ratio, t.seedTime/60/60/24)
						_, e := t.remove()
						chkFatal(e)
					}
				}
				//d.close()
			}
		}
		time.Sleep(6 * time.Hour)
	}
}
func finishTorrents() {
	time.Sleep(time.Duration(3) * time.Second)
	for {
		for _, d := range delugeDaemons {
			d.checkFinishedTorrents()
			d.checkStuckTorrents()
		}
		if !isDirEmpty(preProcFolder) {
			extractPreProc()
			muxPreProc()
			mvTree(preProcFolder, procFolder, true)
		}
		time.Sleep(time.Duration(delugeInterval) * time.Second)
	}
}

func main() {
	parseConfig()
	getDelugeClients()
	go torFile.start()
	go muxConvert()
	go pruneTorrents()
	go finishTorrents()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
	)
	<-signalChan
	p("exiting. doing cleanup.")
	for _, d := range delugeDaemons {
		p("closing connection to daemon %s", d.name)
		d.close()
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
			err := os.MkdirAll(filepath.Dir(recycled), 0755)
			chkFatal(err)
			if isUpgrade(f, dstFile) {
				p("%s is an upgrade, replacing and recycling %s", f, dstFile)
				if fileExists(dstFile) && fileExists(filepath.Dir(recycled)) {
					renErr := os.Rename(dstFile, recycled)
					chkFatal(renErr)
				}
				if fileExists(f) {
					renErr := os.Rename(f, dstFile)
					chkFatal(renErr)
				}
			} else {
				p("recycling %s, it is not an upgrade for %s", f, dstFile)
				if fileExists(f) && fileExists(filepath.Dir(recycled)) {
					renErr := os.Rename(f, recycled)
					chkFatal(renErr)
				}
			}
		} else if errors.Is(err, os.ErrNotExist) {
			// file not exist
			p("moving new file to %s", dstFile)
			err := os.MkdirAll(filepath.Dir(dstFile), 0755)
			chkFatal(err)
			if fileExists(f) {
				renErr := os.Rename(f, dstFile)
				chkFatal(renErr)
			}
		} else {
			// problem checking if exists
			chkFatal(err)
		}
	}
	if removeEmpties {
		rmEmptyFolders(src)
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

var (
	p              = base.P
	chk            = base.Chk
	chkFatal       = base.ChkFatal
	containsString = base.ContainsString
	run            = base.Run
	rmEmptyFolders = base.RmEmptyFolders
	isDirEmpty     = base.IsDirEmpty
	getAltPath     = base.GetAltPath
	isAny          = base.IsAny
	fileExists     = base.FileExists
)
