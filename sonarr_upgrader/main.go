package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	progressFile = ".config/sonarr_upgrader.progress"
	progress     []int
	sonarrHost   = "sonarr"
	sabnzbdHost  = "sabnzbd"
	sonarrKey    = os.Getenv("SONARR_KEY")
	sabnzbdKey   = os.Getenv("SABNZBD_KEY")
	sonarrShows  []*SonarrShow
)

func main() {
	p("starting sonarr_upgrader")
	home, _ := os.UserHomeDir()
	progressFile = filepath.Join(home, progressFile)
	readProgress()
	p("%d entries in progress file", len(progress))
	p("getting shows from sonarr")
	getAllSonarrShows()
	p("sonarr returned %d shows", len(sonarrShows))

	p("initiating series searches")
	for _, show := range sonarrShows {
		if chkProgress(show.Id) {
			continue
		}
		p("searching for upgrades for show %s", show.Title)
		sonarrSeriesSearch(show.Id)
		time.Sleep(30 * time.Second)
		p("checking status of SeriesSearch command in sonarr")
		for isSonarrTaskActive(show) {
			time.Sleep(1 * time.Minute)
		}
		p("checking status of sabnzbd queue")
		for isSabActive() {
			time.Sleep(1 * time.Minute)
		}
		writeProgress(show.Id)
	}
	p("sonarr_upgrader finished")

}

func readProgress() {
	if fileExists(progressFile) {
		b, e := os.ReadFile(progressFile)
		chkFatal(e)
		lines := strings.Split(string(b), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			i, e := strconv.Atoi(line)
			chkFatal(e)
			progress = append(progress, i)
		}
	}
}
func writeProgress(i int) {
	progress = append(progress, i)
	f, e := os.OpenFile(progressFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	chkFatal(e)
	_, e = f.WriteString(fmt.Sprintf("%d\n", i))
	chkFatal(e)
	e = f.Close()
	chkFatal(e)
}
func chkProgress(i int) bool {
	return isAnyInt(i, progress...)
}

type SonarrSearchRequest struct {
	Name     string `json:"name"`
	SeriesId int    `json:"seriesId"`
}

func sonarrSeriesSearch(id int) {
	uri := fmt.Sprintf(`http://%s/api/v3/command`, sonarrHost)
	search := SonarrSearchRequest{
		Name: "SeriesSearch", SeriesId: id,
	}
	b, e := json.Marshal(search)
	chkFatal(e)
	client := &http.Client{}
	req, e := http.NewRequest("POST", uri, bytes.NewBuffer(b))
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	rsp, e := client.Do(req)
	chkFatal(e)
	e = rsp.Body.Close()
	chk(e)
}

type SonarrShow struct {
	Title string `json:"title"`
	Id    int    `json:"id"`
}

func getAllSonarrShows() {
	uri := fmt.Sprintf(`http://%s/api/v3/series`, sonarrHost)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	rsp, e := client.Do(req)
	chkFatal(e)
	defer rsp.Body.Close()
	body, e := io.ReadAll(rsp.Body)
	chkFatal(e)
	e = json.Unmarshal(body, &sonarrShows)
	chkFatal(e)
}

type SonarrTask struct {
	Name        string `json:"name"`
	CommandName string `json:"commandName"`
	Message     string `json:"message"`
	Body        struct {
		SeriesId int `json:"seriesId,omitempty"`
	} `json:"body"`
	Status          string    `json:"status"`
	Queued          time.Time `json:"queued"`
	Started         time.Time `json:"started"`
	StateChangeTime time.Time `json:"stateChangeTime"`
	Id              int       `json:"id"`
	Ended           time.Time `json:"ended,omitempty"`
}

func isSonarrTaskActive(show *SonarrShow) bool {
	uri := fmt.Sprintf(`http://%s/api/v3/command`, sonarrHost)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("X-Api-Key", sonarrKey)
	req.Header.Set("Content-Type", "application/json")
	rsp, e := client.Do(req)
	chkFatal(e)
	defer rsp.Body.Close()
	body, e := io.ReadAll(rsp.Body)
	chkFatal(e)
	var sonarrTasks []*SonarrTask
	e = json.Unmarshal(body, &sonarrTasks)
	chkFatal(e)
	for _, task := range sonarrTasks {
		if task.Name == "SeriesSearch" && task.Body.SeriesId == show.Id {
			if task.Status == "completed" {
				p("series search: %s | status completed", show.Title)
				return false
			} else {
				p("series search: %s | status %s | message %s", show.Title, task.Status, task.Message)
				return true
			}
		}
	}
	p("series search: %s | no search found", show.Title)
	return false

}

type SabQueue struct {
	Queue struct {
		NoOfSlotsTotal int    `json:"noofslots_total"`
		Status         string `json:"status"`
	} `json:"queue"`
}

func isSabActive() bool {
	uri := fmt.Sprintf(`http://%s/sabnzbd/api/`, sabnzbdHost)
	client := &http.Client{}
	req, e := http.NewRequest("GET", uri, nil)
	chkFatal(e)
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Add("output", "json")
	q.Add("apikey", sabnzbdKey)
	q.Add("mode", "queue")
	req.URL.RawQuery = q.Encode()

	rsp, e := client.Do(req)
	chkFatal(e)
	defer rsp.Body.Close()
	body, e := io.ReadAll(rsp.Body)
	chkFatal(e)
	var sq SabQueue
	e = json.Unmarshal(body, &sq)
	chkFatal(e)
	if sq.Queue.NoOfSlotsTotal == 0 && sq.Queue.Status == "Idle" {
		p("sabnzbd | queue is empty")
		return false
	} else {
		p("sabnzbd | downloads in queue: %d | status: %s", sq.Queue.NoOfSlotsTotal, sq.Queue.Status)
		return true
	}
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	fileExists = base.FileExists
	isAnyInt   = base.IsAnyInt
)
