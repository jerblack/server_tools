package main

import (
	"fmt"
	"github.com/jerblack/base"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
hardlink all files in src tree into matching dst tree

linktree folder1 <folder2>

each arg to linktree is a subfolder in the srcBase

iterate through each folder given in args
	recreate folder tree in dstBase
		/srcBase/folderArg1/a/ -> /dstBase/folderArg1/a/
		/srcBase/folderArg1/b/c/ -> /dstBase/folderArg1/b/c/
	hardlink all files in srcBase tree into dstBase tree

*/
const (
	srcBase = "/z/tor_done"
	dstBase = "/z/_proc"
)

var (
	srcs, dsts []string
)

func getArgs() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: linktree folder1 <folder2>\nsource base: %s\ndest base: %s\n", srcBase, dstBase)
		os.Exit(1)
	}
	for _, arg := range os.Args[1:] {
		src := filepath.Join(srcBase, arg)
		dst := filepath.Join(dstBase, arg)
		if !fileExists(src) {
			fmt.Printf("error: specified sourse path does not exist: %s\n", src)
			os.Exit(1)
		}
		srcs = append(srcs, src)
		dsts = append(dsts, dst)
	}
}

func main() {
	getArgs()
	for n, src := range srcs {
		dst := dsts[n]
		linkTree(src, dst)
	}

}

func linkTree(src, dst string) {
	fmt.Printf("linking tree '%s' -> '%s'\n", src, dst)
	var files []string
	var folders []string
	walk := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			folders = append(folders, p)
		} else {
			files = append(files, p)
		}
		return nil
	}
	err := filepath.Walk(src, walk)
	chkFatal(err)
	sort.Slice(folders, func(i, j int) bool { return len(folders[i]) < len(folders[j]) })
	fmt.Println("--making folders--")
	for _, f := range folders {
		fmt.Printf("%s\n", f)
		fi, err := os.Stat(f)
		chkFatal(err)

		newFolder := strings.Replace(f, src, dst, 1)

		err = os.MkdirAll(newFolder, fi.Mode())
		chkFatal(err)
	}
	fmt.Println("--linking files--")
	var probFiles []string
	for _, f := range files {
		fmt.Printf("%s\n", f)
		dstFile := strings.Replace(f, src, dst, 1)
		if !fileExists(dstFile) {
			err = os.Link(f, dstFile)
			if err != nil {
				chk(err)
				probFiles = append(probFiles, f)
			}
		}
	}
	if len(probFiles) > 0 {
		fmt.Printf("FILES FAILED TO LINK\n--------------------\n")
		for _, prob := range probFiles {
			fmt.Println(prob)
		}
	}
}

var (
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	fileExists = base.FileExists
)
