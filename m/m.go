package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
	m
	automatically parse and rename movie files
	if no file specified, look in current folder
		recursively gather all mkv files
	for each video
		clean and guess name
		display menu of possible options
		on selection search tmdb
		use tmdb to
			guess destination subfolder from genre
			clean final file name to match official name
		display menu of possible subfolders with guessed from tmdb on top
		on selection of new name
			move file to new folder with new name
			prompt to remove src folder if not main working folder
*/

var (
	bad = []string{
		"dd+", "ddp", "5.1", "7.1", "truehd", "lpcm", "x264", "h.264", "ac3", "h264", "dvdr", "x.264", "dts",
		"1080p", "720p", "nf", "web-dl", "atmos", "hulu", "dsnp", "dts-hd", "amzn", "hdrip", "avc", "bluray",
		"dd-ex", "bdrip", "hevc",
	}
	procFolder = "/x/_proc"

	sortedFolders = []string{"4K", "4K_docs", "action_adventure_sci-fi", "animated", "before_1980", "before_2000", "comedy",
		"comic_book", "docs", "drama", "foreign", "hallmark_family", "horror", "kids", "martial_arts",
		"new_releases", "politics", "standup_comedy", "unlisted"}
	sortedFolder = "/z/~movies"
	apiKey       string
	genres       = map[int]string{
		28: "Action", 12: "Adventure", 16: "Animation", 35: "Comedy", 80: "Crime", 99: "Documentary", 18: "Drama",
		10751: "Family", 14: "Fantasy", 36: "History", 27: "Horror", 10402: "Music", 9648: "Mystery", 10749: "Romance",
		878: "Science Fiction", 10770: "TV Movie", 53: "Thriller", 10752: "War", 37: "Western",
	}
	recommends = map[int]string{
		28: "action_adventure_sci-fi", 12: "action_adventure_sci-fi", 16: "animated", 35: "comedy", 80: "drama",
		99: "docs", 18: "drama", 10751: "hallmark_family", 14: "action_adventure_sci-fi", 36: "drama", 27: "horror",
		10402: "hallmark_family", 9648: "drama", 10749: "drama", 878: "action_adventure_sci-fi", 10770: "drama",
		53: "horror", 10752: "drama", 37: "drama",
	}
)

type TmdbDate struct {
	time.Time
}

func (td *TmdbDate) UnmarshalJSON(b []byte) (e error) {
	s := strings.Trim(string(b), "\"")
	if s == "null" {
		td.Time = time.Time{}
		return
	}
	td.Time, e = time.Parse("2006-01-02", s)
	return
}

type TmdbResults struct {
	Results []TmdbResult `json:"results"`
}
type TmdbResult struct {
	Adult            bool     `json:"adult"`
	BackdropPath     string   `json:"backdrop_path"`
	GenreIds         []int    `json:"genre_ids"`
	Id               int      `json:"id"`
	OriginalLanguage string   `json:"original_language"`
	OriginalTitle    string   `json:"original_title"`
	Overview         string   `json:"overview"`
	Popularity       float64  `json:"popularity"`
	PosterPath       string   `json:"poster_path"`
	ReleaseDate      TmdbDate `json:"release_date"`
}

func (t *TmdbResult) genres() []string {
	var g []string
	for _, id := range t.GenreIds {
		g = append(g, genres[id])
	}
	return g
}

func tmdbSearch(title, year string) (t *TmdbResult) {
	uri := `https://api.themoviedb.org/3/search/movie`
	req, e := http.NewRequest("GET", uri, nil)
	if e != nil {
		fmt.Println(e)
		return
	}
	q := req.URL.Query()
	q.Add("api_key", apiKey)
	q.Add("query", title)
	if year != "" {
		q.Add("year", year)
	}
	req.URL.RawQuery = q.Encode()
	var client http.Client
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
	var trs TmdbResults
	e = json.Unmarshal(body, &trs)
	if e != nil {
		fmt.Println(e)
		return
	}
	if len(trs.Results) > 0 {
		t = &trs.Results[0]
	}
	return
}
func getNameYear(fName string) (title, year string) {
	reTitleYear := regexp.MustCompile(`(.+) \((\d{4})\)`)
	if reTitleYear.MatchString(fName) {
		matches := reTitleYear.FindStringSubmatch(fName)
		title = matches[1]
		year = matches[2]
	} else {
		title = strings.TrimSuffix(fName, filepath.Ext(fName))
	}
	return
}

