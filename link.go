package main

import (
	"context"
	"os"
	"path"
	"time"
)

func linkFiles(_ context.Context, config Config) error {
	riPath := path.Join(config.TargetDir, config.Index+".remote")
	ri := NewIndex(config, nil)
	if err := ri.Load(riPath); err != nil {
		return err
	}

	chunksPath := path.Join(config.TargetDir, "chunks")
	linkPath := path.Join(config.TargetDir, "restore")
	for fp, fi := range ri.Entries.Dirs {
		targetPath := path.Join(linkPath, fp)
		if err := os.MkdirAll(targetPath, fi.Mode); err != nil {
			return err
		}
	}

	for fp, fi := range ri.Entries.Links {
		targetPath := path.Join(linkPath, fp)
		if err := os.Symlink(fi.LinkTo, targetPath); err != nil {
			return err
		}
	}

	for fp, fi := range ri.Entries.Files {
		targetPath := path.Join(linkPath, fp)
		k := chunkPath(ChunkKey{fi.Size, fi.Hash})
		lp := path.Join(chunksPath, k[len(k)-2:], k)
		if err := os.Link(lp, targetPath); err != nil {
			return err
		}
		if err := os.Chmod(targetPath, fi.Mode); err != nil {
			return err
		}
		now := time.Now()
		mtime := time.Unix(fi.ModTime, 0)
		if err := os.Chtimes(targetPath, now, mtime); err != nil {
			return err
		}
	}
	return nil
}
