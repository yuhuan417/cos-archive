package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

func prepareTestDirTree(tree string) (string, error) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		return "", fmt.Errorf("error creating temp directory: %v\n", err)
	}

	err = os.MkdirAll(filepath.Join(tmpDir, tree), 0755)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}

func processSingleFile(path string, info os.FileInfo, subDirToSkip string, err error) error {
	if err != nil {
		fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
		return err
	}
	if info.IsDir() && info.Name() == subDirToSkip {
		fmt.Printf("skipping a dir without errors: %+v \n", info.Name())
		return filepath.SkipDir
	}
	fmt.Printf("visited file or dir: %q\n", path)
	fmt.Println("name =", info.Name())
	fmt.Println("size =", info.Size())
	fmt.Println("mode =", info.Mode())
	fmt.Println("is link =", info.Mode()&os.ModeSymlink)
	fmt.Println("modtime =", info.ModTime())
	fmt.Println("isDir =", info.IsDir())
	fmt.Println("sys =", info.Sys())
	return nil
}

func main() {
	// tmpDir, err := prepareTestDirTree("dir/to/walk/skip")
	// if err != nil {
	//	fmt.Printf("unable to create test dir tree: %v\n", err)
	//	return
	//}
	//defer os.RemoveAll(tmpDir)
	//os.Chdir(tmpDir)

	cwd, err := os.Getwd()
	subDirToSkip := ".git"
	fmt.Println("On Unix: ", cwd)
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		return processSingleFile(path, info, subDirToSkip, err)
	})

	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", ".", err)
		return
	}
}
