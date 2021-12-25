package main

import (
	"fmt"
	"github.com/jerblack/base"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

/*
	extract all zip and rar files in path and recursively through subfolders
		delete archives after extract
	delete junk files and folders

	requires: unrar unzip file

	name formats
	name.rar
	name.r00
	name.r01
	name.r02
	...
	name.r99
	name.s00
	name.s01
	----
	name.part01.rar
	name.part02.rar
	...
	name.zip
	name.z01
	...
	name.000 (rar)
	name.001 (rar)
	...
	name.000 (zip)
	name.001 (zip)

*/

var (
	startPath string
	unrar     = "unrar"
	unzip     = "unzip"
	count     = 0
	rars      map[string][]string
	zips      map[string][]string
	encs      map[string][]string
	junkExts  = []string{".sfv", ".srr", ".url", ".diz", ".nzb", ".par2",
		".ds_store", "thumbs.db"}
	junkSubs    = []string{"sample", "screens", "proof"}
	junkFiles   map[string][]string
	junkFolders map[string][]string
)

func getFiles() {
	archiveCount := 0
	junkCount := 0
	rars = make(map[string][]string)
	zips = make(map[string][]string)
	encs = make(map[string][]string)
	junkFiles = make(map[string][]string)
	junkFolders = make(map[string][]string)

	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		d := filepath.Dir(path)

		if isRar(path) {
			archiveCount++
			if isRarEncrypted(path) {
				p("found encrypted rar: %s", path)
				encs[d] = append(encs[d], path)
			} else {
				p("found rar: %s", path)
				rars[d] = append(rars[d], path)
			}
		} else if isZip(path) {
			archiveCount++
			if isZipEncrypted(path) {
				p("found encrypted zip: %s", path)
				encs[d] = append(encs[d], path)
			} else {
				p("found zip: %s", path)
				zips[d] = append(zips[d], path)
			}

		} else {
			s := strings.ToLower(path)
			if !info.IsDir() {
				for _, ext := range junkExts {
					if strings.HasSuffix(s, ext) {
						junkCount++
						p("found %s file: %s", ext, path)
						junkFiles[d] = append(junkFiles[d], path)
					}
				}
				if isSample(path) {
					junkCount++
					p("found sample video: %s", path)
					junkFiles[d] = append(junkFiles[d], path)
				}

			} else {
				last := filepath.Base(s)
				for _, folder := range junkSubs {
					if last == folder {
						junkCount++
						p("found %s folder: %s", folder, path)
						junkFolders[d] = append(junkFolders[d], path)
					}
				}
				if isDirEmpty(path) {
					junkCount++
					p("found empty folder: %s", path)
					junkFolders[d] = append(junkFolders[d], path)
				}

			}
		}

		return nil
	}
	err := filepath.Walk(startPath, walk)
	chkFatal(err)

	if (archiveCount > 0 && deleteArchive) || (junkCount > 0 && deleteJunk) {
		extract()
	}
}

func isZip(path string) bool {
	f, e := os.Stat(path)
	if e != nil {
		chk(e)
		return false
	} else if f.IsDir() {
		return false
	}

	s := strings.ToLower(path)
	if strings.HasSuffix(s, ".zip") {
		return true
	}

	r1 := regexp.MustCompile(`.z\d{2}$`)
	if r1.MatchString(s) {
		return true
	}

	r2 := regexp.MustCompile(`.\d{3}$`)
	if r2.MatchString(s) {
		out, e := exec.Command("file", "--mime", path).Output()
		if e == nil && strings.Contains(string(out), "application/zip;") {
			return true
		}
	}
	return false
}
func isRar(path string) bool {
	f, e := os.Stat(path)
	if e != nil {
		chk(e)
		return false
	} else if f.IsDir() {
		return false
	}

	s := strings.ToLower(path)

	if strings.HasSuffix(s, ".rar") {
		return true
	}
	//r1 := regexp.MustCompile(`.part\d+.rar$`)
	r2 := regexp.MustCompile(`.[rs]\d{2}$`)
	r3 := regexp.MustCompile(`.\d{3}$`)
	if r2.MatchString(s) {
		return true
	}

	if r3.MatchString(s) {
		out, e := exec.Command("file", "--mime", path).Output()
		if e == nil && strings.Contains(string(out), "x-rar") {
			return true
		}
	}
	return false
}
func isZipEncrypted(zip string) bool {
	args := []string{"-v", zip}
	out, _ := exec.Command(unzip, args...).Output()
	s := string(out)
	if strings.Contains(s, "Unk:099") {
		return true
	}

	return false
}
func isRarEncrypted(rar string) bool {
	args := []string{"l", "-p-", rar}
	out, _ := exec.Command(unrar, args...).Output()
	s := string(out)
	if strings.Contains(s, "\n*") {
		return true
	}
	if strings.Contains(s, "\nDetails: .+ encrypted headers") {
		return true
	}

	return false
}
func isSample(path string) bool {
	s := strings.ToLower(path)
	r1 := regexp.MustCompile(`\.sample\.(asf|avi|mkv|mp4|m4v|mov|mpg|mpeg|ogg|webm|wmv)$`)
	if r1.MatchString(s) {
		return true
	}
	r2 := regexp.MustCompile(`sample-.+\.(asf|avi|mkv|mp4|m4v|mov|mpg|mpeg|ogg|webm|wmv)$`)
	if r2.MatchString(s) {
		return true
	}
	return false
}

