package main

import "testing"

func TestBuildMountTree(t *testing.T) {
	entries := DirEnt{
		Files: FileInfoMap{
			"/backup/file.txt": {Size: 12, Mode: 0644},
			"/orphan.txt":      {Size: 1, Mode: 0600},
		},
		Dirs: DirInfoMap{
			"/backup": {Mode: 0755},
		},
		Links: LinkInfoMap{
			"/backup/link": {LinkTo: "/tmp/target"},
		},
	}

	tree := buildMountTree(Config{}, entries)
	backupDir, ok := tree.dirs["backup"]
	if !ok {
		t.Fatal("missing backup dir")
	}
	if backupDir.mode != 0755 {
		t.Fatalf("unexpected backup dir mode: %v", backupDir.mode)
	}
	if _, ok := backupDir.files["file.txt"]; !ok {
		t.Fatal("missing file.txt in backup dir")
	}
	if got := string(backupDir.files["file.txt"].content); got != "/backup/file.txt" {
		t.Fatalf("unexpected synthesized file content: %q", got)
	}
	if _, ok := backupDir.links["link"]; !ok {
		t.Fatal("missing link in backup dir")
	}
	if _, ok := tree.files["orphan.txt"]; !ok {
		t.Fatal("missing orphan file in root")
	}
}
