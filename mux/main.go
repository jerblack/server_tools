package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jerblack/base"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
	-------------------
	remux or convert all files into single mkv with known compatible video and audio streams and no external subtitles.
		requires: ffmpeg ffprobe mkvtoolnix
		remux reasons
			non-mkv container
			external subtitle files
			stream types out of order: video -> audio -> subtitles
			video stream not first strewm
			subtitles out of order
				forced, unforced
		convert reasons
			audio not in one of the following formats
				"aac", "ac3", "eac3", "flac", "alac", "dts", "mp3", "truehd"
            video not in one of the following formats
				"h264", "hevc", "mpeg4"
			subtitle in mov_text format
		automatic problem handlers
			all text subtitles converted to UTF-8 text encoding.
			idx/sub subtitle
				Warning: Unknown header [subtitle unusable, idx/sub moved to recycle]
				No sub found for idx [subtitle unusable, idx moved to recycle]
				No language ID set [id in idx file set to en]
			srt subtitle
				Warning: The start timestamp is smaller than that of the previous entry [srt file sorted by timestamp]
			audio errors:
				track contains X bytes of invalid data
				No AC-3 header found in first frame
					[All audio tracks demuxed from video stream and rewritten with corrupted portions of audio removed]
			interlaced video
				video deinterlaced during conversion with yadif video filter

		optional external convert path
			in scenarios where long conversion process would block other processing, an option is provided to move files
			to a separate convert folder for a separate mux instance to convert
			if -mc specified with path to convert folder, any files that need conversion will be moved to that folder
			for later conversion
			separate process should run mux -p <convert folder> -mf <finished file folder> will convert any files in
			convert folder and move them to the finished file folder on completion
		options
			-h	print help
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
	startPath        string
	singleFile       string
	moveConvertPath  string
	moveFinishedPath string
	relPath          string
	probPath         string
	recyclePath      string

	moveConvert  bool
	moveFinished bool
	moveRel      bool
	moveProb     bool
	useRecycle   bool
	force        bool

	exitOnError bool

	argR, argP, argF, argW bool

	videoExts = []string{
		".avi", ".divx", ".mpg", ".ts", ".wmv", ".mpeg", ".webm", ".xvid",
		".asf", ".vob", ".mkv", ".flv", ".mp4", ".m4v", ".m2ts", ".mts",
	}
	subtitleExts = []string{".idx", ".srt", ".ass", ".ssa"}
	allowedVideo = []string{"h264", "hevc", "mpeg4"}
	allowedAudio = []string{"aac", "ac3", "eac3", "flac", "alac", "dts", "mp3", "truehd"}
	engLangs     = []string{"eng", "en", "und", "mis", ""}
)
var help = ` mux options:
  -h	print help
  -p 	-p <start path>
        start path. mux all files in folder.
        if not specified, start path is current working directory (where mux was started from)
  -f    -f <path to file>
        mux single file
  -r    mux all files in start path recursively
  -mc   -mc <path to move files that need conversion>
        move all files in jobs that need conversion to path specified with -mc instead of converting
  -mf   -mf <path to move files after they have been converted>
        move output file to folder specified with -mf after conversion.
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
  -xe   exit on error. if unable to complete job, exit instead of proceeding with queue processing
  -prob -prob <path>
        move all files in job to this folder if there is a failure during remux or convert
  -recycle
        -recycle <path to move files instead of deleting them>
        If specified, files will be moved to this folder instead of being deleted`

