package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	delugeclient "github.com/jerblack/go-libdeluge"
	"github.com/jerblack/server_tools/base"
	base_db "github.com/jerblack/server_tools/base.db"
	"io"
	"net/http"
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
	conf     = "/x/.config/hydra/hydra.conf"
	dbFile   = "/x/.config/hydra/hydra.db"
	keepFile = "/x/.config/hydra/hydra_keep"

	procFolder, preProcFolder, convertFolder string
	recycleFolder, torFolder, probFolder     string
	protectedFolders                         []string

	torFileInterval      = 30 * time.Second
	convertInterval      = 60 * time.Second
	convertStartDelay    = 2 * time.Minute
	finishStartDelay     = 3 * time.Second
	finishInterval       = 60 * time.Second
	staleStartDelay      = 10 * time.Second
	staleInterval        = 10 * time.Minute
	pruneInterval        = 1 * time.Hour
	pruneStartDelay      = 20 * time.Second
	errorCheckStartDelay = 30 * time.Second
	errorCheckInterval   = 30 * time.Minute

	delugeDaemons map[string]*Deluge
	defaultDeluge *Deluge
	torFile       TorFile
	staleTorrent  StaleTorrent

	sabKey, sabIp, sabPort          string
	sonarrKey, sonarrIp, sonarrPort string
	staleAge                        time.Duration
)

type DelugeCommand struct {
	fn           string
	id           string
	ids          []string
	target       string
	tf           bool
	torrentState delugeclient.TorrentState
}
type DelugeResponse struct {
	err      error
	success  bool
	torrents []DelugeTorrent
	torrent  DelugeTorrent
	hash     string
}
type Deluge struct {
	daemon   *delugeclient.ClientV2
	cmd      chan DelugeCommand
	response chan DelugeResponse

	name, ip, user, pass                   string
	doneFolder, seedFolder, downloadFolder string
	trackers                               []string
	port                                   uint
	keepDone                               bool
	keepRatio                              float32
	keepTime                               time.Duration
	stuckDl                                map[string]int
	stuckSeeds                             map[string]int
	stuckPaused                            map[string]int
	finished                               []string
}