func isMkv(filePath string) bool {
	filePath = strings.ToLower(filePath)
	return filepath.Ext(filePath) == ".mkv"
}
func trimToYear(name string) string {
	now := time.Now().Year() + 2
	for y := now; y > 1901; y-- {
		yr := strconv.Itoa(y)
		i := strings.LastIndex(name, yr)
		if i != -1 {
			return fmt.Sprintf("%s(%s)", name[:i], yr)
		}
	}
	return name
}
func trimNoise(name string) string {
	lower := strings.ToLower(name)
	for _, b := range bad {
		i := strings.Index(lower, b)
		if i != -1 {
			lower = lower[:i]
			name = name[:i]
		}
	}
	return name
}

func getYesNo(prompt string) bool {
	r := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(prompt)
		var tf string
		_, err := fmt.Fscanf(r, "%s\n", &tf)
		if err != nil && err.Error() == "unexpected newline" {
			return false
		}
		chk(err)
		if tf == "y" || tf == "Y" || tf == "1" {
			return true
		} else {
			return false
		}
	}
}
func getInt(prompt string, max int) int {
	r := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(prompt)
		var n int
		_, err := fmt.Fscanf(r, "%d\n", &n)
		if n > max {
			fmt.Printf("  selection must be number less than %d\n", max+1)
		} else if err != nil && err.Error() == "expected integer" {
			fmt.Printf("  selection must be number less than %d\n", max+1)
			r.ReadBytes(10)
		} else {
			return n
		}
	}
}
func printSortedFolders() {
	var sfs []string
	for i, sf := range sortedFolders {
		sfs = append(sfs, fmt.Sprintf("  %02d) %s", i+1, sf))
	}
	fmt.Println(strings.Join(sfs, "\n"))
}

