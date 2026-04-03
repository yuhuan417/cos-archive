package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestSplitMountPathVariants(t *testing.T) {
	tests := map[string][]string{
		"/":             nil,
		".":             nil,
		"":              nil,
		"/a/b":          {"a", "b"},
		"a//b///c":      {"a", "b", "c"},
		"/single/":      {"single"},
		"relative/path": {"relative", "path"},
	}

	for input, want := range tests {
		if got := splitMountPath(input); len(got) != len(want) {
			t.Fatalf("splitMountPath(%q)=%v want=%v", input, got, want)
		} else {
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("splitMountPath(%q)=%v want=%v", input, got, want)
				}
			}
		}
	}

	if got := splitNonEmpty("a//b/c", "/"); len(got) != 3 || got[1] != "b" {
		t.Fatalf("unexpected splitNonEmpty result: %v", got)
	}
}

func TestMountNodeOperations(t *testing.T) {
	root := &mountRootNode{
		tree:    newMountTreeDir(0555),
		nextIno: 10,
	}

	stable := root.nextStable(syscall.S_IFDIR)
	if stable.Ino != 11 || stable.Mode != syscall.S_IFDIR {
		t.Fatalf("unexpected stable attr: %+v", stable)
	}

	var rootAttr fuse.AttrOut
	if errno := root.Getattr(context.Background(), nil, &rootAttr); errno != 0 {
		t.Fatalf("root getattr errno: %v", errno)
	}
	if rootAttr.Mode != 0555 {
		t.Fatalf("unexpected root mode: %o", rootAttr.Mode)
	}

	fileInfo := &FileInfo{Mode: 0644, Size: 123, ModTime: 456}
	fileNode := &mountFileNode{file: &mountTreeFile{
		info:    fileInfo,
		content: []byte("/file.txt on cloud: https://example.invalid/chunk"),
	}}
	var fileAttr fuse.AttrOut
	if errno := fileNode.Getattr(context.Background(), nil, &fileAttr); errno != 0 {
		t.Fatalf("file getattr errno: %v", errno)
	}
	if fileAttr.Size != 123 || fileAttr.Mode != 0644 {
		t.Fatalf("unexpected file attr: %+v", fileAttr)
	}
	if _, _, errno := fileNode.Open(context.Background(), uint32(syscall.O_WRONLY)); errno != syscall.EROFS {
		t.Fatalf("expected EROFS for write open, got %v", errno)
	}
	handle, flags, errno := fileNode.Open(context.Background(), uint32(syscall.O_RDONLY))
	if errno != 0 {
		t.Fatalf("unexpected read open errno: %v", errno)
	}
	fileHandle, ok := handle.(mountFileHandle)
	if !ok || flags != fuse.FOPEN_DIRECT_IO {
		t.Fatalf("unexpected file handle open result: %#v flags=%d", handle, flags)
	}
	if got := string(fileHandle.content); got != "/file.txt on cloud: https://example.invalid/chunk" {
		t.Fatalf("unexpected synthetic file content: %q", got)
	}

	readResult, errno := (mountFileHandle{content: []byte("browse content")}).Read(context.Background(), make([]byte, 6), 7)
	if errno != 0 {
		t.Fatalf("unexpected read errno: %v", errno)
	}
	data, status := readResult.Bytes(nil)
	if status != fuse.OK || string(data) != "conten" {
		t.Fatalf("unexpected read result: data=%q status=%v", data, status)
	}
	readResult, errno = (mountFileHandle{content: []byte("browse content")}).Read(context.Background(), make([]byte, 6), 99)
	if errno != 0 {
		t.Fatalf("unexpected EOF read errno: %v", errno)
	}
	data, status = readResult.Bytes(nil)
	if status != fuse.OK || len(data) != 0 {
		t.Fatalf("unexpected EOF read result: data=%q status=%v", data, status)
	}
	if _, errno := (mountFileHandle{content: []byte("browse content")}).Read(context.Background(), make([]byte, 1), -1); errno != syscall.EINVAL {
		t.Fatalf("expected EINVAL from negative offset read, got %v", errno)
	}

	dirNode := &mountDirNode{tree: newMountTreeDir(0750)}
	var dirAttr fuse.AttrOut
	if errno := dirNode.Getattr(context.Background(), nil, &dirAttr); errno != 0 {
		t.Fatalf("dir getattr errno: %v", errno)
	}
	if dirAttr.Mode != 0750 {
		t.Fatalf("unexpected dir mode: %o", dirAttr.Mode)
	}

	linkNode := &mountSymlinkNode{info: &LinkInfo{LinkTo: "/tmp/target"}}
	var linkAttr fuse.AttrOut
	if errno := linkNode.Getattr(context.Background(), nil, &linkAttr); errno != 0 {
		t.Fatalf("link getattr errno: %v", errno)
	}
	if linkAttr.Mode != 0777 || linkAttr.Size != uint64(len("/tmp/target")) {
		t.Fatalf("unexpected link attr: %+v", linkAttr)
	}
	if target, errno := linkNode.Readlink(context.Background()); errno != 0 || string(target) != "/tmp/target" {
		t.Fatalf("unexpected readlink result: target=%q errno=%v", target, errno)
	}
}