func (d *Deluge) start() {
	d.daemon = delugeclient.NewV2(delugeclient.Settings{
		Port: d.port, Login: d.user, Password: d.pass, Hostname: d.ip,
	})
	d.stuckDl = make(map[string]int)
	d.stuckSeeds = make(map[string]int)
	d.cmd = make(chan DelugeCommand, 1)
	d.response = make(chan DelugeResponse, 1)
	//p("deluge daemon %s is marked as keep_finished: %t", d.name, d.keepDone)
	go d.handler()
	d.open()

}
func (d *Deluge) handler() {
	for cmd := range d.cmd {
		var failedVerify bool
		for d.verifyOpen() == false {
			p("deluge daemon %s not available. retrying in 30 seconds", d.name)
			failedVerify = true
			time.Sleep(30 * time.Second)
			d.open()
		}
		if failedVerify {
			p("successfully reconnected to daemon %s", d.name)
		}

		switch cmd.fn {
		case "PauseTorrents":
			e := d.daemon.PauseTorrents(cmd.ids...)
			d.response <- DelugeResponse{
				err: e,
			}
		case "ResumeTorrents":
			e := d.daemon.ResumeTorrents(cmd.ids...)
			d.response <- DelugeResponse{
				err: e,
			}
		case "RemoveTorrent":
			success, e := d.daemon.RemoveTorrent(cmd.id, cmd.tf)
			d.response <- DelugeResponse{
				err:     e,
				success: success,
			}
		case "MoveStorage":
			e := d.daemon.MoveStorage(cmd.ids, cmd.target)
			d.response <- DelugeResponse{
				err: e,
			}
		case "TorrentsStatus":
			t, e := d.daemon.TorrentsStatus(cmd.torrentState, nil)
			d.response <- DelugeResponse{
				err:      e,
				torrents: d.parseTorrents(t),
			}
		case "TorrentStatus":
			t, e := d.daemon.TorrentStatus(cmd.id)
			d.response <- DelugeResponse{
				err:     e,
				torrent: d.parseTorrent(cmd.id, t),
			}
		case "ForceRecheck":
			e := d.daemon.ForceRecheck(cmd.ids...)
			d.response <- DelugeResponse{
				err: e,
			}
		case "AddTorrentMagnet":
			f, e := os.ReadFile(cmd.id)
			chkFatal(e)
			mag := string(f)
			hash, e := d.daemon.AddTorrentMagnet(mag, nil)
			d.response <- DelugeResponse{
				err:  e,
				hash: hash,
			}
		case "AddTorrentFile":
			t, e := os.ReadFile(cmd.id)
			chkFatal(e)
			encoded := base64.StdEncoding.EncodeToString(t)
			fName := filepath.Base(cmd.id)
			hash, e := d.daemon.AddTorrentFile(fName, encoded, nil)
			d.response <- DelugeResponse{
				err:  e,
				hash: hash,
			}
		}
	}
}
func (d *Deluge) parseTorrent(id string, t *delugeclient.TorrentStatus) DelugeTorrent {
	var dt DelugeTorrent
	dt.id = id
	dt.name = t.Name
	dt.timeSeeded = time.Duration(t.SeedingTime) * time.Second
	dt.timeAdded = time.Unix(int64(t.TimeAdded), 0)
	dt.timeActive = time.Duration(t.ActiveTime) * time.Second
	dt.timeCompleted = time.Duration(t.CompletedTime) * time.Second
	dt.ratio = t.Ratio
	dt.deluge = d
	dt.state = t.State
	dt.savePath = t.SavePath
	dt.isFinished = t.IsFinished
	dt.isSeed = t.IsSeed
	dt.progress = t.Progress
	var files []string
	for _, f := range t.Files {
		files = append(files, filepath.Join(t.SavePath, f.Path))
	}
	dt.files = files

	if len(t.Files) > 0 {
		path := t.Files[0].Path
		if strings.Contains(path, "/") {
			dt.relPath = strings.SplitN(path, "/", 2)[0]
		}
	}
	return dt
}
func (d *Deluge) parseTorrents(torrents map[string]*delugeclient.TorrentStatus) []DelugeTorrent {
	var tor []DelugeTorrent
	for k, v := range torrents {
		dt := d.parseTorrent(k, v)
		tor = append(tor, dt)
	}
	return tor
}
func (d *Deluge) open() bool {
	defer func() {
		_ = recover()
	}()
	p("opening connection to daemon %s", d.name)
	e := d.daemon.Connect()
	if e != nil {
		chk(e)
		return false
	}
	return true
}
func (d *Deluge) close() {
	e := d.daemon.Close()
	chk(e)
}
func (d *Deluge) verifyOpen() (open bool) {
	defer func() {
		r := recover()
		if r != nil {
			open = false
		}

	}()
	_, e := d.daemon.DaemonVersion()
	if e != nil {
		chk(e)
		//d.close()
		return d.open()
	}
	return true
}
func (d *Deluge) PauseTorrents(ids ...string) error {
	d.cmd <- DelugeCommand{
		fn:  "PauseTorrents",
		ids: ids,
	}
	rsp := <-d.response
	return rsp.err
}
func (d *Deluge) ResumeTorrents(ids ...string) error {
	d.cmd <- DelugeCommand{
		fn:  "ResumeTorrents",
		ids: ids,
	}
	rsp := <-d.response
	return rsp.err
}
func (d *Deluge) ForceRecheck(ids ...string) error {
	d.cmd <- DelugeCommand{
		fn:  "ForceRecheck",
		ids: ids,
	}
	rsp := <-d.response
	return rsp.err
}
func (d *Deluge) RemoveTorrent(id string, rmFile bool) error {
	d.cmd <- DelugeCommand{
		fn: "RemoveTorrent",
		id: id,
		tf: rmFile,
	}
	rsp := <-d.response
	return rsp.err
}
func (d *Deluge) AddTorrentMagnet(magnetPath string) {
	p("adding magnet file to %s: %s", d.name, magnetPath)
	d.cmd <- DelugeCommand{
		fn: "AddTorrentMagnet",
		id: magnetPath,
	}
	rsp := <-d.response
	if rsp.err != nil {
		p("add magnet file failed: %s", rsp.err.Error())
	} else {
		p("add %s magnet file successful: %s", d.name, rsp.hash)
		rec := strings.Replace(magnetPath, torFolder, recycleFolder, 1)
		rec = getAltPath(rec)
		e := verifyFolder(filepath.Dir(rec))
		chkFatal(e)
		e = os.Rename(magnetPath, rec)
		chkFatal(e)
	}
}
func (d *Deluge) AddTorrentFile(torrentPath string) {
	p("adding torrent file to %s: %s", d.name, torrentPath)
	d.cmd <- DelugeCommand{
		fn: "AddTorrentFile",
		id: torrentPath,
	}
	rsp := <-d.response
	if rsp.err != nil {
		p("add %s torrent file %s failed: %s", d.name, torrentPath, rsp.err.Error())
	} else {
		p("add %s torrent file successful: %s", d.name, rsp.hash)
		rec := strings.Replace(torrentPath, torFolder, recycleFolder, 1)
		rec = getAltPath(rec)
		e := verifyFolder(filepath.Dir(rec))
		chkFatal(e)
		e = os.Rename(torrentPath, rec)
		chkFatal(e)
	}
}
func (d *Deluge) MoveStorage(ids []string, dst string) error {
	d.cmd <- DelugeCommand{
		fn:     "MoveStorage",
		ids:    ids,
		target: dst,
	}
	rsp := <-d.response
	return rsp.err
}
func (d *Deluge) TorrentStatus(id string) (DelugeTorrent, error) {
	d.cmd <- DelugeCommand{
		fn: "TorrentStatus",
		id: id,
	}
	rsp := <-d.response
	return rsp.torrent, rsp.err
}
func (d *Deluge) TorrentsStatus(state delugeclient.TorrentState) ([]DelugeTorrent, error) {
	d.cmd <- DelugeCommand{
		fn:           "TorrentsStatus",
		torrentState: state,
	}
	rsp := <-d.response
	return rsp.torrents, rsp.err
}
func (d *Deluge) getTorrents() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StateUnspecified)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	return torrents
}
func (d *Deluge) getPaused() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StatePaused)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	return torrents
}
func (d *Deluge) getFinished() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StateSeeding)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	var fin []DelugeTorrent
	for _, t := range torrents {
		if t.isSeed && t.isFinished && t.state == "Seeding" && t.progress == 100 && t.savePath == d.doneFolder {
			fin = append(fin, t)
		}
	}
	return fin
}
func (d *Deluge) getDownloading() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StateDownloading)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	var dls []DelugeTorrent
	for _, t := range torrents {
		if !t.isSeed && !t.isFinished && t.state == "Downloading" && t.progress < 100 && t.savePath == d.downloadFolder {
			dls = append(dls, t)
		}
	}
	return dls
}
func (d *Deluge) getErrors() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StateError)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	var errs []DelugeTorrent
	for _, t := range torrents {
		if t.state == "Error" {
			errs = append(errs, t)
		}
	}
	return errs
}
func (d *Deluge) getChecking() []DelugeTorrent {
	torrents, e := d.TorrentsStatus(delugeclient.StateChecking)
	if e != nil && !strings.Contains(e.Error(), `field "ETA"`) {
		chkFatal(e)
	}
	return torrents
}
func (d *Deluge) checkStuckTorrents() {
	tors := d.getTorrents()
	for _, v := range tors {
		if v.state == "Downloading" && v.progress == 100 {
			n := d.stuckDl[v.id]
			n = n + 1
			d.stuckDl[v.id] = n
			if n >= 3 {
				delete(d.stuckDl, v.id)
				e := d.ForceRecheck(v.id)
				chk(e)
			}
		}
		if v.state == "Seeding" && v.progress == 100 && v.savePath == d.downloadFolder {
			n := d.stuckSeeds[v.id]
			n = n + 1
			d.stuckSeeds[v.id] = n
			if n >= 3 {
				delete(d.stuckSeeds, v.id)
				e := d.MoveStorage([]string{v.id}, d.doneFolder)
				chk(e)
			}
		}
		if v.state == "Paused" {
			n := d.stuckPaused[v.id]
			n = n + 1
			d.stuckPaused[v.id] = n
			if n >= 3 {
				delete(d.stuckPaused, v.id)
				e := d.ResumeTorrents(v.id)
				chk(e)
			}
		}
	}
}
func (d *Deluge) checkFinishedTorrents() {
	if !isSnapraidRunning() {
		if d.keepDone {
			d.linkFinishedTorrents()
		} else {
			d.removeFinishedTorrents()
		}
	} else {
		//p("snapraid is running. not removing finished torrents for now.")
	}
}
func (d *Deluge) removeFinishedTorrents() {
	torrents := d.getFinished()

	for _, dt := range torrents {
		if keepTorrent(dt.id) {
			continue
		}
		p("torrent finished on %s: %s", d.name, dt.name)
		e := dt.pause()
		if e != nil {
			p(e.Error())
			continue
		}
		e = dt.remove(false)
		chk(e)
		dt.moveFiles()
	}
	rmEmptyFolders(d.doneFolder)
}
func (d *Deluge) linkFinishedTorrents() {
	var fin []string
	torrents := d.getFinished()
	if len(torrents) > 0 {
		p("found %d finished torrents on %s", len(torrents), d.name)
	}
	for _, dt := range torrents {
		if isAny(dt.name, d.finished...) {
			fin = append(fin, dt.name)
			continue
		}
		p("torrent finished on %s: %s", d.name, dt.name)
		e := dt.linkFiles()
		chkFatal(e)
		e = dt.moveStorage()
		chkFatal(e)
		fin = append(fin, dt.name)
	}
	d.finished = fin
}
func (d *Deluge) recheckErrors() {
	errs := d.getErrors()
	if len(errs) > 0 {
		p("found %d torrents in error state on deluge %s. forcing recheck now.", len(errs), d.name)
	}
	for _, t := range errs {
		st, e := d.TorrentStatus(t.id)
		chk(e)
		state := delugeclient.TorrentState(st.state)
		if state == delugeclient.StateError {
			e := d.ForceRecheck(t.id)
			chk(e)
			checking := true
			var lastState string
			for checking {
				st, e := d.TorrentStatus(t.id)
				chk(e)
				state := delugeclient.TorrentState(st.state)
				if state != delugeclient.StateChecking && state != delugeclient.StateError {
					p("Torrent recheck on %s for %s complete. State is now %s", d.name, st.name, st.state)
					checking = false
				} else {
					msg := fmt.Sprintf("Torrent state for %s is %s", st.name, st.state)
					if msg != lastState {
						lastState = msg
						p(msg)
					}
				}

				time.Sleep(3 * time.Second)
			}
		}
	}
	if len(errs) > 0 {
		p("finished recheck on %d torrents in error state on deluge %s.", len(errs), d.name)
	}
}

