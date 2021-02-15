package main

import (
	"log"
	"os"
	"path"
	"syscall"
	"time"
)

func hardlinkCount(fp string) uint64 {
	fi, err := os.Stat(fp)
	if err != nil {
		log.Println(err)
		return uint64(0)
	}
	nlink := uint64(0)
	if sys := fi.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			nlink = uint64(stat.Nlink)
		}
	}
	return nlink
}

func linkFiles(config Config) {
	if config.TargetDir == "" {
		log.Fatalln("Empty target dir!")
	}
	// index path
	riPath := path.Join(config.TargetDir, config.Index+".remote")
	ri := NewIndex(config)
	err := ri.Load(riPath)
	if err != nil {
		log.Fatalln("Can't load index")

	}

	// chunk path
	chunksPath := path.Join(config.TargetDir, "chunks")

	// linking path
	linkPath := path.Join(config.TargetDir, "restore")

	for fp, fi := range ri.Files {
		targetPath := path.Join(linkPath, fp)
		if fi.IsDir {
			err := os.MkdirAll(targetPath, fi.Mode)
			if err != nil {
				log.Fatal("MkdirAll error:", targetPath, err)
			}
			continue
		}
		if fi.LinkTo != "" {
			err := os.Symlink(fi.LinkTo, targetPath)
			if err != nil {
				log.Fatal("Symlink error:", targetPath, fi.LinkTo, err)
			}
			continue
		}
	}
	for fp, fi := range ri.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		targetPath := path.Join(linkPath, fp)
		k := chunkPath(ChunkKey{fi.Size, fi.Hash})
		lp := path.Join(chunksPath, k[len(k)-2:], k)
		err := os.Link(lp, targetPath)
		if err != nil {
			log.Fatal("Link error:", targetPath, lp, err)
		}
		err = os.Chmod(targetPath, fi.Mode)
		if err != nil {
			log.Fatal("Chmod error:", targetPath, err)
		}
		now := time.Now()
		mtime := time.Unix(fi.ModTime, 0)

		err = os.Chtimes(targetPath, now, mtime)
		if err != nil {
			log.Fatal("Chmod error:", targetPath, err)
		}
	}
}
