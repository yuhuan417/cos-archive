package main

import (
	"log"
	"net/http"
	"path"

	"github.com/yuhuan417/go-billy/memfs"
)

func browseFile(config Config) {
	fs := memfs.New()
	index := loadIndex(path.Join(config.TargetDir, config.Index))
	for fp, fi := range index.Files {
		if fi.IsDir {
			err := fs.MkdirAll(fp, fi.Mode)
			if err != nil {
				log.Fatalln("MkdirAll error:", fp, err)
			}
			continue
		}
		if fi.LinkTo != "" {
			err := fs.Symlink(fi.LinkTo, fp)
			if err != nil {
				log.Fatalln("Symlink error:", fp, fi.LinkTo, err)
			}
			continue
		}
	}
	for fp, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		f, err := fs.Open(fp, fi.Mode)
		if err != nil {
			log.Fatal("Open error:", fp, err)
		}

		f.WriteString("haha")
		f.Close()

		// This memfs doesn't support mtime, maybe enable later.
		// now := time.Now()
		// mtime := time.Unix(fi.ModTime, 0)

		// err = fs.Chtimes(fp, now, mtime)
		// if err != nil {
		// 	log.Fatal("Chtimes error:", fp, err)
		// }
	}

	log.Fatal(http.ListenAndServe(config.Port, fs))
}
