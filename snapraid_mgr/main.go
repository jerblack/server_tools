package main

import (
	"fmt"
	. "github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	sonarrCompose = "/x/.config/docker/sonarr/docker-compose.yml"
	snapraidConf  = "/etc/snapraid.conf"
	snapraidBin   = "/usr/local/bin/snapraid"
)

func isSnapraidRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "snapraid")
	e := cmd.Run()
	return e == nil
}
func isSsdFlushRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "ssd_flush")
	e := cmd.Run()
	return e == nil
}
func waitForSsdFlush() {
	p("checking for already running instance of ssd_flush")
	for {
		if isSsdFlushRunning() {
			p("ssd_flush is running")
			time.Sleep(30 * time.Second)
		} else {
			return
		}
	}
}
func runSsdFlush() error {
	// ssd_flush will email own log if error
	p("running ssd_flush -f")
	e := run("/usr/local/bin/ssd_flush", "-f")
	return e
}
func amIRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", os.Args[0])
	out, e := cmd.Output()
	chk(e)
	pids := strings.Split(string(out), " ")
	return len(pids) > 1
}
func stopSonarr() {
	p("stopping sonarr.service")
	cmd := exec.Command("/usr/bin/systemctl", "stop", "sonarr.service")
	e := cmd.Run()
	chk(e)
	// should be stopped, but make sure
	p("ensuring sonarr container is stopped")
	cmd = exec.Command("/usr/bin/docker", "-f", sonarrCompose, "down")
	e = cmd.Run()
	chk(e)
}
func startSonarr() {
	p("starting sonarr.service")
	cmd := exec.Command("/usr/bin/systemctl", "start", "sonarr.service")
	e := cmd.Run()
	chk(e)
}

func verifySnapraidConf() error {
	p("verifying references in %s", snapraidConf)
	b, e := os.ReadFile(snapraidConf)
	if e != nil {
		return e
	}
	var log []string
	var dataDrives []string
	lines := strings.Split(string(b), "\n")
	p("verifying .parity and .content files are accessible")
	for _, line := range lines {
		if strings.HasSuffix(line, ".parity") {
			parts := strings.SplitN(line, " ", 2)
			_, e = os.Stat(parts[1])
			if e != nil {
				err := fmt.Sprintf("parity file error: %s : %s", parts[1], e.Error())
				log = append(log, err)
			}
		} else if strings.HasSuffix(line, ".content") {
			parts := strings.SplitN(line, " ", 2)
			_, e = os.Stat(parts[1])
			if e != nil {
				err := fmt.Sprintf("content file error: %s : %s", parts[1], e.Error())
				log = append(log, err)
			}
		} else if strings.HasPrefix(line, "data ") {
			parts := strings.SplitN(line, " ", 3)
			dataDrives = append(dataDrives, parts[2])
		}
	}
	p("verifying all data drives are mounted")
	var mnts []string
	b, e = os.ReadFile("/proc/self/mountinfo")
	if e != nil {
		return e
	}
	lines = strings.Split(string(b), "\n")
	for _, line := range lines {
		parts := strings.Split(line, " ")
		if len(parts) > 2 {
			mnts = append(mnts, parts[4])
		}
	}

	for _, d := range dataDrives {
		if !isAny(d, mnts...) {
			err := fmt.Sprintf("data drive error: %s : drive not mounted", d)
			log = append(log, err)
		} else {
			_, e = os.Stat(d)
			if e != nil {
				err := fmt.Sprintf("data drive error: %s : %s", d, e.Error())
				log = append(log, err)
			}
		}
	}

	if len(log) > 0 {
		err := strings.Join(log, "\n")
		email := Email{
			Subject: "snapraid_mgr error: verifying snapraid.conf",
			Body:    err,
		}
		e = email.Send()
		chk(e)
		return fmt.Errorf("error encountered during snapraid.conf verification. check email for details")
	}
	return nil
}
func runSnapraidSync() error {
	p("starting snapraid sync")
	logFile := fmt.Sprintf("/var/server_logs/snapraid_sync_%s.log", GetTimestamp())
	e := run(snapraidBin, "sync", "--log", logFile, "--verbose")
	if e != nil {
		b, err := os.ReadFile(logFile)
		if err != nil {
			fmt.Printf("error during sync log file read: %s, %s\n", logFile, err.Error())
			return e
		}
		log := fmt.Sprintf("%s\n\n%s", e.Error(), string(b))
		email := Email{
			Subject: "snapraid_mgr error: snapraid sync",
			Body:    log,
		}
		err = email.Send()
		if err != nil {
			fmt.Printf("error during sync log file email send: %s, %s\n", logFile, err.Error())
		}
		return e
	}
	return nil
}
func runSnapraidScrub() error {
	p("starting snapraid scrub")

	logFile := fmt.Sprintf("/var/server_logs/snapraid_scrub_%s.log", GetTimestamp())
	e := run(snapraidBin, "scrub", "--log", logFile, "--verbose")
	if e != nil {
		b, err := os.ReadFile(logFile)
		if err != nil {
			fmt.Printf("error during scrub log file read: %s, %s\n", logFile, err.Error())
			return e
		}
		log := fmt.Sprintf("%s\n\n%s", e.Error(), string(b))
		email := Email{
			Subject: "snapraid_mgr error: snapraid scrub",
			Body:    log,
		}
		err = email.Send()
		if err != nil {
			fmt.Printf("error during scrub log file email send: %s, %s\n", logFile, err.Error())
		}
		return e
	}
	return nil
}

func main() {
	if isSnapraidRunning() {
		p("snapraid_mgr: snapraid is already running")
		os.Exit(0)
	}
	stopSonarr()
	waitForSsdFlush()
	e := runSsdFlush()
	if e != nil {
		chk(e)
		os.Exit(1)
	}
	e = verifySnapraidConf()
	if e != nil {
		chk(e)
		os.Exit(1)
	}
	e = runSnapraidSync()
	if e != nil {
		chk(e)
		os.Exit(1)
	}
	e = runSnapraidScrub()
	if e != nil {
		chk(e)
		os.Exit(1)
	}
	startSonarr()

}

var (
	p              = P
	chk            = Chk
	chkFatal       = ChkFatal
	run            = Run
	rmEmptyFolders = RmEmptyFolders
	isAny          = IsAny
	arrayIdx       = ArrayIdx
	mvFile         = MvFile
)