func titleCase(name string) string {
	smalls := []string{"a", "an", "the", "of", "on", "vs", "and"}
	roman := []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"}
	name = strings.ToLower(name)
	var parts []string
	for n, part := range strings.Split(name, " ") {
		if isAny(part, smalls...) && n != 0 {
			part = strings.ToLower(part)
		} else if isAny(strings.ToUpper(part), roman...) {
			part = strings.ToUpper(part)
		} else {
			part = strings.Title(part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func cleanFileName(filePath string) string {
	fName := path.Base(filePath)
	name := strings.TrimSuffix(fName, path.Ext(fName))
	name = trimToYear(name)
	name = trimNoise(name)
	re := regexp.MustCompile(`[_.]`)
	name = re.ReplaceAllString(name, " ")
	re = regexp.MustCompile(`\(+`)
	name = re.ReplaceAllString(name, "(")
	name = strings.TrimSpace(name)
	return titleCase(name)
}
func cleanFolderName(filePath string) string {
	parentPath := path.Dir(filePath)
	name := path.Base(parentPath)
	name = trimToYear(name)
	name = trimNoise(name)
	re := regexp.MustCompile(`[_.]`)
	name = re.ReplaceAllString(name, " ")
	re = regexp.MustCompile(`\(+`)
	name = re.ReplaceAllString(name, "(")
	name = strings.TrimSpace(name)
	return titleCase(name)
}

func getNameOptions(filePath string, clean string) []string {
	/*
		proc/parent/clean.ext
		proc/parent/parent.ext
		proc/clean.ext
		proc/parent.ext
	*/
	var names []string
	names = append(names, filePath)
	ext := path.Ext(filePath)
	parentPath, _ := path.Split(filePath)
	parentPath = path.Clean(parentPath)
	rootFolder, parent := path.Split(parentPath)
	rootFolder = path.Clean(rootFolder)

	// proc/parent/clean.ext
	name := path.Join(parentPath, clean+ext)
	names = append(names, name)
	//fmt.Println(name)

	// proc/parent/parent.ext
	if parentPath != path.Clean(procFolder) {
		name = path.Join(parentPath, parent+ext)
		names = append(names, name)
		//fmt.Println(name)

		// proc/clean.ext
		name = path.Join(rootFolder, clean+ext)
		names = append(names, name)
		//fmt.Println(name)

		// proc/parent.ext
		name = path.Join(rootFolder, parent+ext)
		names = append(names, name)
		//fmt.Println(name)
	}

	return names
}

func getMkvs(workDir string) []string {
	var mkvs []string

	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && isMkv(p) {
			mkvs = append(mkvs, p)
		}
		return nil
	}
	err := filepath.Walk(workDir, walk)
	chkFatal(err)

	return mkvs
}
func getFiles(folder string) (files []string) {
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		files = append(files, p)
		return nil
	}
	err := filepath.Walk(folder, walk)
	chkFatal(err)
	return
}

func main() {
	/*
		show work folder
		show video in folder
		text: name options :
		show numbered list of name options
		text: select name [1]
			enter number and hit enter to select
			or just hit enter to pick default option
		text: renamed to ...mkv

		text: move file to movies subfolder folder [0]?
			NO, 4K, action_adventure_sci-fi, animated, before_1980, before_2000, comedy, comic_book,
			docs, drama, foreign, hallmark_family, horror, kids, martial_arts, new_releases,
			standup_comedy, unlisted
			[numbered, with No as default option 0]
			text: moved to /z/~movies/folder/name.ext for each file
		if renamed to subfolder or moved to sorted and source folder is not proc folder
			text: ## files remaining in folder, fname1 fname2 ...
			text: delete source folder [Yn]
				if yes, change cwd to .. and delete source folder
	*/

	apiKey = os.Getenv("TMDB_KEY")
	if apiKey == "" {
		fmt.Println("no api key found in TMDB_KEY env var")
	}
	args := os.Args[1:]
	workDir := ""
	if len(args) > 0 {
		workDir = args[0]
	} else {
		workDir, _ = os.Getwd()
	}
	workDir, _ = filepath.Abs(workDir)
	fmt.Printf("path : %s\n", workDir)
	mkvs := getMkvs(workDir)
	if len(mkvs) == 0 {
		fmt.Printf("| no videos found in folder\n")
		return
	} else {
		fmt.Printf("| found %d mkvs in folder\n", len(mkvs))
	}
	for _, mkv := range mkvs {
		searchName := mkv
		fmt.Printf("video :\n| %s\n", mkv)
		fmt.Println("name options :")
		file := cleanFileName(mkv)
		folder := cleanFolderName(mkv)
		var names []string
		add(&names, getNameOptions(mkv, file)...)
		add(&names, getNameOptions(mkv, folder)...)
		for i, name := range names {
			fmt.Printf("  %d) %s\n", i, name)
		}
		nameIndex := getInt("select name [# or Enter to skip] ", len(names))
		if nameIndex >= 0 && nameIndex < len(names)+1 {
			selected := names[nameIndex]
			if nameIndex > 0 {
				fmt.Printf("| renamed: %s -> %s\n", mkv, selected)
				err := os.Rename(mkv, selected)
				chkFatal(err)
				searchName = selected
			} else {
				fmt.Println("| original name kept")
				if len(names) > 0 {
					searchName = names[0]
				}
			}

		} else {
			fmt.Println("| file name unchanged.")
		}

		fName := filepath.Base(searchName)
		title, year := getNameYear(fName)
		tmdb := tmdbSearch(title, year)
		guess := ""
		if tmdb != nil {
			fmt.Printf("tmdb info :\n")
			fmt.Printf("| title: %s (%d)\n", tmdb.OriginalTitle, tmdb.ReleaseDate.Year())
			fmt.Printf("| genres: %s\n", strings.Join(tmdb.genres(), ", "))
			fmt.Printf("| overview: %s\n", tmdb.Overview)
			if len(tmdb.GenreIds) > 0 {
				guess = recommends[tmdb.GenreIds[0]]
			}
			if tmdb.ReleaseDate.Year() < 1980 {
				guess = "before_1980"
			} else if tmdb.ReleaseDate.Year() < 2000 {
				guess = "before_2000"
			}

			if title != tmdb.OriginalTitle {
				fmt.Printf("tmdb title different from current title :\n")
				tmdbFname := fmt.Sprintf("%s (%d).mkv", tmdb.OriginalTitle, tmdb.ReleaseDate.Year())
				tmdbFname = getLegalFilename(tmdbFname)
				prompt := fmt.Sprintf("| rename %s -> %s ? [1,0,y,N or Enter to skip] ", fName, tmdbFname)
				if getYesNo(prompt) {
					newPath := filepath.Join(filepath.Dir(searchName), tmdbFname)
					e := os.Rename(searchName, newPath)
					chkFatal(e)
					searchName = newPath
					fName = tmdbFname
					fmt.Printf("| renamed to %s\n", searchName)
				} else {
					fmt.Println("| file name unchanged.")
				}
			}
		}
		if guess == "" && year != "" {
			yearInt, e := strconv.Atoi(year)
			if e == nil {
				if yearInt < 1980 {
					guess = "before_1980"
				} else if yearInt < 2000 {
					guess = "before_2000"
				}
			}
		}
		if strings.Contains(mkv, "2160") || strings.Contains(strings.ToLower(mkv), "4k") {
			if tmdb != nil && isAny("Documentary", tmdb.genres()...) {
				guess = "4K_docs"
			} else {
				guess = "4K"
			}
		}

		fmt.Println("folder options :")
		if guess != "" {
			fmt.Printf("  00) %s [guessed]\n", guess)
		}
		printSortedFolders()

		prompt := fmt.Sprintf("Move %s to /z/~movies/ subfolder? [# or Enter to skip] ", fName)
		moveTo := getInt(prompt, len(sortedFolders))
		var dst string
		if moveTo == 0 && guess != "" {
			dst = path.Join(sortedFolder, guess, fName)
		} else if moveTo > 0 && moveTo < len(sortedFolders) {
			dst = path.Join(sortedFolder, sortedFolders[moveTo-1], fName)
		}
		if dst != "" {
			fmt.Printf("| Moving -> %s\n", dst)
			e := mvFile(searchName, dst)
			chkFatal(e)
			mkvDir := filepath.Dir(mkv)
			if mkvDir != procFolder && mkvDir != workDir {
				files := getFiles(mkvDir)
				if len(files) > 0 {
					fmt.Printf("| %d files remaining in movie folder %s\n", len(files), mkvDir)
					for _, f := range files {
						fmt.Printf("| > %s\n", f)
					}
				}
				if getYesNo(fmt.Sprintf("delete folder %s ? [1,0,y,N or Enter to skip] ", mkvDir)) {
					fmt.Println("| deleting folder")
					e := os.RemoveAll(mkvDir)
					chkFatal(e)
				} else {
					fmt.Println("| leaving folder in place")
				}
			}
		} else {
			fmt.Println("| leaving file in place")
		}
	}
}

func add(arr *[]string, items ...string) {
	for _, item := range items {
		if !isAny(item, *arr...) {
			*arr = append(*arr, item)
		}
	}
}

var (
	p                = base.P
	chk              = base.Chk
	chkFatal         = base.ChkFatal
	arrayIdx         = base.ArrayIdx
	run              = base.Run
	rmEmptyFolders   = base.RmEmptyFolders
	printCmd         = base.PrintCmd
	mvFile           = base.MvFile
	fileExists       = base.FileExists
	isAny            = base.IsAny
	getLegalFilename = base.GetLegalFilename
)
