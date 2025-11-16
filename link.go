package main

import (
	"log/slog"
	"os"
	"path"
	"time"
)

func linkFiles(config Config) {
	if config.TargetDir == "" {
		slog.Error("Empty target dir!")
	}
	// index path
	riPath := path.Join(config.TargetDir, config.Index+".remote")
	ri := NewIndex(config)
	err := ri.Load(riPath)
	if err != nil {
		slog.Error("Can't load index")

	}

	// chunk path
	chunksPath := path.Join(config.TargetDir, "chunks")

	// linking path
	linkPath := path.Join(config.TargetDir, "restore")
	for fp, fi := range ri.Entries.Dirs {
		targetPath := path.Join(linkPath, fp)
		err := os.MkdirAll(targetPath, fi.Mode)
		if err != nil {
			slog.Error("MkdirAll error:", targetPath, err)
		}
	}

	for fp, fi := range ri.Entries.Links {
		targetPath := path.Join(linkPath, fp)
		err := os.Symlink(fi.LinkTo, targetPath)
		if err != nil {
			slog.Error("Symlink error:", targetPath, fi.LinkTo, err)
		}
		continue
	}

	for fp, fi := range ri.Entries.Files {
		targetPath := path.Join(linkPath, fp)
		k := chunkPath(ChunkKey{fi.Size, fi.Hash})
		lp := path.Join(chunksPath, k[len(k)-2:], k)
		err := os.Link(lp, targetPath)
		if err != nil {
			slog.Error("Link error:", targetPath, lp, err)
		}
		err = os.Chmod(targetPath, fi.Mode)
		if err != nil {
			slog.Error("Chmod error:", targetPath, err)
		}
		now := time.Now()
		mtime := time.Unix(fi.ModTime, 0)

		err = os.Chtimes(targetPath, now, mtime)
		if err != nil {
			slog.Error("Chmod error:", targetPath, err)
		}
	}
}
