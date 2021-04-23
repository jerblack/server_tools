package main

import (
	"github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"path/filepath"
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

	service depends on
		mnt-ssd01.mount                                                                                                                                                    loaded active mounted   /mnt/ssd01
		mnt-ssd02.mount                                                                                                                                                    loaded active mounted   /mnt/ssd02
		mnt-ssd03.mount                                                                                                                                                    loaded active mounted   /mnt/ssd03
		mnt-ssd04.mount                                                                                                                                                    loaded active mounted   /mnt/ssd04
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

	moveAge = 6 // hours
)

func isSnapraidRunning() bool {
	cmd := exec.Command("/usr/bin/pidof", "snapraid")
	e := cmd.Run()
	return e == nil
}

func timeToMove(info os.FileInfo) bool {
	stat := info.Sys().(*syscall.Stat_t)
	ctime := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	return time.Since(ctime) > time.Duration(moveAge)*time.Hour
}

func moveOldFiles() {
	var files []string
	var folders []string
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if timeToMove(info) {
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
		for _, folder := range folders {
			folder = strings.Replace(folder, ssd, array, 1)
			p("creating folder: %s", folder)
			err = os.MkdirAll(folder, 0777)
			chkFatal(err)
		}
		for _, src := range files {
			dst := strings.Replace(src, ssd, array, 1)
			p("moving file: %s -> %s", ssd, dst)
			e := run("rsync", "-aWmvh", "--preallocate", "--remove-source-files", src, dst)
			chkFatal(e)
		}
		p("removing empty folders on %s", ssd)
		rmEmptyFolders(ssd)
	}
}

func main() {
	if isSnapraidRunning() {
		p("snapraid is running. exiting.")
		return
	}
	moveOldFiles()
}

var (
	p              = base.P
	chk            = base.Chk
	chkFatal       = base.ChkFatal
	run            = base.Run
	rmEmptyFolders = base.RmEmptyFolders
)
