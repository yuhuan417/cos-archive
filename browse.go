package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// DirMap for browse
type DirMap = map[string]DirEnt

// NewDirEnt ...
func NewDirEnt() *DirEnt {
	d := DirEnt{}
	d.Files = make(FileInfoMap)
	d.Dirs = make(DirInfoMap)
	d.Links = make(LinkInfoMap)
	return &d
}

func handleDir(m DirEnt, p string, w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
<title>Directory listing for %s</title>
</head>
<body>
<h1>Directory listing for %s</h1>
<hr>
<table style="float:left" border="1">
  <thead>
    <tr>
      <th>Filename</th>
      <th>Mode</th>
      <th>Size <small>(bytes)</small></th>
      <th>Date Modified</th>
    </tr>
  </thead>
  <tbody>
`, p, p)

	keys := make([]string, 0, len(m.Dirs))
	for k := range m.Dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fi := m.Dirs[k]
		alink := url.PathEscape(k) + "/"
		bn := k + "/"
		fmt.Fprintf(w, `
      <tr>
        <td><a href="%s">%s</a></td>
        <td>%s</td>
        <td>%d</td>
        <td>%s</td>
      </tr>`, alink, bn, fi.Mode.String(), 0, time.Now().String())
	}
	keys = make([]string, 0, len(m.Files))
	for k := range m.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fi := m.Files[k]
		alink := url.PathEscape(k)
		bn := k
		fmt.Fprintf(w, `
      <tr>
        <td><a href="%s">%s</a></td>
        <td>%s</td>
        <td>%d</td>
        <td>%s</td>
      </tr>`, alink, bn, fi.Mode.String(), fi.Size, time.Unix(fi.ModTime, 0).String())
	}
	keys = make([]string, 0, len(m.Links))
	for k := range m.Links {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		alink := url.PathEscape(k)
		bn := k
		fmt.Fprintf(w, `
      <tr>
        <td><a href="%s">%s</a></td>
        <td>%s</td>
        <td>%d</td>
        <td>%s</td>
      </tr>`, alink, bn, "", 0, time.Now().String())
	}
	fmt.Fprintf(w, `
</tbody>
</table>
</body>
</html>`)
}

func handleFile(config Config, fi *RFileInfo, p string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	fmt.Fprintf(w, `
<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
<title>File %s</title>
</head>
<body>`, p)

	u, _ := url.Parse(config.COS.URL)
	u.Path = path.Join(u.Path, config.COS.ChunkPrefix, chunkPath(ChunkKey{fi.Size, fi.Hash}))
	s := u.String()
	fmt.Fprintf(w, "%s on cloud: %s", p, s)
	fmt.Fprintf(w, "</body></html>")
}

func handleLink(config Config, fi *LinkInfo, p string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	fmt.Fprintf(w, `
<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
<title>File %s</title>
</head>
<body>`, p)

	fmt.Fprintf(w, "%s links to %s", p, fi.LinkTo)
	fmt.Fprintf(w, "</body></html>")
	return
}

func browseHandler(config Config, entries DirEnt, dm DirMap) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := url.PathUnescape(r.URL.Path)
		p = strings.TrimSuffix(p, "/")
		if p == "" {
			p = "/"
		}

		m, ok := dm[p]
		if ok {
			handleDir(m, p, w, r)
			return
		}

		if fi, ok := entries.Files[p]; ok {
			handleFile(config, fi, p, w, r)
			return
		}
		if fi, ok := entries.Links[p]; ok {
			handleLink(config, fi, p, w, r)
			return
		}
		http.Error(w, "404 not found.", http.StatusNotFound)
		return

	})
}

func browseFiles(config Config) {
	index := NewIndex(config)
	index.Load(path.Join(config.TargetDir, config.Index))
	dm := make(DirMap)
	for fp, fi := range index.Entries.Files {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := index.Entries.Files[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Files[bn] = fi
	}
	for fp, fi := range index.Entries.Dirs {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := index.Entries.Dirs[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Dirs[bn] = fi
	}
	for fp, fi := range index.Entries.Links {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := index.Entries.Links[dp]; !ok {
			dp = "/"
			bn = fp
		}

		if _, ok := dm[dp]; !ok {
			dm[dp] = *NewDirEnt()
		}
		dm[dp].Links[bn] = fi
	}

	http.Handle("/", browseHandler(config, index.Entries, dm))
	log.Println("Server started at port ", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}
