package main

import (
	"fmt"
	. "github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

/*
	ssd_flush
	automatically move files with ctime older than x hours from mergerfs ssd cache to hard disks
	this is intended to move files from a set of SSDs acting as a cache in a mergerfs array onto the hdds in this array
		ssds are mounted into mergerfs /z array at /mnt/ssd0{1..4}/cache
		hdds are mounted into mergerfs /z and /zhdd array at /mnt/hdd*
		/mnt/zhdd is a separate mergerfs array with only hdds mounted
		/mnt/zhdd is subset of /z that excludes ssds and includes only hdds
		when service is started (by systemd timer) each ssd is checked for files with ctime older than moveAge (hours)
			qualifying files are moved from cache folders on ssd mount to hdd-only array

	- this is not run as a container, but installed onto the host system as a systemd service
	- will not run while snapraid is active
	- will not run more than one instance of itself

	service depends on
		mnt-ssd01.mount
		mnt-ssd02.mount
		mnt-ssd03.mount
		mnt-ssd04.mount
		mnt-zhdd.mount

*/

var (
	ssds = []string{
		"/mnt/ssd01/cache",
		"/mnt/ssd02/cache",
		"/mnt/ssd03/cache",
		"/mnt/ssd04/cache",
	}
	array = "/mnt/zhdd"

	moveAge = 1  // hours
	force   bool // for -f arg to ignore create time and just flush everything
	logFile = fmt.Sprintf("/var/server_logs/ssd_flush-%s.log", getTimestamp())
)

func isSnapraidRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "snapraid")
	e := cmd.Run()
	return e == nil
}
func amIRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "ssd_flush")
	out, _ := cmd.Output()
	pids := strings.Split(string(out), " ")
	return len(pids) > 1
}

func timeToMove(info os.FileInfo) bool {
	stat := info.Sys().(*syscall.Stat_t)
	ctime := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	return time.Since(ctime) > time.Duration(moveAge)*time.Hour
}

func moveOldFiles() error {
	var files []string
	var folders []string
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if force || timeToMove(info) {
			if info.IsDir() {
				folders = append(folders, p)
			} else {
				files = append(files, p)
			}
		}
		return nil
	}
	for _, ssd := range ssds {
		files = []string{}
		folders = []string{}
		err := filepath.Walk(ssd, walk)
		chk(err)
		numFile := len(files)
		numFolder := len(folders)
		p("found %d files on %s to move", numFile, ssd)
		p("found %d folders on %s to move", numFolder, ssd)
		for i, folder := range folders {
			folder = strings.Replace(folder, ssd, array, 1)
			p("%d/%d creating folder: %s", i, numFolder, folder)
			err = os.MkdirAll(folder, 0777)
			if err != nil {
				return err
			}
		}
		for i, src := range files {
			dst := strings.Replace(src, ssd, array, 1)
			p("%d/%d moving file: %s -> %s", i+1, numFile, ssd, dst)
			err = mvFile(src, dst)
			if err != nil {
				return err
			}

			//err = os.MkdirAll(filepath.Dir(dst), 0777)
			//e := run("rsync", "-aWmvh", "--preallocate", "--remove-source-files", src, dst)
			//chkFatal(e)
		}
		p("removing empty folders on %s", ssd)
		rmEmptyFolders(ssd)
	}
	return nil
}

func getArgs() {
	var e error
	args := os.Args

	if isAny("-h", args...) {
		fmt.Println("ssd_flush\n" +
			"  -h help\n" +
			"  -f force, move all data regardless of age\n" +
			"  -t <number> age in hours of data to move\n" +
			"     default is 1 hour")
		os.Exit(0)
	}

	if isAny("-f", args...) {
		force = true
	}

	if setTimeArg := arrayIdx(args, "-t"); setTimeArg != -1 {
		if force {
			fmt.Println("cannot set -f with -t. -f is equivalent to -t 0.")
			os.Exit(1)
		}
		if len(args) >= setTimeArg+2 {
			moveAge, e = strconv.Atoi(args[setTimeArg+1])
			if e != nil {
				fmt.Println("must specify whole number for time in hours with -t.")
				os.Exit(1)
			}
		} else {
			fmt.Println("must specify whole number for time in hours with -t.")
			os.Exit(1)
		}
	}

}

func main() {
	p("ssd_flush called with args: %s", strings.Join(os.Args[1:], " "))
	//fn := logOutput(logFile)
	if isSnapraidRunning() {
		p("snapraid is running. exiting.")
		return
	}
	if amIRunning() {
		p("ssd_flush is already running. exiting.")
		return
	}
	getArgs()

	_ = moveOldFiles()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
	)
	<-signalChan
	p("exiting.")
	//fn()
	//if err != nil {
	//	b, e := os.ReadFile(logFile)
	//	if e != nil {
	//		p("error during log read for email: %s", e.Error())
	//		return
	//	}
	//	email := Email{
	//		Subject: fmt.Sprintf("ssd_flush error: %s", filepath.Base(logFile)),
	//		Body:    string(b),
	//	}
	//	e = email.Send()
	//	if e != nil {
	//		p("error during email send: %s", e.Error())
	//	}
	//}
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
	logOutput      = LogOutput
	getTimestamp   = GetTimestamp
)
