package main

import (
	"log"
	"os"
	"path"
	"path/filepath"
)

func verifySingleFile(path string, info os.FileInfo, config Config, index Index, chunks ChunksMap, unmatchedFiles *[]string, err error) error {
	// log.Println("Verifying: ", path)
	if err != nil {
		log.Println("On error: ", err, " Skip: ", path)
		return err
	}
	if info.IsDir() {
		for _, skip := range config.SkipList {
			log.Println("In skiplist: ", skip, " Skip: ", path)
			if info.Name() == skip {
				return filepath.SkipDir
			}
		}
	}
	if !info.IsDir() && !info.Mode().IsRegular() && (info.Mode()&os.ModeSymlink == 0) {
		log.Println("Skip non-regular file: ", path)
		return nil
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		link, _ = os.Readlink(path)
	}
	f := FileInfo{
		Mode:    info.Mode(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		LinkTo:  link,
	}
	if info.Mode().IsRegular() {
		f.Size = info.Size()
		f.Hash = xunleiHash(path, &f)
		// log.Println("Calculate hash: ", path, f.Hash)
		if _, ok := chunks[chunkHash(&f)]; !ok {
			log.Println("Missing chunk: ", path, chunkHash(&f))
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
	}
	if fi, ok := index.Files[path]; !ok {
		log.Println("Missing meta: ", path)
		*unmatchedFiles = append(*unmatchedFiles, path)
	} else if f != *fi {
		log.Println("Unmatched meta: ", path, f, fi)
		*unmatchedFiles = append(*unmatchedFiles, path)
	}
	return nil

}

func verifyFiles(config Config) {
	ri_path := path.Join(config.WorkingDir, config.Index + ".remote")
	if !downloadRemoteIndex(config, ri_path) {
		log.Fatalln("Can't download remote index")
		return
	}
	ri := loadIndex(ri_path)

	cm := scanRemoteChunksMap(config)

	uf := []string{}

	for _, filePath := range config.FilePaths {
		log.Println("Walk ", filePath)
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return verifySingleFile(path, info, config, ri, cm, &uf, err)
		})

		if err != nil {
			continue
		}
	}
	if len(uf) > 0 {
		log.Fatalln("Verify failed: ", uf)
	}
	log.Println("Verified!")
}
