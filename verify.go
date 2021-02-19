package main

import (
	"log"
	"os"

	"github.com/karrick/godirwalk"
)

func verifySingleFile(path string, de *godirwalk.Dirent, config Config, index Index, chunks ChunksMap, unmatchedFiles *[]string) error {
	// log.Println("Verifying: ", path)
	if de.IsDir() {
		for _, skip := range config.SkipList {
			if de.Name() == skip {
				// log.Println("In skiplist: ", skip, " Skip: ", path)
				return godirwalk.SkipThis
			}
		}
		d := DirInfo{
			Mode: de.ModeType(),
		}
		if fi, ok := index.Entries.Dirs[path]; !ok {
			log.Println("Missing meta(dir): ", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if d != *fi {
			log.Println("Unmatched meta(dir): ", path, d, fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	if de.IsSymlink() {
		link, _ := os.Readlink(path)
		l := LinkInfo{
			LinkTo: link,
		}
		if fi, ok := index.Entries.Links[path]; !ok {
			log.Println("Missing meta(link): ", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if l != *fi {
			log.Println("Unmatched meta(link): ", path, l, fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	if de.IsRegular() {
		fi, err := os.Stat(path)
		if err != nil {
			log.Println("Can't stat file:", path)
			return err
		}
		f := FileInfo{
			Mode:    fi.Mode(),
			ModTime: fi.ModTime().Unix(),
			Size:    fi.Size(),
		}

		f.Hash = chunkHash(path, f.Size)
		// log.Println("Calculate hash: ", path, f.Hash)
		if _, ok := chunks[ChunkKey{f.Size, f.Hash}]; !ok {
			log.Println("Missing chunk: ", path, f.Size, f.Hash)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		if fi, ok := index.Entries.Files[path]; !ok {
			log.Println("Missing meta: ", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if f != *fi {
			log.Println("Unmatched meta: ", path, f, fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	log.Println("Skip non-regular file: ", path)
	return nil
}

func verifyFiles(config Config) {
	ri := NewIndex(config)
	err := ri.LoadRemote()
	if err != nil {
		log.Fatalln("Can't download remote index")
		return
	}

	cm := scanRemoteChunksMap(config)

	uf := []string{}

	for _, filePath := range config.FilePaths {
		log.Println("Walk ", filePath)
		err := godirwalk.Walk(filePath, &godirwalk.Options{
			Callback: func(path string, de *godirwalk.Dirent) error {
				return verifySingleFile(path, de, config, *ri, cm, &uf)
			},
			Unsorted: true, // (optional) set true for faster yet non-deterministic enumeration (see godoc)
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
