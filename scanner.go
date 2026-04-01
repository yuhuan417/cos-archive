package main

import (
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
)

func (index *Index) scanSingleFile(path string, de fs.DirEntry) error {
	if de.IsDir() {
		for _, skip := range index.config.SkipList {
			if de.Name() == skip {
				return filepath.SkipDir
			}
		}
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat dir", "path", path)
			return nil
		}
		d := DirInfo{
			Mode: fi.Mode().Perm(),
		}
		index.Entries.Dirs[path] = &d
		return nil
	}
	if de.Type()&fs.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			slog.Debug("Can't read link", "path", path, "error", err)
			return nil
		}
		l := LinkInfo{
			LinkTo: link,
		}
		index.Entries.Links[path] = &l
		return nil
	}
	if de.Type().IsRegular() {
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat file", "path", path)
			return nil
		}
		f := FileInfo{
			Mode:    fi.Mode().Perm(),
			ModTime: fi.ModTime().Unix(),
			Size:    fi.Size(),
		}
		index.Entries.Files[path] = &f
		return nil
	}
	slog.Debug("Skip non-regular file", "path", path)
	return nil
}

// GenerateLocal ...
func (index *Index) GenerateLocal(remoteIndex *Index) {
	slog.Debug("Generating local index")
	for _, filePath := range index.config.FilePaths {
		slog.Debug("Walk", "path", filePath)

		err := filepath.WalkDir(path.Join(index.config.BasePath, filePath), func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return index.scanSingleFile(path, de)
		})

		if err != nil {
			continue
		}
	}
	for fp, fi := range index.Entries.Files {
		var h HashType
		cachedHash := false
		if remoteFileInfo, ok := remoteIndex.Entries.Files[fp]; ok {
			if remoteFileInfo.Size == fi.Size && remoteFileInfo.ModTime == fi.ModTime {
				h = remoteFileInfo.Hash
				cachedHash = true
			}
		}
		if !cachedHash {
			var err error
			h, err = chunkHash(fp, fi.Size)
			if err != nil {
				slog.Error("Hash calculation failed", "path", fp, "error", err)
				delete(index.Entries.Files, fp)
				continue
			}
		}
		fi.Hash = h
	}
}
