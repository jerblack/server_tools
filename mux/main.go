package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

/*
	remux all files into mkv with h264 video and ac3 audio with external subtitles embedded as mkv streams
		requires: ffmpeg ffprobe mkvtoolnix
		remux reasons
			non-mkv container
			external subtitles
			video stream not first strewm
			english audio not first audio stream (unless no english audio present)
			english subtitles not first subtitle stream (unless no english subs present)
		convert reason - audio
			not "aac", "ac3", "eac3", "flac", "alac", "dts", "mp3", "truehd"
		convert reason - video
			not "h264", "hevc", "mpeg4"
		optional external convert path
			in scenarios where long conversion process would block other processing, an option is provided to move files
			to a separate convert folder for a separate mux instance to convert
			if -mc specified with path to convert folder, any files that need conversion will be moved to that folder
			for later conversion
			separate process should run mux -p <convert folder> -mf <finished file folder> will convert any files in
			convert folder and move them to the finished file folder on completion
		options
			-p 	-p <start path>
				start path. mux all files in folder.
				if not specified, start path is current working directory (where mux was started from)
			-f  -f <path to file>
				mux single file
			-r	mux all files in start path recursively
			-mc	-mc <path to move files that need conversion>
				move any files that need conversion to path specified with -mc instead of converting
			-mf -mf <path to move files after they have been converted>
				move files to folder specified with -mf after conversion.
				meant to handle conversions delayed by -mc
					mux instance 1
						mux -p ~/_pre_proc -mc ~/_convert
					mux instance 2
						mux -w -p ~/_convert -mf ~/_proc
          -rel  -rel with -mc or -mf will preserve the portion of the path after the path specified with -rel
					file: /a/b/c/d/e/file.mkv -> ~/_convert/d/e/file.mkv
					run from /
						mux -p /a/b/ -r -mc ~/_convert -rel /a/b/c/
					run from /a/b/
						mux -r -mc ~/_convert -rel /a/b/c/
			-w	watch start path for new files. stay running and remux and convert files as they appear.
				recommend using with -mf
         -prob  -prob <path>
				move files to this folder if they fail during remux or convert
*/

var (
	argR, argP, argF, argW bool

	startPath  string
	singleFile string
	videoExts  = []string{
		".avi", ".divx", ".mpg", ".ts", ".wmv", ".mpeg", ".webm", ".xvid",
		".asf", ".vob", ".mkv", ".flv", ".mp4", ".m4v", ".m2ts", ".mts"}
	subtitleExts                                         = []string{".idx", ".sub", ".srt", ".ass", ".ssa"}
	allowedVideo                                         = []string{"h264", "hevc", "mpeg4"}
	allowedAudio                                         = []string{"aac", "ac3", "eac3", "flac", "alac", "dts", "mp3", "truehd"}
	keptLangs                                            = []string{"eng", "en", "und", "mis", ""}
	moveConvertPath, moveFinishedPath, relPath, probPath string
	moveConvert, moveFinished, moveRel, moveProb         bool
)