func TestMountOnAddBuildsChildren(t *testing.T) {
	root := &mountRootNode{
		tree: buildMountTree(Config{}, DirEnt{
			Files: FileInfoMap{
				"/dir/file.txt": {Size: 10, Mode: 0644},
				"/root.txt":     {Size: 1, Mode: 0600},
			},
			Dirs: DirInfoMap{
				"/dir": {Mode: 0750},
			},
			Links: LinkInfoMap{
				"/dir/link": {LinkTo: "/tmp/target"},
			},
		}),
		nextIno: 1,
	}

	_ = fs.NewNodeFS(root, &fs.Options{})

	dir := root.GetChild("dir")
	if dir == nil {
		t.Fatal("expected mounted dir child")
	}
	if dir.StableAttr().Mode != syscall.S_IFDIR {
		t.Fatalf("unexpected dir stable mode: %o", dir.StableAttr().Mode)
	}

	file := dir.GetChild("file.txt")
	if file == nil {
		t.Fatal("expected mounted file child")
	}
	if file.StableAttr().Mode != syscall.S_IFREG {
		t.Fatalf("unexpected file stable mode: %o", file.StableAttr().Mode)
	}
	if got := file.Path(root.EmbeddedInode()); got != "dir/file.txt" {
		t.Fatalf("unexpected file path: %s", got)
	}

	link := dir.GetChild("link")
	if link == nil {
		t.Fatal("expected mounted symlink child")
	}
	if link.StableAttr().Mode != syscall.S_IFLNK {
		t.Fatalf("unexpected link stable mode: %o", link.StableAttr().Mode)
	}

	rootFile := root.GetChild("root.txt")
	if rootFile == nil {
		t.Fatal("expected root-level file child")
	}
}

type fakeMountServer struct {
	unmountErr error
	unmounted  chan struct{}
}

func (f *fakeMountServer) Unmount() error {
	select {
	case <-f.unmounted:
	default:
		close(f.unmounted)
	}
	return f.unmountErr
}

func (f *fakeMountServer) Wait() {
	<-f.unmounted
}

func TestMountFilesBranches(t *testing.T) {
	t.Run("missing browse index", func(t *testing.T) {
		if err := mountFiles(context.Background(), Config{
			TargetDir:  t.TempDir(),
			Index:      "meta.json",
			MountPoint: filepath.Join(t.TempDir(), "mnt"),
		}); err == nil {
			t.Fatal("expected missing index error")
		}
	})

	t.Run("mount point mkdir error", func(t *testing.T) {
		targetDir := t.TempDir()
		index := NewIndex(Config{}, nil)
		if err := index.Save(filepath.Join(targetDir, "meta.json")); err != nil {
			t.Fatal(err)
		}
		mountPoint := filepath.Join(t.TempDir(), "mount-file")
		if err := os.WriteFile(mountPoint, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := mountFiles(context.Background(), Config{
			TargetDir:  targetDir,
			Index:      "meta.json",
			MountPoint: mountPoint,
		}); err == nil {
			t.Fatal("expected mkdir error for file mount point")
		}
	})

	t.Run("mount success and cancellation", func(t *testing.T) {
		targetDir := t.TempDir()
		index := NewIndex(Config{}, nil)
		index.Entries.Files["/file.txt"] = &FileInfo{Size: 1}
		if err := index.Save(filepath.Join(targetDir, "meta.json")); err != nil {
			t.Fatal(err)
		}

		origMountFS := mountFS
		defer func() { mountFS = origMountFS }()

		var gotRoot *mountRootNode
		var gotOptions *fs.Options
		mountFS = func(mountPoint string, root *mountRootNode, options *fs.Options) (mountServer, error) {
			gotRoot = root
			gotOptions = options
			return &fakeMountServer{unmounted: make(chan struct{})}, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- mountFiles(ctx, Config{
				TargetDir:  targetDir,
				Index:      "meta.json",
				MountPoint: filepath.Join(t.TempDir(), "mnt"),
			})
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("unexpected mountFiles error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("mountFiles did not return after cancellation")
		}

		if gotRoot == nil || gotRoot.tree == nil {
			t.Fatal("expected mount root to be built")
		}
		if gotOptions == nil || gotOptions.MountOptions.FsName != "cos-archive" {
			t.Fatalf("unexpected mount options: %#v", gotOptions)
		}
	})

	t.Run("mount deadline exceeded", func(t *testing.T) {
		targetDir := t.TempDir()
		index := NewIndex(Config{}, nil)
		if err := index.Save(filepath.Join(targetDir, "meta.json")); err != nil {
			t.Fatal(err)
		}

		origMountFS := mountFS
		defer func() { mountFS = origMountFS }()
		mountFS = func(mountPoint string, root *mountRootNode, options *fs.Options) (mountServer, error) {
			return &fakeMountServer{unmounted: make(chan struct{})}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		err := mountFiles(ctx, Config{
			TargetDir:  targetDir,
			Index:      "meta.json",
			MountPoint: filepath.Join(t.TempDir(), "mnt"),
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	})

	t.Run("mount stub error", func(t *testing.T) {
		targetDir := t.TempDir()
		index := NewIndex(Config{}, nil)
		if err := index.Save(filepath.Join(targetDir, "meta.json")); err != nil {
			t.Fatal(err)
		}

		origMountFS := mountFS
		defer func() { mountFS = origMountFS }()
		sentinel := errors.New("mount failed")
		mountFS = func(mountPoint string, root *mountRootNode, options *fs.Options) (mountServer, error) {
			return nil, sentinel
		}

		err := mountFiles(context.Background(), Config{
			TargetDir:  targetDir,
			Index:      "meta.json",
			MountPoint: filepath.Join(t.TempDir(), "mnt"),
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected mount stub error, got %v", err)
		}
	})
}
