package base

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func P(s string, i ...interface{}) {
	now := time.Now()
	t := strings.ToLower(strings.TrimRight(now.Format("3.04PM"), "M"))
	notice := fmt.Sprintf("%s | %s", t, fmt.Sprintf(s, i...))
	fmt.Println(notice)
}
func ChkFatal(err error) {
	if err != nil {
		fmt.Println("----------------------")
		panic(err)
	}
}
func Chk(err error) {
	if err != nil {
		fmt.Println("----------------------")
		fmt.Println(err)
		fmt.Println("----------------------")
	}
}
func HumanSize(b int64) string {
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

func ArrayIdx(slice []string, val string) int {
	for n, item := range slice {
		if item == val {
			return n
		}
	}
	return -1
}
func IsAny(a string, b ...string) bool {
	for _, _b := range b {
		if a == _b {
			return true
		}
	}
	return false
}
func IsAnyInt(a int, b ...int) bool {
	for _, _b := range b {
		if a == _b {
			return true
		}
	}
	return false
}
func ContainsString(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
func Run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)

	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &stdBuffer)

	cmd.Stdout = mw
	cmd.Stderr = mw

	return cmd.Run()
}
func PrintCmd(cmd []string) {
	var parts []string
	for _, c := range cmd {
		if strings.Contains(c, " ") {
			c = fmt.Sprintf("\"%s\"", c)
		}
		parts = append(parts, c)
	}
	P(strings.Join(parts, " "))
}
func RmEmptyFolders(root string) {
	var folders []string
	root, _ = filepath.Abs(root)
	walk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			path, _ = filepath.Abs(path)
			if root != path {
				folders = append(folders, path)
			}
		}
		return nil
	}
	err := filepath.Walk(root, walk)
	ChkFatal(err)

	fn := func(i, j int) bool {
		// reverse sort
		return len(folders[j]) < len(folders[i])
	}
	sort.Slice(folders, fn)
	for _, f := range folders {
		if IsDirEmpty(f) {
			err = os.Remove(f)
			Chk(err)
		}
	}
}
func IsDirEmpty(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer func() {
		err = f.Close()
		Chk(err)
	}()

	// read in ONLY one file
	_, err = f.Readdir(1)

	// if file is EOF the dir is empty.
	if err == io.EOF {
		return true
	}
	if err == io.EOF {
		return true
	}
	return false
}
func GetAltPath(path string) string {
	i := 1
	newPath := path
	for {
		_, e := os.Stat(newPath)
		if errors.Is(e, os.ErrNotExist) {
			return newPath
		}
		newPath = fmt.Sprintf("%s.%d", path, i)
		i += 1
	}
}
func GetFile(file string) string {
	b, e := os.ReadFile(file)
	if e == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}
func SetFile(file, val string) {
	_ = os.WriteFile(file, []byte(val), 0400)
}
func RenFile(src, dst string) error {
	e := os.MkdirAll(filepath.Dir(dst), 0755)
	if e != nil {
		return e
	}
	return os.Rename(src, dst)
}
func MvFile(src, dst string) error {
	e := os.MkdirAll(filepath.Dir(dst), 0755)
	if e != nil {
		return e
	}
	e = os.Rename(src, dst)
	if e == nil {
		return nil
	}
	if !strings.Contains(e.Error(), "invalid cross-device link") {
		return e
	}

	in, e := os.Open(src)
	if e != nil {
		return e
	}
	out, e := os.Create(dst)
	if e != nil {
		return e
	}
	defer out.Close()
	_, e = io.Copy(out, in)
	if e != nil {
		return e
	}
	e = in.Close()
	if e != nil {
		return e
	}
	e = out.Sync()
	if e != nil {
		return e
	}
	st, e := os.Stat(src)
	if e != nil {
		return e
	}
	e = os.Chmod(dst, st.Mode())
	if e != nil {
		return e
	}
	e = os.Remove(src)
	if e != nil {
		return e
	}
	return nil
}

func MvTree(src, dst string, removeEmpties bool) {
	P("moving tree %s to %s", src, dst)
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
	ChkFatal(err)

	for _, f := range folders {
		newFolder := strings.Replace(f, src, dst, 1)
		err := os.MkdirAll(newFolder, 0777)
		ChkFatal(err)
	}
	for _, f := range files {
		dstFile := strings.Replace(f, src, dst, 1)
		dstFile = GetAltPath(dstFile)
		P("moving file to %s", dstFile)
		renErr := os.Rename(f, dstFile)
		ChkFatal(renErr)

	}
	if removeEmpties {
		RmEmptyFolders(src)
	}
}

func FileExists(f string) bool {
	_, e := os.Stat(f)
	return e == nil
}