func getArgs() {
	var e error
	args := os.Args[1:]

	if specifyMoveConvert := arrayIdx(args, "-mc"); specifyMoveConvert != -1 {
		moveConvert = true
		if len(args) >= specifyMoveConvert+2 {
			moveConvertPath = args[specifyMoveConvert+1]
			moveConvertPath, e = filepath.Abs(moveConvertPath)
			chkFatal(e)
			st, e := os.Stat(moveConvertPath)
			if errors.Is(e, os.ErrNotExist) {
				fmt.Println("path specified with -mc does not exist.")
				os.Exit(1)
			}
			if !st.IsDir() {
				fmt.Println("path specified with -mc is not a folder.")
				os.Exit(1)
			}

		} else {
			fmt.Println("must specify path with -mc.")
			os.Exit(1)
		}
	}

	if specifyMoveFinished := arrayIdx(args, "-mf"); specifyMoveFinished != -1 {
		moveFinished = true
		if len(args) >= specifyMoveFinished+2 {
			moveFinishedPath = args[specifyMoveFinished+1]
			moveFinishedPath, e = filepath.Abs(moveFinishedPath)
			chkFatal(e)
			st, e := os.Stat(moveFinishedPath)
			if errors.Is(e, os.ErrNotExist) {
				fmt.Println("path specified with -mf does not exist.")
				os.Exit(1)
			}
			if !st.IsDir() {
				fmt.Println("path specified with -mf is not a folder.")
				os.Exit(1)
			}

		} else {
			fmt.Println("must specify path with -mf.")
			os.Exit(1)
		}
	}

	if specifyRel := arrayIdx(args, "-rel"); specifyRel != -1 {
		moveRel = true
		if len(args) >= specifyRel+2 {
			relPath = args[specifyRel+1]
			relPath, e = filepath.Abs(relPath)
			chkFatal(e)
			st, e := os.Stat(relPath)
			if errors.Is(e, os.ErrNotExist) {
				fmt.Println("path specified with -rel does not exist.")
				os.Exit(1)
			}
			if !st.IsDir() {
				fmt.Println("path specified with -rel is not a folder.")
				os.Exit(1)
			}

		} else {
			fmt.Println("must specify path with -rel.")
			os.Exit(1)
		}
	}

	if isAny("-r", args...) {
		argR = true
	}
	if isAny("-w", args...) {
		argW = true
	}
	if specifyP := arrayIdx(args, "-p"); specifyP != -1 {
		argP = true
		if len(args) >= specifyP+2 {
			startPath = args[specifyP+1]
			startPath, e = filepath.Abs(startPath)
			chkFatal(e)
			st, e := os.Stat(startPath)
			if errors.Is(e, os.ErrNotExist) {
				fmt.Println("path specified with -p does not exist.")
				os.Exit(1)
			}
			if !st.IsDir() {
				fmt.Println("path specified with -p is not a folder.")
				os.Exit(1)
			}

		} else {
			fmt.Println("must specify path with -p.")
			os.Exit(1)
		}
	} else {
		startPath, _ = os.Getwd()
	}

	if specifyF := arrayIdx(args, "-f"); specifyF != -1 {
		argF = true
		if argR || argP {
			p("-f is not compatible with -r or -p")
			os.Exit(1)
		}
		if len(args) >= specifyF+2 {
			singleFile = args[specifyF+1]
			singleFile, e = filepath.Abs(singleFile)
			chkFatal(e)
			st, e := os.Stat(singleFile)

			if errors.Is(e, os.ErrNotExist) {
				p("file specified with -f does not exist.")
				os.Exit(1)
			}
			if st.IsDir() {
				p("path specified with -f is a folder. use -p for folders.")
				os.Exit(1)
			}
			if !isVideo(singleFile) {
				p("file specified with -f is not a video.")
				os.Exit(1)
			}
		} else {
			p("must specify path with -f.")
			os.Exit(1)
		}
	}

	if specifyProb := arrayIdx(args, "-prob"); specifyProb != -1 {
		moveProb = true
		if len(args) >= specifyProb+2 {
			probPath = args[specifyProb+1]
			probPath, e = filepath.Abs(probPath)
			chkFatal(e)
			st, e := os.Stat(probPath)
			if errors.Is(e, os.ErrNotExist) {
				fmt.Println("path specified with -prob does not exist.")
				os.Exit(1)
			}
			if !st.IsDir() {
				fmt.Println("path specified with -prob is not a folder.")
				os.Exit(1)
			}

		} else {
			fmt.Println("must specify path with -prob.")
			os.Exit(1)
		}
	}
}