type DelugeTorrent struct {
	name, id, relPath                     string
	state, savePath                       string
	timeSeeded, timeActive, timeCompleted time.Duration
	timeAdded                             time.Time
	ratio, progress                       float32
	files                                 []string
	deluge                                *Deluge
	isSeed, isFinished                    bool
}

func (dt *DelugeTorrent) pause() error {
	p("pausing %s torrent %s", dt.deluge.name, dt.name)
	return dt.deluge.PauseTorrents(dt.id)
}
func (dt *DelugeTorrent) remove(rmFile bool) error {
	p("removing %s torrent %s", dt.deluge.name, dt.name)
	return dt.deluge.RemoveTorrent(dt.id, rmFile)
}
func (dt *DelugeTorrent) resume() error {
	p("resuming %s torrent %s", dt.deluge.name, dt.name)
	return dt.deluge.ResumeTorrents(dt.id)
}
func (dt *DelugeTorrent) moveFiles() {
	p("moving %d files from %s torrent %s", len(dt.files), dt.deluge.name, dt.name)
	if dt.relPath == "" {
		e := verifyFolder(preProcFolder)
		chkFatal(e)
		for _, f := range dt.files {
			dst := strings.Replace(f, dt.deluge.doneFolder, preProcFolder, 1)
			e := os.Rename(f, dst)
			chk(e)
		}
	} else {
		src := filepath.Join(dt.deluge.doneFolder, dt.relPath)
		dst := filepath.Join(preProcFolder, dt.relPath)
		e := verifyFolder(filepath.Dir(dst))
		chkFatal(e)
		mvTree(src, dst, true)
	}
}
func (dt *DelugeTorrent) linkFiles() error {
	p("linking %d files from %s torrent %s", len(dt.files), dt.deluge.name, dt.name)

	for _, src := range dt.files {
		dst := strings.Replace(src, dt.deluge.doneFolder, preProcFolder, 1)
		e := verifyFolder(filepath.Dir(dst))
		chkFatal(e)
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
	p("move %s torrent to new storage location: %s -> %s", dt.deluge.name, dt.name, dt.deluge.seedFolder)
	return dt.deluge.MoveStorage([]string{dt.id}, dt.deluge.seedFolder)
}

func keepTorrent(id string) bool {
	if fileExists(keepFile) {
		keeps := getFile(keepFile)
		for _, line := range strings.Split(keeps, "\n") {
			if id == line {
				return true
			}
		}
	}
	return false
}

func parseConfig() {
	var confFile string
	b, e := os.ReadFile(conf)
	if e == nil {
		confFile = string(b)
	}

	if confFile == "" {
		p("no conf file found at %s", conf)
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
				p("pre_proc_folder  -> %s", v)
				preProcFolder = v
				protectedFolders = append(protectedFolders, v)
			case "proc_folder":
				p("procFolder       -> %s", v)
				procFolder = v
				protectedFolders = append(protectedFolders, v)
			case "convert_folder":
				p("convert_folder   -> %s", v)
				convertFolder = v
				protectedFolders = append(protectedFolders, v)
			case "recycle_folder":
				p("recycle_folder   -> %s", v)
				recycleFolder = v
				protectedFolders = append(protectedFolders, v)
			case "torrent_folder":
				p("torrent_folder   -> %s", v)
				torFolder = v
				protectedFolders = append(protectedFolders, v)
			case "problem_folder":
				p("problem_folder   -> %s", v)
				probFolder = v
				protectedFolders = append(protectedFolders, v)
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
				deluge.keepTime = time.Duration(days*24) * time.Hour
			case "download_folder":
				deluge.downloadFolder = v
				protectedFolders = append(protectedFolders, v)
			case "finished_folder":
				deluge.doneFolder = v
				protectedFolders = append(protectedFolders, v)
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
			case "sab_key":
				sabKey = v
			case "sonarr_ip":
				sonarrIp = v
			case "sonarr_port":
				sonarrPort = v
			case "sonarr_key":
				sonarrKey = v
			case "stale_age":
				age, e := strconv.Atoi(v)
				chkFatal(e)
				staleAge = time.Duration(age*24) * time.Hour
			}
		}
	}
	e = verifyFolders()
	chkFatal(e)
}
func getDelugeClients() {
	for _, d := range delugeDaemons {
		d.start()
	}
}

