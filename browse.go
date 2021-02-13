package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"time"
)

// DirMap for browse
type DirMap = map[string]FileInfoMap

func handleDir(m FileInfoMap, p string, w http.ResponseWriter, r *http.Request) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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

	for _, k := range keys {
		fi := m[k]
		alink := url.PathEscape(k)
		bn := k
		if m[k].IsDir {
			bn = bn + "/"
		}
		fmt.Fprintf(w, `
      <tr>
        <td><a href="%s">%s</a></td>
        <td>%s</td>
        <td>%d</td>
        <td>%s</td>
      </tr>`, alink, bn, fi.Mode.String(), fi.Size, time.Unix(fi.ModTime, 0).String())
	}

	fmt.Fprintf(w, `
</tbody>
</table>
</body>
</html>`)
}

func handleFile(config Config, fi *FileInfo, p string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Content-Type-Options", "no-sniff")
	if fi.LinkTo != "" {
		fmt.Fprintf(w, "%s links to %s", p, fi.LinkTo)
		return
	}
	u, _ := url.Parse(config.COS.URL)
	u.Path = path.Join(u.Path, config.COS.ChunkPrefix, p)
	s := u.String()
	fmt.Fprintf(w, "%s on cloud: %s", p, s)
}

func browseHandler(config Config, fm FileInfoMap, dm DirMap) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := url.PathUnescape(r.URL.Path)
		fmt.Println("path:", p)
		if p == "" {
			p = "/"
		}

		m, ok := dm[p]
		if ok {
			handleDir(m, p, w, r)
			return
		}
		if fi, ok := fm[p]; ok {
			handleFile(config, fi, p, w, r)
			return
		}
		http.Error(w, "404 not found.", http.StatusNotFound)
		return

	})
}

func browseFiles(config Config) {
	index := loadIndex(path.Join(config.TargetDir, config.Index))
	dm := make(DirMap)
	for fp, fi := range index.Files {
		dp := path.Dir(fp)
		bn := path.Base(fp)
		if _, ok := index.Files[dp]; !ok {
			dp = ""
			bn = fp
		}
		k := dp + "/"
		if _, ok := dm[k]; !ok {
			dm[k] = make(FileInfoMap)
		}
		dm[k][bn] = fi
	}

	http.Handle("/", browseHandler(config, index.Files, dm))
	log.Println("Server started at port ", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}