func isVideo(path string) bool {
	path = strings.ToLower(path)
	for _, ext := range videoExts {
		if strings.HasSuffix(path, ext) && !strings.HasSuffix(path, ".tmp.mkv") {
			return true
		}
	}
	return false
}
func isSub(path string) bool {
	path = strings.ToLower(path)
	for _, ext := range subtitleExts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

type Muxer struct {
	jobs []*Job
}

func (m *Muxer) start() {
	m.jobs = []*Job{}
	m.getJobs()
	m.doJobs()
	//if moveFinished {
	//	emp, _ := isDirEmpty(startPath)
	//	if !emp {
	//		p("finished, moving files from '%s' to '%s'", startPath, moveFinishedPath)
	//		mvTree(startPath, moveFinishedPath, true)
	//	}
	//}
}
func (m *Muxer) getJobs() {
	makeJob := func(vid string) {
		j := Job{
			video: vid,
		}
		j.filename = filepath.Base(j.video)
		j.basename = strings.TrimSuffix(j.filename, filepath.Ext(j.filename))
		j.ext = strings.ToLower(filepath.Ext(j.filename))
		if j.ext != ".mkv" {
			j.mux = true
		}
		j.baseWithPath = strings.TrimSuffix(vid, filepath.Ext(j.filename))
		j.tmpVideo = j.baseWithPath + ".tmp.mkv"
		j.finalVideo = j.baseWithPath + ".mkv"
		m.jobs = append(m.jobs, &j)
	}
	if argF {
		makeJob(singleFile)
	} else {
		if !argR {
			sp, e := os.Open(startPath)
			chkFatal(e)

			files, e := sp.ReadDir(-1)
			_ = sp.Close()
			chkFatal(e)
			for _, f := range files {
				if !f.IsDir() && isVideo(f.Name()) {
					makeJob(filepath.Join(startPath, f.Name()))
				}
			}
		} else {
			walk := func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && isVideo(p) {
					makeJob(p)
				}
				return nil
			}
			err := filepath.Walk(startPath, walk)
			chkFatal(err)
		}
	}

}
func (m *Muxer) doJobs() {
	for n, job := range m.jobs {
		if n > 0 {
			fmt.Println("--------------------------------------------------")
		}
		p("checking remux candidate: %s", job.video)
		if e := job.getStreams(); e != nil {
			p("failed to get streams for file: %s", job.video)
			p("got error: %s", e)
			if moveProb {
				p("FILE LIKELY CORRUPT, MOVING TO %s", probPath)
				job.move(probPath)
			} else {
				p("FILE LIKELY CORRUPT, SKIPPING")
			}
			continue
		}
		job.parseStreams()
		job.getExternalSubs()
		//job.printStreams()
		if job.convert && moveConvert {
			p("-mc is set, moving file to '%s' for conversion", moveConvertPath)
			job.move(moveConvertPath)
		} else if job.mux {
			job.convertStreams()
			job.buildCmdLine()
			job.runJob()
		}
	}
}

type Job struct {
	video, filename, basename, ext, baseWithPath string
	tmpVideo, finalVideo                         string
	subtitles, dotSubs                           []string
	elementaryStreams                            []string
	mux, convert                                 bool

	Streams             []*Stream `json:"streams"`
	vidStream           []*Stream
	audioEng, audioForn []*Stream
	subEng, subForced   []*Stream
	cmdLine             []string
}