type Sonarr struct {
	Episodes []struct {
		Id int `json:"id"`
	} `json:"episodes"`
	Records []struct {
		Id        int    `json:"id"`
		EventType string `json:"eventType"`
	} `json:"records"`
}

func (s *Sonarr) blacklist(title string) (e error) {
	var epId, recordId int
	epId, e = s.parseTitle(title)
	if e != nil {
		p("error during sonarr title parse: %s", e.Error())
		return
	}
	recordId, e = s.getLastGrabId(epId)
	if e != nil {
		p("error during sonarr episode history lookup: %s", e.Error())
		return
	}
	e = s.markFailed(recordId)
	if e != nil {
		p("error during sonarr mark failed: %s", e.Error())
	}
	return
}
func (s *Sonarr) parseTitle(title string) (id int, e error) {
	uri := fmt.Sprintf(`http://%s:%s/api/v3/parse`, sonarrIp, sonarrPort)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Add("title", title)
	req.URL.RawQuery = q.Encode()
	rsp, e := client.Do(req)
	if e != nil {
		return
	}
	defer rsp.Body.Close()
	if rsp.ContentLength == 0 {
		e = fmt.Errorf("no results found parsing title: %s", title)
		return
	}
	body, e := io.ReadAll(rsp.Body)
	if e != nil {
		return
	}
	e = json.Unmarshal(body, s)
	if e != nil {
		return
	}
	if len(s.Episodes) > 0 {
		id = s.Episodes[0].Id
	} else {
		e = fmt.Errorf("no results found parsing title: %s", title)
	}
	return
}
func (s *Sonarr) getLastGrabId(episodeId int) (recordId int, e error) {
	uri := fmt.Sprintf(`http://%s:%s/api/v3/history`, sonarrIp, sonarrPort)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Add("sortKey", "date")
	q.Add("sortDir", "desc")
	q.Add("episodeId", fmt.Sprintf("%d", episodeId))
	req.URL.RawQuery = q.Encode()
	rsp, e := client.Do(req)
	if e != nil {
		fmt.Println(e)
		return
	}
	defer rsp.Body.Close()
	body, e := io.ReadAll(rsp.Body)
	if e != nil {
		fmt.Println(e)
		return
	}
	e = json.Unmarshal(body, s)
	if e != nil {
		fmt.Println(e)
		return
	}

	for _, record := range s.Records {
		if record.EventType == "grabbed" {
			recordId = record.Id
			return
		}
	}
	e = fmt.Errorf("no grabbed record found for episodeId")
	return
}
func (s *Sonarr) markFailed(recordId int) (e error) {
	uri := fmt.Sprintf(`http://%s:%s/api/v3/history/failed/%d`, sonarrIp, sonarrPort, recordId)
	client := &http.Client{}
	req, e := http.NewRequest("POST", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", "0")
	rsp, e := client.Do(req)
	if e != nil {
		fmt.Println(e)
		return
	}
	defer rsp.Body.Close()
	return
}
func (s *Sonarr) isOnline() bool {
	uri := fmt.Sprintf(`http://%s:%s/api/v3/system/status`, sonarrIp, sonarrPort)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	rsp, e := client.Do(req)
	if e != nil {
		return false
	}
	defer rsp.Body.Close()
	return rsp.StatusCode == http.StatusOK
}

type StaleTorrent struct{}

func (st *StaleTorrent) start() {
	time.Sleep(staleStartDelay)
	p("starting stale torrent monitor")
	e := os.MkdirAll(filepath.Dir(dbFile), 0644)
	chkFatal(e)
	cmd := `CREATE TABLE IF NOT EXISTS blacklist(id TEXT, title TEXT, daemon TEXT);`
	dbExec(cmd, dbFile)
	s := Sonarr{}

	purge := func(t *DelugeTorrent) {
		e = t.pause()
		if e != nil {
			p("error during pause attempt: %s", e.Error())
			//chkFatal(e)
			return
		}
		e = t.remove(true)
		if e != nil {
			p("error during remove attempt: %s", e.Error())
			//chkFatal(e)
			return
		}
		p("blacklist torrent in sonarr: %s", t.name)
		e = s.blacklist(t.name)
		if e != nil {
			p("sonarr blacklist failed for %s", t.name)
			//chkFatal(e)
			return
		}
	}

	for {
		if s.isOnline() {
			staleTime := time.Now().Add(-1 * staleAge)
			for _, d := range delugeDaemons {
				torrents := d.getDownloading()
				for _, torrent := range torrents {
					if st.inBlacklist(&torrent) {
						p("staleAge %v", staleAge)
						p("timeAdded %s", torrent.timeAdded)
						p("blacklisted torrent found in %s. Removing %s", torrent.deluge.name, torrent.name)
						purge(&torrent)
					} else if torrent.progress < 100 && torrent.timeAdded.Before(staleTime) {
						p("staleAge %v", staleAge)
						p("timeAdded %s", torrent.timeAdded)
						p("torrent %s is stale: %f%% available after %d days.",
							torrent.name, torrent.progress, int(time.Now().Sub(torrent.timeAdded).Hours()/24))
						st.addToBlacklist(&torrent)
						purge(&torrent)
					}
					//os.Exit(1)
				}
			}
		} else {
			p("sonarr is offline. skipping stale torrent check")
		}
		time.Sleep(staleInterval)
	}
}
func (st *StaleTorrent) addToBlacklist(t *DelugeTorrent) {
	cmd := "INSERT INTO blacklist VALUES(?, ?, ?);"
	dbExec(cmd, dbFile, t.id, t.name, t.deluge.name)
}
func (st *StaleTorrent) inBlacklist(t *DelugeTorrent) bool {
	result := dbQuery(`SELECT title FROM blacklist WHERE daemon = ?;`, dbFile, t.deluge.name)
	return len(result) > 0
}

type TorFile struct{}

func (tf *TorFile) start() {
	p("torFile started and monitoring %s", torFolder)
	for {
		tf.getFiles()
		time.Sleep(torFileInterval)
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
	e := verifyFolder(torFolder)
	chkFatal(e)
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
			dd.AddTorrentMagnet(magnetPath)
			return
		}
	}
	defaultDeluge.AddTorrentMagnet(magnetPath)
}
func (tf *TorFile) torrent(torrentPath string) {
	b, e := os.ReadFile(torrentPath)
	chkFatal(e)
	mag := string(b)
	for _, dd := range delugeDaemons {
		if containsString(mag, dd.trackers...) {
			dd.AddTorrentFile(torrentPath)
			return
		}
	}
	defaultDeluge.AddTorrentFile(torrentPath)
}

