package main

import (
	"context"
	"errors"
	"os"
	"path"
	"sort"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type mountTreeDir struct {
	mode  os.FileMode
	dirs  map[string]*mountTreeDir
	files map[string]*FileInfo
	links map[string]*LinkInfo
}

func newMountTreeDir(mode os.FileMode) *mountTreeDir {
	return &mountTreeDir{
		mode:  mode,
		dirs:  make(map[string]*mountTreeDir),
		files: make(map[string]*FileInfo),
		links: make(map[string]*LinkInfo),
	}
}

func buildMountTree(entries DirEnt) *mountTreeDir {
	root := newMountTreeDir(0555)
	ensureDir := func(fullPath string, mode os.FileMode) *mountTreeDir {
		if fullPath == "/" || fullPath == "." || fullPath == "" {
			return root
		}
		parts := splitMountPath(fullPath)
		current := root
		for i, part := range parts {
			next, ok := current.dirs[part]
			if !ok {
				next = newMountTreeDir(0555)
				current.dirs[part] = next
			}
			if i == len(parts)-1 && mode != 0 {
				next.mode = mode
			}
			current = next
		}
		return current
	}

	dirPaths := make([]string, 0, len(entries.Dirs))
	for p := range entries.Dirs {
		dirPaths = append(dirPaths, p)
	}
	sort.Strings(dirPaths)
	for _, dirPath := range dirPaths {
		ensureDir(path.Dir(dirPath), 0)
		ensureDir(dirPath, entries.Dirs[dirPath].Mode)
	}

	for filePath, fi := range entries.Files {
		parent := ensureDir(path.Dir(filePath), 0)
		parent.files[path.Base(filePath)] = fi
	}
	for linkPath, li := range entries.Links {
		parent := ensureDir(path.Dir(linkPath), 0)
		parent.links[path.Base(linkPath)] = li
	}

	return root
}

func splitMountPath(p string) []string {
	if p == "/" || p == "." || p == "" {
		return nil
	}
	p = path.Clean(p)
	if p == "/" {
		return nil
	}
	if p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return nil
	}
	parts := make([]string, 0)
	for _, part := range splitNonEmpty(p, "/") {
		parts = append(parts, part)
	}
	return parts
}

func splitNonEmpty(s string, sep string) []string {
	raw := make([]string, 0)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i:i+1] == sep {
			if start < i {
				raw = append(raw, s[start:i])
			}
			start = i + 1
		}
	}
	return raw
}

type mountRootNode struct {
	fs.Inode
	tree    *mountTreeDir
	nextIno uint64
}

var _ = (fs.InodeEmbedder)((*mountRootNode)(nil))
var _ = (fs.NodeOnAdder)((*mountRootNode)(nil))
var _ = (fs.NodeGetattrer)((*mountRootNode)(nil))

func (n *mountRootNode) nextStable(mode uint32) fs.StableAttr {
	n.nextIno++
	return fs.StableAttr{Mode: mode, Ino: n.nextIno}
}

func (n *mountRootNode) OnAdd(ctx context.Context) {
	n.addDirChildren(ctx, &n.Inode, n.tree)
}

func (n *mountRootNode) addDirChildren(ctx context.Context, inode *fs.Inode, tree *mountTreeDir) {
	dirNames := make([]string, 0, len(tree.dirs))
	for name := range tree.dirs {
		dirNames = append(dirNames, name)
	}
	sort.Strings(dirNames)
	for _, name := range dirNames {
		childTree := tree.dirs[name]
		dirNode := &mountDirNode{root: n, tree: childTree}
		child := inode.NewPersistentInode(ctx, dirNode, n.nextStable(syscall.S_IFDIR))
		inode.AddChild(name, child, false)
		n.addDirChildren(ctx, child, childTree)
	}

	fileNames := make([]string, 0, len(tree.files))
	for name := range tree.files {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		fileNode := &mountFileNode{info: tree.files[name]}
		child := inode.NewPersistentInode(ctx, fileNode, n.nextStable(syscall.S_IFREG))
		inode.AddChild(name, child, false)
	}

	linkNames := make([]string, 0, len(tree.links))
	for name := range tree.links {
		linkNames = append(linkNames, name)
	}
	sort.Strings(linkNames)
	for _, name := range linkNames {
		linkNode := &mountSymlinkNode{info: tree.links[name]}
		child := inode.NewPersistentInode(ctx, linkNode, n.nextStable(syscall.S_IFLNK))
		inode.AddChild(name, child, false)
	}
}

func (n *mountRootNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(n.tree.mode.Perm())
	out.SetTimeout(time.Second)
	return 0
}

type mountDirNode struct {
	fs.Inode
	root *mountRootNode
	tree *mountTreeDir
}

var _ = (fs.InodeEmbedder)((*mountDirNode)(nil))
var _ = (fs.NodeGetattrer)((*mountDirNode)(nil))

func (n *mountDirNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(n.tree.mode.Perm())
	out.SetTimeout(time.Second)
	return 0
}

type mountFileNode struct {
	fs.Inode
	info *FileInfo
}

var _ = (fs.InodeEmbedder)((*mountFileNode)(nil))
var _ = (fs.NodeGetattrer)((*mountFileNode)(nil))
var _ = (fs.NodeOpener)((*mountFileNode)(nil))

func (n *mountFileNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(n.info.Mode.Perm())
	out.Size = uint64(n.info.Size)
	out.Mtime = uint64(n.info.ModTime)
	out.Ctime = uint64(n.info.ModTime)
	out.Atime = uint64(n.info.ModTime)
	out.SetTimeout(time.Second)
	return 0
}

func (n *mountFileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&fuse.O_ANYWRITE != 0 {
		return nil, 0, syscall.EROFS
	}
	return mountFileHandle{}, fuse.FOPEN_DIRECT_IO, 0
}

type mountFileHandle struct{}

var _ = (fs.FileHandle)((mountFileHandle{}))
var _ = (fs.FileReader)((mountFileHandle{}))

func (mountFileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	return nil, syscall.EIO
}

type mountSymlinkNode struct {
	fs.Inode
	info *LinkInfo
}

var _ = (fs.InodeEmbedder)((*mountSymlinkNode)(nil))
var _ = (fs.NodeGetattrer)((*mountSymlinkNode)(nil))
var _ = (fs.NodeReadlinker)((*mountSymlinkNode)(nil))

func (n *mountSymlinkNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0777
	out.Size = uint64(len(n.info.LinkTo))
	out.SetTimeout(time.Second)
	return 0
}

func (n *mountSymlinkNode) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	return []byte(n.info.LinkTo), 0
}

func mountFiles(ctx context.Context, config Config) error {
	index, err := loadBrowseIndex(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.MountPoint, 0755); err != nil {
		return err
	}
	root := &mountRootNode{
		tree:    buildMountTree(index.Entries),
		nextIno: 1,
	}
	timeout := time.Second
	server, err := fs.Mount(config.MountPoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			Options:       []string{"ro"},
			FsName:        "cos-archive",
			Name:          "cos-archive",
			DisableXAttrs: true,
		},
		AttrTimeout:  &timeout,
		EntryTimeout: &timeout,
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = server.Unmount()
	}()
	server.Wait()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