func extract() {

	/*
		find first rar/zip
		start extract from first archive
		after extract, delete archives listed in array
		delete enc files
		delete all junk files and folders
		call getFiles again, will loop until no more qualifying files
	*/
	for path := range rars {
		frs := firstRar(path)
		if frs != nil {
			for _, r := range frs {
				p("extracting rar file: %s", r)
				extractRar(r)
			}
		}
	}
	for path := range zips {
		fzs := firstZip(path)
		if fzs != nil {
			for _, z := range fzs {
				p("extracting zip file: %s", z)
				extractZip(z)
			}
		}
	}
	clean()
	getFiles()
}
func clean() {
	if deleteArchive {
		for _, fileMaps := range []map[string][]string{rars, zips, encs} {
			for _, fMap := range fileMaps {
				for _, f := range fMap {
					p("removing file: %s", f)
					err := os.Remove(f)
					chk(err)
				}
			}
		}
	}

	if !deleteJunk {
		return
	}
	for _, fileMaps := range []map[string][]string{junkFiles} {
		for _, fMap := range fileMaps {
			for _, f := range fMap {
				p("removing file: %s", f)
				err := os.Remove(f)
				chk(err)
			}
		}
	}
	for _, junkFolder := range junkFolders {
		for _, jf := range junkFolder {
			p("removing folder: %s", jf)

			err := os.RemoveAll(jf)
			chk(err)
		}
	}
}

func extractRar(rar string) {
	cmd := []string{unrar, "x", "-o+", "-y", rar, dstFolder(rar)}
	e := run(cmd...)
	chk(e)
}
func extractZip(zip string) {
	cmd := []string{unzip, "-o", zip, "-d", dstFolder(zip)}
	e := run(cmd...)
	chk(e)
}

func firstZip(path string) []string {
	list := zips[path]
	var zips, zeroes, ones []string

	for _, path := range list {
		s := strings.ToLower(path)
		if strings.HasSuffix(s, ".zip") {
			zips = append(zips, path)
		}
		if strings.HasSuffix(s, ".000") {
			zeroes = append(zeroes, path)
		}
		if strings.HasSuffix(s, ".001") {
			ones = append(ones, path)
		}
	}
	if len(zips) > 0 {
		return zips
	}
	if len(zeroes) > 0 {
		return zeroes
	}
	if len(ones) > 0 {
		return ones
	}

	return nil
}
func firstRar(path string) []string {
	list := rars[path]
	reg := `.part(?P<num>\d+).rar$`
	lowNum := -1
	var lowPaths, rars, zeroes, ones []string

	for _, path := range list {
		s := strings.ToLower(path)
		groups := getParams(reg, s)

		if num, hasVal := groups["num"]; hasVal {
			n, _ := strconv.Atoi(num)
			if lowNum == -1 || n < lowNum {
				lowNum = n
				lowPaths = []string{path}
			} else if lowNum == n {
				lowPaths = append(lowPaths, path)
			}
		}
		if strings.HasSuffix(s, ".rar") {
			rars = append(rars, path)
		}
		if strings.HasSuffix(s, ".000") {
			zeroes = append(zeroes, path)
		}
		if strings.HasSuffix(s, ".001") {
			ones = append(ones, path)
		}
	}
	if lowNum != -1 {
		return lowPaths
	}
	if len(rars) > 0 {
		return rars
	}
	if len(zeroes) > 0 {
		return zeroes
	}
	if len(ones) > 0 {
		return ones
	}

	return nil
}
func dstFolder(f string) string {
	//if extract into folder with same name as archive
	//re := regexp.MustCompile(`(?i).part\d+.rar$`)
	//if re.MatchString(f) {
	//	return re.ReplaceAllString(f, "/")
	//}
	//re = regexp.MustCompile(`(?i).(zip|rar|\d+)$`)
	//return re.ReplaceAllString(f, "/")

	//if extract into same folder archive lives
	return filepath.Dir(f) + "/"
}

var deleteArchive = true
var deleteJunk = true

var help = `extract [-h][-a][-j][folder path]
  extract all zip and rar files in current folder and all subfolders, delete archives on successful extraction, 
  and delete junk files.
  -h  print this message
  -a  DO NOT delete archive files after extract.
      These files are deleted by default.
  -j  DO NOT delete junk files after extract.
      These files are deleted by default.
      files:
        ".sfv", ".srr", ".url", ".diz", ".nzb", ".par2", ".ds_store", 
        "thumbs.db", "*sample*"
      folders:
        "sample", "screens", "proof"
   extract runs recursively in the current folder by default unless a folder path is provided, 
   in which case it runs recursively from there. 
`

func main() {
	startPath, _ = os.Getwd()
	for _, arg := range os.Args {
		lower := strings.ToLower(arg)
		if lower == "-h" || lower == "-?" {
			fmt.Println(help)
			os.Exit(0)
		} else if lower == "-a" {
			fmt.Println("-a set: don't delete archives")
			deleteArchive = false
		} else if lower == "-j" {
			fmt.Println("-j set: don't delete junk")
			deleteJunk = false
		} else {
			f, e := os.Stat(arg)
			if e == nil && f.IsDir() {
				startPath = arg
			}
		}
	}

	p("extract called in: %s", startPath)

	getFiles()
	extract()
}

/**
 * Parses url with the given regular expression and returns the
 * group values defined in the expression.
 *
 */
func getParams(regEx, s string) (paramsMap map[string]string) {

	var compRegEx = regexp.MustCompile(regEx)
	match := compRegEx.FindStringSubmatch(s)

	paramsMap = make(map[string]string)
	for i, name := range compRegEx.SubexpNames() {
		if i > 0 && i <= len(match) {
			paramsMap[name] = match[i]
		}
	}
	return paramsMap
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	isDirEmpty = base.IsDirEmpty
	run        = base.Run
)