func (j *Job) getStreams() error {
	output, e := exec.Command("ffprobe", "-v", "quiet",
		"-print_format", "json", "-show_streams", j.video).Output()
	if e != nil {
		return e
	}
	e = json.Unmarshal(output, j)
	if e != nil {
		return e
	}
	return nil
}
func (j *Job) parseStreams() {
	for n, s := range j.Streams {
		if s.CodecType == "video" && !isAny(s.CodecName, "mjpeg", "bmp", "png") {
			if !isAny(s.CodecName, allowedVideo...) {
				p("convert reason, video stream is '%s' ", s.CodecName)
				s.convert = true
				j.convert = true
				s.elementaryStream = fmt.Sprintf("%s.%d.h264", j.baseWithPath, n)
				j.mux = true
			}
			j.vidStream = append(j.vidStream, s)
		}
		if s.CodecType == "audio" {
			if !isAny(s.CodecName, allowedAudio...) {
				p("convert reason, audio stream is '%s' ", s.CodecName)
				s.convert = true
				j.convert = true
				s.elementaryStream = fmt.Sprintf("%s.%d.ac3", j.baseWithPath, n)
				j.mux = true
			}
			if isAny(s.Tags.Language, keptLangs...) {
				j.audioEng = append(j.audioEng, s)
			} else {
				s.foreignAudio = true
				j.audioForn = append(j.audioForn, s)
			}
		}
		if s.CodecType == "subtitle" {
			if isAny(s.Tags.Language, keptLangs...) {
				if s.CodecName == "mov_text" {
					p("convert reason, subtitle stream is '%s' ", s.CodecName)
					s.convert = true
					j.convert = true
					s.elementaryStream = fmt.Sprintf("%s.%d.srt", j.baseWithPath, n)
					j.mux = true
				}
				if s.Disposition.Forced == 1 {
					j.subForced = append(j.subForced, s)
				} else {
					j.subEng = append(j.subEng, s)
				}
			}
		}
	}
	offset := len(j.vidStream)
	for n, aud := range j.audioEng {
		aud.newIndex = offset + n
		if aud.Index == 0 {
			p("remux reason, video was not first stream")
			j.mux = true
		} else if aud.newIndex != aud.Index {
			p("remux reason, english audio was not first: move stream %d to %d", aud.Index, aud.newIndex)
			j.mux = true
		}
	}

	offset = offset + len(j.audioEng)
	for n, aud := range j.audioForn {
		aud.newIndex = offset + n
		if aud.newIndex != aud.Index && len(j.audioEng) == 0 {
			p("remux reason, audio order changed: move stream %d to %d", aud.Index, aud.newIndex)
			j.mux = true
		}
	}
}
func (j *Job) convertStreams() {
	var cmd []string
	add := func(str ...string) {
		for _, s := range str {
			cmd = append(cmd, s)
		}
	}
	for _, s := range j.Streams {
		if s.convert {
			cmd = []string{"ffmpeg", "-hide_banner", "-loglevel", "warning", "-stats", "-y", "-i", j.video, "-map", fmt.Sprintf("0:%d", s.Index)}

			if s.CodecType == "video" {
				if !isAny(s.FieldOrder, "progressive", "unknown", "") {
					add("-vf", "yadif")
				}
				add("-c:v", "h264", "-preset", "slow", "-crf", "17", "-movflags", "+faststart", "-pix_fmt", "yuv420p", s.elementaryStream)
			}
			if s.CodecType == "audio" {
				add("-c:a", "ac3", s.elementaryStream)
			}
			if s.foreignAudio && len(j.audioEng) > 0 {
				s.convert = false
				continue
			}

			if s.CodecType == "subtitle" {
				if s.CodecName == "mov_text" {
					add("-c:s", "text", s.elementaryStream)
				}
			}

			p("creating elementary stream: %s", s.elementaryStream)
			j.elementaryStreams = append(j.elementaryStreams, s.elementaryStream)
			printCmd(cmd)
			err := run(cmd...)
			chkFatal(err)
		}
	}
}
func (j *Job) buildCmdLine() {
	cmd := []string{"mkvmerge", "-o", j.tmpVideo}
	add := func(str ...string) {
		for _, s := range str {
			cmd = append(cmd, s)
		}
	}
	for _, s := range j.vidStream {
		if s.convert {
			add(s.elementaryStream)
		} else {
			add("-A", "-S", "-d", fmt.Sprintf("%d", s.Index), j.video)
		}
	}
	for _, s := range j.audioEng {
		if s.convert {
			add(s.elementaryStream)
		} else {
			add("-S", "-D", "-a", fmt.Sprintf("%d", s.Index), j.video)
		}
	}
	if len(j.audioEng) == 0 {
		for _, s := range j.audioForn {
			if s.convert {
				add(s.elementaryStream)
			} else {
				add("-S", "-D", "-a", fmt.Sprintf("%d", s.Index), j.video)
			}
		}
	}

	var subIndexes []string
	for _, s := range j.subForced {
		if !s.convert {
			subIndexes = append(subIndexes, fmt.Sprintf("%d", s.Index))
		} else {
			add(s.elementaryStream)
		}
	}
	for _, s := range j.subEng {
		if !s.convert {
			subIndexes = append(subIndexes, fmt.Sprintf("%d", s.Index))
		} else {
			add(s.elementaryStream)
		}
	}
	if len(subIndexes) > 0 {
		add("-D", "-A", "-s", strings.Join(subIndexes, ","), j.video)
	}
	add(j.subtitles...)
	j.cmdLine = cmd
}

func (j *Job) getExternalSubs() {
	src := strings.ToLower(j.basename)
	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && isSub(path) {
			subFname := filepath.Base(strings.ToLower(path))
			if strings.HasPrefix(subFname, src) {
				j.mux = true
				p("found sub: %s", path)
				if strings.HasSuffix(strings.ToLower(path), ".idx") {
					p("verifying idx file")
					if cleanIdx(path) {
						j.subtitles = append(j.subtitles, path)
					} else {
						p("idx failed verification")
					}
				} else if strings.HasSuffix(strings.ToLower(path), ".sub") {
					j.dotSubs = append(j.dotSubs, path)
				} else {
					j.subtitles = append(j.subtitles, path)
				}
			}
		}
		return nil
	}
	d := filepath.Dir(j.video)
	err := filepath.Walk(d, walk)
	chkFatal(err)
}
func (j *Job) printStreams() {
	p("vidStream")
	for _, s := range j.vidStream {
		fmt.Printf("%+v\n", s)
	}
	p("audioEng")
	for _, s := range j.audioEng {
		fmt.Printf("%+v\n", s)
	}
	p("audioForn")
	for _, s := range j.audioForn {
		fmt.Printf("%+v\n", s)
	}
	p("subForced")
	for _, s := range j.subForced {
		fmt.Printf("%+v\n", s)
	}
	p("subEng")
	for _, s := range j.subEng {
		fmt.Printf("%+v\n", s)
	}
	p("subtitles")
	for _, s := range j.subtitles {
		fmt.Println(s)
	}
}

