package main

import "testing"

func TestBuildDirMap(t *testing.T) {
	entries := *NewDirEnt()
	entries.Dirs["/backup"] = &DirInfo{Mode: 0755}
	entries.Files["/backup/file.txt"] = &FileInfo{Size: 1}
	entries.Files["/orphan.txt"] = &FileInfo{Size: 2}
	entries.Links["/backup/link"] = &LinkInfo{LinkTo: "/tmp/target"}

	dm := buildDirMap(entries)

	if _, ok := dm["/backup"]; !ok {
		t.Fatal("missing /backup directory map")
	}
	if _, ok := dm["/backup"].Files["file.txt"]; !ok {
		t.Fatal("missing nested file entry")
	}
	if _, ok := dm["/backup"].Links["link"]; !ok {
		t.Fatal("missing nested link entry")
	}
	if _, ok := dm["/"].Files["/orphan.txt"]; !ok {
		t.Fatal("missing orphan file entry in root")
	}
}