func extractPreProc() {
	e := verifyFolder(preProcFolder)
	chkFatal(e)
	if !isDirEmpty(preProcFolder) {
		p("running extract in %s", preProcFolder)
		err := run("extract", preProcFolder)
		chk(err)
	}
}
func muxPreProc() {
	e := verifyFolder(preProcFolder)
	chkFatal(e)
	if !isDirEmpty(preProcFolder) {
		p("running mux in %s", preProcFolder)
		cmd := []string{"mux", "-r", "-p", preProcFolder, "-mc", convertFolder}
		if probFolder != "" {
			cmd = append(cmd, "-prob", probFolder)
		}
		if recycleFolder != "" {
			cmd = append(cmd, "-recycle", recycleFolder)
		}

		err := run(cmd...)
		chk(err)
	}
}
func muxConvert() {
	time.Sleep(convertStartDelay)
	p("starting convert folder monitor")
	for {
		e := verifyFolder(convertFolder, probFolder, recycleFolder)
		chkFatal(e)
		if !isDirEmpty(convertFolder) {
			p("running mux in %s", convertFolder)
			cmd := []string{"mux", "-r", "-p", convertFolder, "-mf", procFolder}
			if probFolder != "" {
				cmd = append(cmd, "-prob", probFolder)
			}
			if recycleFolder != "" {
				cmd = append(cmd, "-recycle", recycleFolder)
			}
			err := run(cmd...)
			chk(err)
			rmEmptyFolders(convertFolder)
		}
		time.Sleep(convertInterval)
	}
}
func recheckErrors() {
	time.Sleep(errorCheckStartDelay)
	p("starting errored torrents monitor")
	for {
		for _, d := range delugeDaemons {
			d.recheckErrors()
		}
		time.Sleep(errorCheckInterval)
	}
}
func pruneTorrents() {
	time.Sleep(pruneStartDelay)
	p("starting prune torrents monitor")
	for {
		if !isSnapraidRunning() {
			for _, d := range delugeDaemons {
				if d.keepDone {
					torrents := d.getTorrents()
					for _, t := range torrents {
						if t.timeSeeded > d.keepTime || (t.ratio > d.keepRatio && d.keepRatio != 0) {
							p("torrent %s being removed from %s with ratio %f and seed time of %d days",
								t.name, d.name, t.ratio, int(t.timeSeeded.Hours()/24))
							e := t.pause()
							chk(e)
							e = t.remove(true)

						}
					}
				}
			}
		}

		time.Sleep(pruneInterval)
	}
}
func finishTorrents() {
	time.Sleep(finishStartDelay)
	p("starting finished & stuck torrent monitors")
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
		time.Sleep(finishInterval)
	}
}

func main() {
	p("starting hydra")
	p("--------------")
	parseConfig()
	getDelugeClients()
	go torFile.start()
	go staleTorrent.start()
	go muxConvert()
	go pruneTorrents()
	go finishTorrents()
	go recheckErrors()

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
func isSnapraidRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "snapraid")
	e := cmd.Run()
	return e == nil
}

func verifyFolder(paths ...string) error {
	for _, path := range paths {
		f, e := os.Stat(path)
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				e = os.MkdirAll(path, 0755)
				if e != nil {
					return e
				}
			} else {
				return e
			}
		} else {
			if !f.IsDir() {
				return errors.New(fmt.Sprintf("path is not a folder: %s", path))
			}
		}
	}
	return nil
}
func verifyFolders() error {
	for _, f := range protectedFolders {
		e := verifyFolder(f)
		if e != nil {
			return e
		}
	}
	return nil
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
	getFile        = base.GetFile
	dbExec         = base_db.DbExec
	dbQuery        = base_db.DqQuery
)
