package main

import (
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"unicode/utf8"
)

// localNameOK reports whether a path can be stored in the index. The index is
// JSON, so every name has to survive a UTF-8 round-trip, while Linux happily
// accepts any byte in a filename. A single mojibake entry would otherwise make
// the whole index unwritable.
func localNameOK(p string) bool {
	return utf8.ValidString(p)
}

// reportSkippedLocal names the entry exactly as it exists on disk. The raw
// path is logged as-is, plus a quoted form that escapes the offending bytes,
// since the raw one is unreadable in a terminal and the quoted one is what
// makes the file findable again.
func reportSkippedLocal(kind string, p string) {
	slog.Error("Skipping local entry with a name that is not valid UTF-8",
		"kind", kind, "localPath", p, "escaped", strconv.Quote(p))
}

func (index *Index) scanSingleFile(path string, de fs.DirEntry) error {
	if !localNameOK(path) {
		index.skippedLocal = append(index.skippedLocal, path)
		kind := "file"
		if de.IsDir() {
			kind = "dir"
		} else if de.Type()&fs.ModeSymlink != 0 {
			kind = "symlink"
		}
		reportSkippedLocal(kind, path)
		if de.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
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
		// The target is arbitrary bytes too, and it is stored in the index
		// just like a name is.
		if !localNameOK(link) {
			index.skippedLocal = append(index.skippedLocal, path)
			reportSkippedLocal("symlink target", path)
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
	index.skippedLocal = nil
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

	if n := len(index.skippedLocal); n > 0 {
		slog.Error("Local entries were left out of the index because their names are not valid UTF-8",
			"count", n, "hint", "each one is logged above with its escaped path")
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
