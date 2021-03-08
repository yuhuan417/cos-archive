package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func verifySingleFile(path string, de fs.DirEntry, config Config, index Index, chunks ChunksMap, unmatchedFiles *[]string) error {
	// log.Println("Verifying: ", path)
	if de.IsDir() {
		for _, skip := range config.SkipList {
			if de.Name() == skip {
				// log.Println("In skiplist: ", skip, " Skip: ", path)
				return filepath.SkipDir
			}
		}
		fi, err := de.Info()
		if err != nil {
			log.Println("Can't stat file:", path)
			return err
		}
		d := DirInfo{
			Mode: fi.Mode().Perm(),
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
	if de.Type()&fs.ModeSymlink != 0 {
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
	if de.Type().IsRegular() {
		fi, err := de.Info()
		if err != nil {
			log.Println("Can't stat file:", path)
			return err
		}
		f := FileInfo{
			Mode:    fi.Mode().Perm(),
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
		err := filepath.WalkDir(filePath, func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return verifySingleFile(path, de, config, *ri, cm, &uf)
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
