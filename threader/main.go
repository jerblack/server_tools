package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	srcPath, dstPath string
	jobs             chan []string
	done             chan bool
	watch, relative  bool
)

func move(src, dst string) {
	st, e := os.Stat(src)
	if e != nil {
		chk(e)
		return
	}
	p("moving | %6s | %s", humanSize(st.Size()), strings.TrimPrefix(src, srcPath))
	e = os.MkdirAll(filepath.Dir(dst), 0777)
	chk(e)

	cmd := []string{"rsync", "-aWmvh", "--preallocate", "--remove-source-files"}
	//if relative {
	//	cmd = append(cmd, "--relative")
	//}
	cmd = append(cmd, src, dst)
	for i := 1; i < 4; i++ {
		e = run(cmd...)
		if e == nil {
			return
		}
		p(e.Error())
		p("failed, retry %d: %s", i, src)
	}
}

func empty(src string) {
	cmd := []string{"find", src, "-type", "d", "-empty", "-delete"}
	st, e := os.Stat(src)
	if e == nil && st.IsDir() {
		p("emptying | %s", src)
		e := run(cmd...)
		chk(e)
	}
}

func getJobs() {
	//maxSize := int64(size * 1000000)
	if relative && !strings.Contains(srcPath, "/./") {
		p("-r set without including /./ in source path")
		os.Exit(1)
	}
	var base string
	if relative {
		base = strings.Split(srcPath, "/./")[0]
		base, _ = filepath.Abs(base)
	}

	st, e := os.Stat(srcPath)
	if e == nil && st.IsDir() {
		p("walking dir: %s", srcPath)
		walk := func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if relative {
					abs, _ := filepath.Abs(p)
					dst := strings.Replace(abs, base, dstPath, 1)
					jobs <- []string{abs, dst}

				} else {
					jobs <- []string{p, strings.Replace(p, srcPath, dstPath, 1)}
				}
			}
			return nil
		}
		e = filepath.Walk(srcPath, walk)
		chkFatal(e)
	}
	close(jobs)
}

func worker() {
	for j := range jobs {
		move(j[0], j[1])
	}
	done <- true
}

func main() {
	help := func() {
		fmt.Println("threader FLAGS <src> (+ <src> + <src>) <dst>\n" +
			"-h help\n" +
			"-t <threads> number of threads\n" +
			"-r relative, use dot (/./) in src. './a/./b/c/f' to copy relative to b\n" +
			"-w watch rsync, for debugging process")
		os.Exit(0)
	}
	workers := 4
	var srcs []string

	for i, a := range os.Args {
		switch a {
		case "-t":
			w, e := strconv.Atoi(os.Args[i+1])
			if e != nil {
				help()
				return
			}
			workers = w
		case "-r":
			relative = true
		case "-h":
			help()
			return
		case "-w":
			watch = true
		case "+":
			if len(srcs) == 0 {
				srcs = append(srcs, os.Args[i-1])
			}
			srcs = append(srcs, os.Args[i+1])
		}
	}

	work := func(numWorkers int) {
		jobs = make(chan []string, 1024)
		done = make(chan bool, numWorkers)

		go getJobs()

		for i := 0; i < numWorkers; i++ {
			go worker()
		}
		for i := 0; i < numWorkers; i++ {
			<-done
		}
		empty(srcPath)
	}
	srcDst := os.Args[len(os.Args)-2:]
	dstPath = srcDst[1]

	if len(srcs) == 0 {
		srcPath = srcDst[0]
		p("threader | src: %s | dst: %s", srcPath, dstPath)
		work(workers)
	} else {
		for _, src := range srcs {
			srcPath = src
			p("threader | src: %s | dst: %s", srcPath, dstPath)
			work(workers)
		}
	}
	p("done")
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
func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	if watch {
		printCmd(args)
		var stdBuffer bytes.Buffer
		mw := io.MultiWriter(os.Stdout, &stdBuffer)

		cmd.Stdout = mw
		cmd.Stderr = mw
	}
	return cmd.Run()

}
func printCmd(cmd []string) {
	var parts []string
	for _, c := range cmd {
		if strings.Contains(c, " ") {
			c = fmt.Sprintf("\"%s\"", c)
		}
		parts = append(parts, c)
	}
	p(strings.Join(parts, " "))
}
func humanSize(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%8dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%6.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}
