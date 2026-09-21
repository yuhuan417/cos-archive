package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

var browseTemplates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// DirMap for browse
type DirMap = map[string]DirEnt

type dirListItem struct {
	Href     string
	Name     string
	Mode     string
	Size     int64
	Modified string
}

type dirPageData struct {
	Path  string
	Items []dirListItem
}

type filePageData struct {
	Path    string
	Message string
}

type linkPageData struct {
	Path   string
	LinkTo string
}

func directoryItems(m DirEnt) []dirListItem {
	items := make([]dirListItem, 0, len(m.Dirs)+len(m.Files)+len(m.Links))

	keys := make([]string, 0, len(m.Dirs))
	for k := range m.Dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fi := m.Dirs[k]
		items = append(items, dirListItem{
			Href:     url.PathEscape(k) + "/",
			Name:     k + "/",
			Mode:     fi.Mode.String(),
			Size:     0,
			Modified: "",
		})
	}

	keys = make([]string, 0, len(m.Files))
	for k := range m.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fi := m.Files[k]
		items = append(items, dirListItem{
			Href:     url.PathEscape(k),
			Name:     k,
			Mode:     fi.Mode.String(),
			Size:     fi.Size,
			Modified: time.Unix(fi.ModTime, 0).String(),
		})
	}

	keys = make([]string, 0, len(m.Links))
	for k := range m.Links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		items = append(items, dirListItem{
			Href:     url.PathEscape(k),
			Name:     k,
			Mode:     "",
			Size:     0,
			Modified: "",
		})
	}

	return items
}

func buildCloudURL(config Config, fi *FileInfo) string {
	if config.COS.URL == "" {
		return ""
	}
	u, err := url.Parse(config.COS.URL)
	if err != nil {
		return ""
	}
	u.Path = path.Join(u.Path, config.COS.ChunkPrefix, chunkPath(ChunkKey{fi.Size, fi.Hash}))
	return u.String()
}

func browseFileMessage(config Config, fi *FileInfo, p string) string {
	if cloudURL := buildCloudURL(config, fi); cloudURL != "" {
		return p + " on cloud: " + cloudURL
	}
	return p
}

func renderDir(m DirEnt, p string, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	return browseTemplates.ExecuteTemplate(w, "dir.html", dirPageData{
		Path:  p,
		Items: directoryItems(m),
	})
}

func renderFile(config Config, fi *FileInfo, p string, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	return browseTemplates.ExecuteTemplate(w, "file.html", filePageData{
		Path:    p,
		Message: browseFileMessage(config, fi, p),
	})
}

func renderLink(fi *LinkInfo, p string, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	return browseTemplates.ExecuteTemplate(w, "link.html", linkPageData{
		Path:   p,
		LinkTo: string(fi.LinkTo),
	})
}

func browseHandler(config Config, entries DirEnt, dm DirMap) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := url.PathUnescape(r.URL.Path)
		if err != nil {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		p = strings.TrimSuffix(p, "/")
		if p == "" {
			p = "/"
		}

		if m, ok := dm[p]; ok {
			if err := renderDir(m, p, w); err != nil {
				http.Error(w, fmt.Sprintf("render dir: %v", err), http.StatusInternalServerError)
			}
			return
		}

		if fi, ok := entries.Files[p]; ok {
			if err := renderFile(config, fi, p, w); err != nil {
				http.Error(w, fmt.Sprintf("render file: %v", err), http.StatusInternalServerError)
			}
			return
		}
		if fi, ok := entries.Links[p]; ok {
			if err := renderLink(fi, p, w); err != nil {
				http.Error(w, fmt.Sprintf("render link: %v", err), http.StatusInternalServerError)
			}
			return
		}
		http.Error(w, "404 not found.", http.StatusNotFound)
	})
}

func loadBrowseIndex(config Config) (*Index, error) {
	index := NewIndex(config, nil)
	paths := []string{
		path.Join(config.TargetDir, config.Index+".remote"),
		path.Join(config.TargetDir, config.Index),
	}
	for _, p := range paths {
		if err := index.Load(p); err == nil {
			return index, nil
		}
	}
	return nil, fmt.Errorf("can't load browse index from target dir")
}

func buildDirMap(entries DirEnt) DirMap {
	dm := make(DirMap)
	for fp, fi := range entries.Files {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := entries.Dirs[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Files[bn] = fi
	}
	for fp, fi := range entries.Dirs {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := entries.Dirs[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Dirs[bn] = fi
	}
	for fp, fi := range entries.Links {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := entries.Dirs[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Links[bn] = fi
	}
	return dm
}

func browseFiles(ctx context.Context, config Config) error {
	index, err := loadBrowseIndex(config)
	if err != nil {
		return err
	}
	dm := buildDirMap(index.Entries)

	mux := http.NewServeMux()
	mux.Handle("/", browseHandler(config, index.Entries, dm))
	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