func getArgs() {

	var e error
	args := os.Args[1:]
	if isAny("-h", args...) {
		fmt.Println(help)
		os.Exit(0)
	}

	recyclePath = os.Getenv("RECYCLE")
	if recyclePath != "" {
		useRecycle = true
	}

	if specifyRecycle := arrayIdx(args, "-recycle"); specifyRecycle != -1 {
		useRecycle = true
		if len(args) >= specifyRecycle+2 {
			recyclePath = args[specifyRecycle+1]
			recyclePath, e = filepath.Abs(recyclePath)
			chkFatal(e)
			st, e := os.Stat(recyclePath)
			if errors.Is(e, os.ErrNotExist) {
				e = os.MkdirAll(recyclePath, 0644)
				if e != nil {
					chk(e)
					fmt.Println("path specified with -recycle is not valid (doesn't exist and can't be created).")
					os.Exit(1)
				}
			}
			if !st.IsDir() {
				fmt.Println("path specified with -recycle is not a folder.")
				os.Exit(1)
			}
		} else {
			fmt.Println("must specify path with -recycle.")
			os.Exit(1)
		}
	}

	if specifyMoveConvert := arrayIdx(args, "-mc"); specifyMoveConvert != -1 {
		moveConvert = true
		if len(args) >= specifyMoveConvert+2 {
			moveConvertPath = args[specifyMoveConvert+1]
			moveConvertPath, e = filepath.Abs(moveConvertPath)
			chkFatal(e)
			st, e := os.Stat(moveConvertPath)
			if errors.Is(e, os.ErrNotExist) {
				e = os.MkdirAll(moveConvertPath, 0644)
				if e != nil {
					chk(e)
					fmt.Println("path specified with -mc is not valid (doesn't exist and can't be created).")
					os.Exit(1)
				}
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
				e = os.MkdirAll(moveFinishedPath, 0644)
				if e != nil {
					chk(e)
					fmt.Println("path specified with -mf is not valid (doesn't exist and can't be created).")
					os.Exit(1)
				}
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

	if isAny("-force", args...) {
		force = true
	}
	if isAny("-xe", args...) {
		exitOnError = true
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
				e = os.MkdirAll(probPath, 0644)
				if e != nil {
					chk(e)
					fmt.Println("path specified with -prob is not valid (doesn't exist and can't be created).")
					os.Exit(1)
				}
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
		job.start()
	}
}

type Job struct {
	video        string //   /x/a/b/c/file.ext
	filename     string //   /x/a/b/c/file.ext -> file.ext
	basename     string //   /x/a/b/c/file.ext -> file
	ext          string //   /x/a/b/c/file.ext -> .ext
	baseWithPath string //   /x/a/b/c/file.ext -> /x/a/b/c/file
	tmpVideo     string //   /x/a/b/c/file.ext -> /x/a/b/c/file.tmp.mkv
	finalVideo   string //   /x/a/b/c/file.ext -> /x/a/b/c/file.mkv
	mux          bool   //	remux required for job
	convert      bool   //	convert required for job
	restarted    bool   //   job has been restarted
	reStream     bool   //   job is restarted and needs streams refreshed
	failed       bool   //  mark job failed for -xe exit on error

	streams         []*Stream //	 all streams found for job, internal and external
	vidStream       []*Stream //  video stream in primary main file
	audioStream     []*Stream //  internal audio streams
	subStream       []*Stream //  internal subtitle streams
	subStreamForced []*Stream //  internal subtitle streams with forced attribute set

	cmdLine []string
}

func (j *Job) start() {
	var e error
	if !j.restarted || j.reStream {
		e, j.streams = j.getStreams(j.video)
		if e != nil {
			p("failed to get streams for file: %s", j.video)

			p("got error: %s", e)
			//if !j.reStream && strings.HasSuffix(strings.ToLower(j.video), ".mkv") {
			//	p("fix needed: remux with ffmpeg")
			//	e = j.remuxWithFfmpeg()
			//	if e != nil {
			//		chk(e)
			//		j.failed = true
			//	} else {
			//		j.restarted = true
			//		j.start()
			//		return
			//	}
			//}

			if moveProb {
				p("FILE LIKELY CORRUPT, MOVING TO %s", probPath)
				j.move(probPath)
			} else {
				p("FILE LIKELY CORRUPT, SKIPPING")
			}
			if exitOnError {
				p("job failed and -xe set. exiting")
				os.Exit(1)
			}
			return
		}
		j.streams = append(j.streams, j.findExternalSubs()...)
	}

	j.parseStreams()
	//j.printStreams()

	if j.convert && moveConvert {
		p("-mc is set, moving file to '%s' for conversion", moveConvertPath)
		j.move(moveConvertPath)
	} else if j.mux || force {
		j.convertStreams()
		j.buildCmdLine()
		j.runJob()
	} else if moveFinished {
		p("-mf is set, moving file to '%s'", moveFinishedPath)
		j.move(moveFinishedPath)
	}
	if j.failed && exitOnError {
		p("job failed and -xe set. exiting")
		os.Exit(1)
	}
}

func (j *Job) findExternalSubs() []*Stream {
	src := strings.ToLower(j.basename)
	var subStreams []*Stream

	walk := func(path string, info os.FileInfo, err error) error {
		if !fileExists(path) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() && isSub(path) {
			subFname := filepath.Base(strings.ToLower(path))
			if strings.HasPrefix(subFname, src) {
				p("found sub: %s", path)
				e, subFile := j.validateSub(path)
				if e != nil {
					p("subtitle failed validation, skipping file %s", path)
					return nil
				}

				e, streams := j.getStreams(path)
				if e != nil {
					p("could not get streams from subtitle file: %s", path)
					return nil
				}
				s := ""
				if len(streams) != 1 {
					s = "s"
				}
				p("found %d stream%s in %s", len(streams), s, path)

				for _, stream := range streams {
					stream.elementaryStream = path
					stream.subFile = subFile
					subStreams = append(subStreams, stream)
				}

				j.mux = true
			}
		}
		return nil
	}
	d := filepath.Dir(j.video)
	err := filepath.Walk(d, walk)
	chkFatal(err)
	return subStreams
}
func (j *Job) validateSub(path string) (error, string) {
	var subFile string

	reIdx := regexp.MustCompile(`(?i).idx$`)
	reSrt := regexp.MustCompile(`(?i).srt$`)

	p("ensuring external subtitle text encoding is UTF-8")
	convertTextUtf8(path)

	if reIdx.MatchString(path) {
		p("ensuring idx has language id set")
		if !checkIdxNoId(path) {
			p("idx failed verification")
			removeIdxSub(path)
			return errors.New("idx failed verification"), ""
		}
		p("running idx warning checks")
		e := checkUnusableIdx(path)
		if e != nil {
			errText := fmt.Sprintf("idx failed validation with error: %s", e.Error())
			p(errText)
			removeIdxSub(path)
			return errors.New(errText), ""
		}
		e, subFile = findIdxSub(path)
		if e != nil {
			errText := fmt.Sprintf("no matching sub file found for idx: %s", path)
			p(errText)
			removeFile(path)
			return errors.New(errText), ""
		}
	}
	if reSrt.MatchString(path) {
		e := checkSortedSrt(path)
		if e != nil {
			return e, ""
		}
	}
	return nil, subFile
}
func (j *Job) getStreams(path string) (error, []*Stream) {
	var ffStreams FfprobeStreams
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", path)
	output, e := cmd.Output()
	if e != nil {
		return e, []*Stream{}
	}
	e = json.Unmarshal(output, &ffStreams)
	var streams []*Stream
	for n, _ := range ffStreams.Streams {
		// "Error: 'cmn' is neither a valid ISO 639-2 nor a valid ISO 639-1 code. See 'mkvmerge --list-languages'
		// for a list of all languages and their respective ISO 639-2 codes."
		if isAny(ffStreams.Streams[n].Tags.Language, "cmn", "yue") {
			ffStreams.Streams[n].Tags.Language = "chi"
		}
		streams = append(streams, &ffStreams.Streams[n])
	}
	return nil, streams
}
func (j *Job) parseStreams() {
	j.vidStream = []*Stream{}
	j.audioStream = []*Stream{}
	j.subStream = []*Stream{}
	j.subStreamForced = []*Stream{}

	for n, s := range j.streams {
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
			j.audioStream = append(j.audioStream, s)
		}
		if s.CodecType == "subtitle" {
			if s.CodecName == "mov_text" {
				p("convert reason, subtitle stream is '%s' ", s.CodecName)
				s.convert = true
				j.convert = true
				s.elementaryStream = fmt.Sprintf("%s.%d.srt", j.baseWithPath, n)
				j.mux = true
			}
			if s.Disposition.Forced == 1 {
				j.subStreamForced = append(j.subStreamForced, s)
			} else {
				j.subStream = append(j.subStream, s)
			}
		}
	}

	var allStreams []*Stream
	for _, streams := range [][]*Stream{j.vidStream, j.audioStream, j.subStreamForced, j.subStream} {
		allStreams = append(allStreams, streams...)
	}

	for n, stream := range allStreams {
		if stream.elementaryStream != "" {
			p("remux reason, stream(s) in external file: %s", stream.elementaryStream)
			j.mux = true
		} else if stream.Index != n {
			p("remux reason, %s stream moved from index %d to %d.",
				stream.CodecType, stream.Index, n)
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
	for _, s := range j.streams {
		if s.convert && !s.converted {
			cmd = []string{"ffmpeg", "-hide_banner", "-loglevel", "warning", "-stats", "-y",
				"-i", j.video, "-map", fmt.Sprintf("0:%d", s.Index)}

			if s.CodecType == "video" {
				if !isAny(s.FieldOrder, "progressive", "unknown", "") {
					add("-vf", "yadif")
				}
				if s.Height%2 != 0 || s.Width%2 != 0 {
					x := math.Ceil(float64(s.Width)/2) * 2
					y := math.Ceil(float64(s.Height)/2) * 2
					add("-vf", fmt.Sprintf("pad=%d:%d", int(x), int(y)))
				}
				add("-c:v", "h264", "-preset", "slow", "-crf", "17", "-movflags", "+faststart", "-pix_fmt",
					"yuv420p", s.elementaryStream)
			}
			if s.CodecType == "audio" {
				sampleRate, _ := strconv.ParseInt(s.SampleRate, 10, 64)
				if sampleRate < 44100 {
					add("-c:a", "ac3", s.elementaryStream)
				} else {
					add("-c:a", "eac3", s.elementaryStream)
				}
			}
			if s.CodecType == "subtitle" {
				if s.CodecName == "mov_text" {
					add("-c:s", "text", s.elementaryStream)
				}
			}

			p("creating elementary stream: %s", s.elementaryStream)
			printCmd(cmd)
			err := run(cmd...)
			chkFatal(err)
			s.converted = true
		}
	}
}
func (j *Job) buildCmdLine() {
	cmd := []string{"mkvmerge", "--abort-on-warnings", "-o", j.tmpVideo}
	add := func(str ...string) {
		for _, s := range str {
			cmd = append(cmd, s)
		}
	}
	for _, s := range j.vidStream {
		if s.elementaryStream != "" {
			add(s.elementaryStream)
		} else {
			add("-A", "-S", "-d", fmt.Sprintf("%d", s.Index), j.video)
		}
	}

	for _, s := range j.audioStream {
		if s.elementaryStream != "" {
			if s.Tags.Language == "" {
				add(s.elementaryStream)
			} else {
				add("--language", fmt.Sprintf("0:%s", s.Tags.Language), s.elementaryStream)
			}
		} else {
			add("-S", "-D", "-a", fmt.Sprintf("%d", s.Index), j.video)
		}
	}
	allSubs := append(j.subStreamForced, j.subStreamForced...)

	for _, s := range allSubs {
		if s.elementaryStream != "" {
			if s.Tags.Language == "" {
				add(s.elementaryStream)
			} else {
				add("--language", fmt.Sprintf("0:%s", s.Tags.Language), s.elementaryStream)
			}
		} else {
			add("-D", "-A", "-s", fmt.Sprintf("%d", s.Index), j.video)
		}
	}
	j.cmdLine = cmd
}
func (j *Job) printStreams() {
	p("/////////////////////////")
	p("vidStream")
	for _, s := range j.vidStream {
		fmt.Printf("%+v\n", s)
	}
	p("audioStream")
	for _, s := range j.audioStream {
		fmt.Printf("%+v\n", s)
	}
	p("subStreamForced")
	for _, s := range j.subStreamForced {
		fmt.Printf("%+v\n", s)
	}
	p("subStream")
	for _, s := range j.subStream {
		fmt.Printf("%+v\n", s)
	}
	p(`\\\\\\\\\\\\\\\\\\\\\\\\\`)
}
func (j *Job) extractSubs() {
	// s.codec_name =  idx -> dvd_subtitle, ass/ssa -> ass, srt -> subrip
	exts := map[string]string{
		"subrip": ".srt", "dvd_subtitle": ".idx", "ass": ".ssa",
	}
	for _, stream := range j.streams {
		if stream.CodecType == "subtitle" && stream.elementaryStream == "" {
			ext := exts[stream.CodecName]
			elm := fmt.Sprintf("%s.%d%s", j.baseWithPath, stream.Index, ext)
			cmd := exec.Command("ffmpeg", "-i", j.video, "-map", fmt.Sprintf("0:%d", stream.Index),
				"-c:s", stream.CodecName, elm)
			e := cmd.Run()
			chk(e)
			if e == nil {
				e, subName := j.validateSub(elm)
				if e != nil {
					p("subtitle failed validation, skipping file %s", elm)
					removeFile(elm)
					if subName != "" {
						removeFile(subName)
					}
				} else {
					stream.elementaryStream = elm
					stream.subFile = subName
				}
			}
		}
	}
}
func (j *Job) extractAudio(recode bool) {
	for _, stream := range j.streams {
		if stream.CodecType == "audio" {
			stream.elementaryStream = fmt.Sprintf("%s.%d.%s", j.baseWithPath, stream.Index, stream.CodecName)
			p("extracting audio stream %s", stream.elementaryStream)
			codec := "copy"
			if recode {
				codec = stream.CodecName
			}
			args := []string{"-fflags", "discardcorrupt", "-i", j.video, "-map",
				fmt.Sprintf("0:%d", stream.Index), "-c:a", codec}
			args = append(args, stream.elementaryStream)
			fmt.Println(strings.Join(args, " "))
			cmd := exec.Command("ffmpeg", args...)
			_ = cmd.Run()
		}
	}
}
func (j *Job) rewriteMkvContainer() error {
	cmd := exec.Command("mkvmerge", "-o", j.tmpVideo, j.video)
	_ = cmd.Run()
	e := removeFile(j.video)
	if e != nil {
		return e
	}
	e = os.Rename(j.tmpVideo, j.video)
	return e
}
func (j *Job) remuxWithFfmpeg() error {
	tmpVid := filepath.Join(filepath.Dir(j.video), "tmp."+filepath.Base(j.video))
	cmd := exec.Command("ffmpeg", "-i", j.video, "-avoid_negative_ts", "1", "-codec", "copy", tmpVid)
	_ = cmd.Run()
	if !fileExists(tmpVid) {
		return fmt.Errorf("failed to remux file with ffmpeg: %s", j.video)
	}
	e := removeFile(j.video)
	if e != nil {
		return e
	}
	e = os.Rename(tmpVid, j.video)
	j.reStream = true
	return e
}

func (j *Job) runJob() {
	if len(j.cmdLine) == 0 {
		return
	}
	var restart bool
	printCmd(j.cmdLine)
	w := runWarning(j.cmdLine, true)

	if w == nil {
		p("removing file '%s'", j.video)
		err := removeFile(j.video)
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
					if err != nil {
						j.failed = true
					}
				}
				for _, s := range j.streams {
					if s.elementaryStream != "" && fileExists(s.elementaryStream) {
						p("removing temporary elementary stream: %s", s.elementaryStream)
						e := removeFile(s.elementaryStream)
						chk(e)
						if err != nil {
							j.failed = true
						}
					}
					if s.subFile != "" && fileExists(s.subFile) {
						p("removing temporary elementary stream: %s", s.subFile)
						e := removeFile(s.subFile)
						chk(e)
						if err != nil {
							j.failed = true
						}
					}
				}
			} else {
				j.failed = true
			}
		}
	} else {
		p("remux failed for '%s'", j.video)

		trackRequestedNotFound := regexp.MustCompile(`A track with the ID \d+ was requested but not found in the file. The corresponding option will be ignored.`)
		if trackRequestedNotFound.MatchString(w.warning) && isVideo(w.filename) {
			p("fix needed: remux with ffmpeg")
			e := j.remuxWithFfmpeg()
			if e == nil {
				restart = true
			} else {
				chk(e)
			}
		}

		qtReaderNoChunk := regexp.MustCompile(`Quicktime/MP4 reader: Could not read chunk number \d+/\d+ with size \d+ from position \d+. Aborting.`)
		if qtReaderNoChunk.MatchString(w.warning) {
			p("failed to read corrupt video file: %s", j.video)
			j.failed = true
		}

		noHeaderAtoms := "Have not found any header atoms"
		if strings.Contains(w.warning, noHeaderAtoms) {
			p("fix needed: remux with ffmpeg")
			e := j.remuxWithFfmpeg()
			if e == nil {
				restart = true
			} else {
				chk(e)
			}
		}

		matroskaFileStructure := "Error in the Matroska file structure at position"
		if strings.Contains(w.warning, matroskaFileStructure) {
			p("fix needed: rewrite mkv container")
			e := j.rewriteMkvContainer()
			if e == nil {
				restart = true
			} else {
				chk(e)
			}
		}

		invalidChars := "text subtitle track contains invalid 8-bit characters"
		if strings.Contains(w.warning, invalidChars) && isVideo(w.filename) {
			p("fix needed: extract all internal subtitles")
			j.extractSubs()
			restart = true
		}

		audioInvalidData := regexp.MustCompile(`audio track contains \d+ bytes of invalid data`)
		if audioInvalidData.MatchString(w.warning) && isVideo(w.filename) {
			p("fix needed: extract all internal audio streams")
			j.extractAudio(true)
			restart = true
		}

		noAc3Header := "No AC-3 header found in first frame"
		if strings.Contains(w.warning, noAc3Header) && isVideo(w.filename) {
			p("fix needed: extract all internal audio streams")
			j.extractAudio(true)
			restart = true
		}

		_, err := os.Stat(j.tmpVideo)
		if !errors.Is(err, os.ErrNotExist) {
			p("removing temp file %s", j.tmpVideo)
			e := removeFile(j.tmpVideo)
			chkFatal(e)

		}
		if !restart {
			j.failed = true
		}
		if moveProb && !restart {
			p("moving %s -> %s", j.video, probPath)
			j.move(probPath)
		}
	}

	if restart {
		p("encountered addressable error. restarting job.")
		j.restarted = true
		j.start()
	} else {
		rmEmptyFolders(startPath)
	}

}
func (j *Job) move(path string) {
	files := []string{j.video}
	for _, s := range j.streams {
		files = append(files, s.elementaryStream)
	}
	for _, f := range files {
		if !fileExists(f) {
			continue
		}
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

func checkSortedSrt(srt string) error {
	ext := filepath.Ext(srt)
	if strings.ToLower(ext) != ".srt" {
		return nil
	}
	p("ensuring srt is sorted by timestamp")
	tmp := strings.TrimSuffix(srt, ext) + ".tmp.srt"

	cmd := exec.Command("mkvmerge", "-o", tmp, srt)
	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	done := make(chan struct{})
	scanner := bufio.NewScanner(r)
	var errText string
	var timestampProblem bool
	re := regexp.MustCompile(`Warning: '.*': (.*)`)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Warning:") {
				warning := re.ReplaceAllString(line, "$1")
				if strings.Contains(warning, "The start timestamp is smaller than that of the previous entry.") {
					timestampProblem = true
				} else {
					errText = warning
				}
			}
		}
		done <- struct{}{}
	}()
	_ = cmd.Start()
	<-done
	_ = cmd.Wait()

	if timestampProblem {
		p("srt was not sorted by timestamp. fixing.")
		if fileExists(srt) {
			removeFile(srt)
		}

		e := os.Rename(tmp, srt)
		chkFatal(e)
	} else {
		if fileExists(tmp) {
			removeFile(tmp)
		}
	}

	if errText != "" {
		return errors.New(errText)
	} else {
		return nil
	}

}
func checkUnusableIdx(f string) error {
	cmd := exec.Command("mkvmerge", "--abort-on-warnings", "-o", "/dev/null", f)
	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	done := make(chan struct{})
	scanner := bufio.NewScanner(r)
	var errText string
	re := regexp.MustCompile(`Warning: '.*': (.*)`)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Warning:") {
				warning := re.ReplaceAllString(line, "$1")
				if strings.Contains(warning, "Unknown header") {
					errText = warning
				}
			}
		}
		done <- struct{}{}
	}()
	_ = cmd.Start()
	<-done
	_ = cmd.Wait()
	if errText != "" {
		return errors.New(errText)
	} else {
		return nil
	}
}
func checkIdxNoId(f string) bool {
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
func findIdxSub(idx string) (error, string) {
	// find companion sub for idx file
	reIdx := regexp.MustCompile(`(?i)(.idx$)`)
	reSub := regexp.MustCompile(`(?i)(.sub$)`)
	idxBase := reIdx.ReplaceAllString(filepath.Base(idx), "")
	d, _ := os.Open(filepath.Dir(idx))
	defer d.Close()
	files, _ := d.ReadDir(0)
	for _, file := range files {
		if reSub.MatchString(file.Name()) && idxBase == reSub.ReplaceAllString(file.Name(), "") {
			subName := filepath.Join(filepath.Dir(idx), file.Name())
			return nil, subName
		}
	}
	return errors.New(".sub file not found"), ""
}
func removeIdxSub(idx string) {
	e, sub := findIdxSub(idx)
	if e != nil {
		p("removing broken sub: %s", sub)
		removeFile(sub)
	}
	p("removing broken sub: %s", idx)
	removeFile(idx)
}
func convertTextUtf8(f string) {
	st, e := os.Stat(f)
	chkFatal(e)
	b, e := os.ReadFile(f)
	chkFatal(e)
	c, _, _ := charset.DetermineEncoding(b, "")
	out, _, e := transform.Bytes(c.NewDecoder(), b)
	chkFatal(e)
	e = os.WriteFile(f, out, st.Mode())
	chkFatal(e)
}
func removeFile(f string) error {
	// expects full path
	dir := filepath.Dir(f)
	if recyclePath != "" {
		dst := strings.Replace(f, dir, recyclePath, 1)
		//p("moving file %s -> %s", f, dst)
		return mvFile(f, dst)

	} else {
		//p("deleting file %s", f)
		return os.Remove(f)
	}

}

type FfprobeStreams struct {
	Streams []Stream `json:"streams"`
}
type Stream struct {
	Index            int `json:"index"`
	convert          bool
	converted        bool
	subFile          string
	elementaryStream string
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	CodecName        string `json:"codec_name"`
	CodecType        string `json:"codec_type"`
	FieldOrder       string `json:"field_order"`
	SampleRate       string `json:"sample_rate"`
	Disposition      struct {
		Default int `json:"default"`
		Forced  int `json:"forced"`
	} `json:"disposition"`
	Tags struct {
		Language string `json:"language"`
	} `json:"tags"`
}

func main() {
	getArgs()
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
	arrayIdx       = base.ArrayIdx
	run            = base.Run
	rmEmptyFolders = base.RmEmptyFolders
	printCmd       = base.PrintCmd
	fileExists     = base.FileExists
	isAny          = base.IsAny
	mvFile         = base.MvFile
)

type Warning struct {
	filename string
	track    int
	warning  string
}

func runWarning(cmdLine []string, showStdout bool) *Warning {
	cmd := exec.Command(cmdLine[0], cmdLine[1:]...)
	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	done := make(chan Warning)
	scanner := bufio.NewScanner(r)
	reNoTrack := regexp.MustCompile(`Warning: '(.+)': (.+)`)
	reTrack := regexp.MustCompile(`Warning: '(.+)' track (\d+): (.+)`)
	reNoFile := regexp.MustCompile(`Warning: (.+)`)
	reError := regexp.MustCompile(`Error: (.+)`)

	go func() {
		var w Warning
		for scanner.Scan() {
			line := scanner.Text()
			if showStdout {
				fmt.Println(line)
			}
			if reNoTrack.MatchString(line) {
				matches := reNoTrack.FindStringSubmatch(line)
				w.filename = matches[1]
				w.warning = matches[2]
			} else if reTrack.MatchString(line) {
				matches := reTrack.FindStringSubmatch(line)
				w.filename = matches[1]
				w.track, _ = strconv.Atoi(matches[2])
				w.warning = matches[3]
			} else if reNoFile.MatchString(line) {
				matches := reNoFile.FindStringSubmatch(line)
				w.warning = matches[1]
			} else if reError.MatchString(line) {
				matches := reError.FindStringSubmatch(line)
				w.warning = matches[1]
			}
		}

		done <- w
	}()
	_ = cmd.Start()
	var w Warning
	w = <-done
	_ = cmd.Wait()
	if w.warning == "" {
		return nil
	} else {
		return &w
	}
}
