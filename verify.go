package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
)

func verifySingleFile(path string, de fs.DirEntry, config Config, index Index, chunks ChunksMap, unmatchedFiles *[]string) error {
	if de.IsDir() {
		for _, skip := range config.SkipList {
			if de.Name() == skip {
				return filepath.SkipDir
			}
		}
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat file", "path", path)
			return err
		}
		d := DirInfo{
			Mode: fi.Mode().Perm(),
		}
		if fi, ok := index.Entries.Dirs[path]; !ok {
			slog.Debug("Missing meta(dir)", "path", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if d != *fi {
			slog.Debug("Unmatched meta(dir)", "path", path, "expected", d, "found", fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	if de.Type()&fs.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			return err
		}
		l := LinkInfo{
			LinkTo: link,
		}
		if fi, ok := index.Entries.Links[path]; !ok {
			slog.Debug("Missing meta(link)", "path", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if l != *fi {
			slog.Debug("Unmatched meta(link)", "path", path, "expected", l, "found", fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	if de.Type().IsRegular() {
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat file", "path", path)
			return err
		}
		f := FileInfo{
			Mode:    fi.Mode().Perm(),
			ModTime: fi.ModTime().Unix(),
			Size:    fi.Size(),
		}

		f.Hash, err = chunkHash(path, f.Size)
		if err != nil {
			slog.Debug("Hash calculation failed", "path", path, "error", err)
			*unmatchedFiles = append(*unmatchedFiles, path)
			return nil
		}
		if _, ok := chunks[ChunkKey{f.Size, f.Hash}]; !ok {
			slog.Debug("Missing chunk", "path", path, "size", f.Size, "hash", f.Hash)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		if fi, ok := index.Entries.Files[path]; !ok {
			slog.Debug("Missing meta", "path", path)
			*unmatchedFiles = append(*unmatchedFiles, path)
		} else if f != *fi {
			slog.Debug("Unmatched meta", "path", path, "expected", f, "found", fi)
			*unmatchedFiles = append(*unmatchedFiles, path)
		}
		return nil
	}
	slog.Debug("Skip non-regular file", "path", path)
	return nil
}

func verifyFiles(ctx context.Context, config Config) error {
	c, err := NewCOS(config.COS)
	if err != nil {
		return err
	}
	ri := NewIndex(config, c)
	if err := ri.LoadRemote(ctx); err != nil {
		return fmt.Errorf("download remote index: %w", err)
	}

	cm, err := scanRemoteChunksMap(ctx, c, config)
	if err != nil {
		return err
	}

	uf := []string{}

	for _, filePath := range config.FilePaths {
		slog.Debug("Walk", "path", filePath)
		err := filepath.WalkDir(path.Join(config.BasePath, filePath), func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return verifySingleFile(p, de, config, *ri, cm, &uf)
		})
		if err != nil {
			return err
		}
	}
	if len(uf) > 0 {
		return fmt.Errorf("verify failed: %v", uf)
	}
	slog.Debug("Verified!")
	return nil
}