func (j *Job) runJob() {
	if len(j.cmdLine) == 0 {
		return
	}
	printCmd(j.cmdLine)
	err := run(j.cmdLine...)
	chk(err)
	if err == nil {
		p("removing file '%s'", j.video)
		err = os.Remove(j.video)
		chk(err)
		if err == nil {
			p("renaming '%s' to '%s'", j.tmpVideo, j.finalVideo)
			err = os.Rename(j.tmpVideo, j.finalVideo)
			chk(err)
			if err == nil {
				if moveFinished {
					var dst string
					if moveRel {
						dst = strings.Replace(j.finalVideo, relPath, moveFinishedPath, 1)
					} else {
						dst = strings.Replace(j.finalVideo, startPath, moveFinishedPath, 1)
					}
					err = mvFile(j.finalVideo, dst)
					chk(err)
				}

				for _, s := range j.subtitles {
					p("removing subtitle file '%s'", s)
					err = os.Remove(s)
					chk(err)
					if strings.HasSuffix(s, ".idx") {
						s = strings.TrimSuffix(s, ".idx") + ".sub"
						p("removing subtitle file '%s'", s)
						err = os.Remove(s)
						chk(err)
					}
				}
			}
		}
	} else {
		p("remux failed for '%s'", j.video)
		_, err = os.Stat(j.tmpVideo)
		if !errors.Is(err, os.ErrNotExist) {
			p("removing temp file %s", j.tmpVideo)
			err = os.Remove(j.tmpVideo)
			chk(err)
		}
		if moveProb {
			p("moving %s -> %s", j.video, probPath)
			j.move(probPath)
		}
	}
	for _, s := range j.elementaryStreams {
		p("removing temporary elementary stream: %s", s)
		e := os.Remove(s)
		chk(e)
	}
	rmEmptyFolders(startPath)

}

func (j *Job) move(path string) {
	files := append(j.subtitles, j.video)
	files = append(files, j.dotSubs...)
	for _, f := range files {
		var newPath string
		if moveRel {
			newPath = strings.Replace(f, relPath, path, 1)
		} else {
			newPath = strings.Replace(f, startPath, path, 1)
		}

		e := mvFile(f, newPath)
		chkFatal(e)
	}
	rmEmptyFolders(startPath)
}

func cleanIdx(f string) bool {
	idxB, e := os.ReadFile(f)
	if e != nil {
		chk(e)
		return false
	}
	var outIdx []string
	idxIn := string(idxB)
	idxLines := strings.Split(idxIn, "\n")
	found := false
	for _, line := range idxLines {
		if strings.HasPrefix(line, "id: --") {
			line = strings.Replace(line, "id: --", "id: en", 1)
			found = true
		}
		outIdx = append(outIdx, line)
	}
	if !found {
		return true
	}
	newIdx := strings.Join(outIdx, "\n") + "\n"
	e = os.WriteFile(f, []byte(newIdx), 0666)
	if e != nil {
		chk(e)
		return false
	}
	return true
}

type Stream struct {
	Index                 int `json:"index"`
	newIndex              int
	convert, foreignAudio bool
	elementaryStream      string
	CodecName             string         `json:"codec_name"`
	CodecType             string         `json:"codec_type"`
	FieldOrder            string         `json:"field_order"`
	Disposition           DispositionMap `json:"disposition"`
	Tags                  TagsMap        `json:"tags"`
}
type DispositionMap struct {
	Default int `json:"default"`
	Forced  int `json:"forced"`
}
type TagsMap struct {
	Language string `json:"language"`
}

func main() {
	getArgs()
	//viewArgs()
	var m Muxer
	if argW {
		p("starting watcher. scanning for new files every 60 seconds")
		for {
			m.start()
			time.Sleep(60 * time.Second)
		}
	} else {
		m.start()
	}
}

var (
	p              = base.P
	chk            = base.Chk
	chkFatal       = base.ChkFatal
	isAny          = base.IsAny
	arrayIdx       = base.ArrayIdx
	run            = base.Run
	rmEmptyFolders = base.RmEmptyFolders
	printCmd       = base.PrintCmd
	mvFile         = base.MvFile
)
