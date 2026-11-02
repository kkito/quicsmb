// Package mount 把 volume 暴露为 FUSE 文件系统（go-fuse Node API）。
// 文件自动具现：Lookup 到不存在的名字即创建节点，读写走 volume。
package mount

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"quicsmb/internal/volume"
)

type FS struct {
	vol   *volume.Volume
	fetch volume.Fetcher
}

type rootNode struct {
	fs.Inode
	f *FS
}

type fileNode struct {
	fs.Inode
	f    *FS
	name string
}

func (r *rootNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	ch := r.GetChild(name)
	if ch == nil {
		ch = r.NewPersistentInode(ctx, &fileNode{f: r.f, name: name}, fs.StableAttr{Mode: syscall.S_IFREG})
		r.AddChild(name, ch, true)
	}
	out.SetAttrTimeout(0)
	out.SetEntryTimeout(0)
	out.Attr.FromStat(&syscall.Stat_t{
		Mode: syscall.S_IFREG | 0o644,
		Size: r.f.vol.Size(name),
		Nlink: 1,
	})
	return ch, 0
}

func (r *rootNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0o755
	out.Nlink = 2
	return 0
}

func (r *rootNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	var ents []fuse.DirEntry
	for name := range r.Children() {
		ents = append(ents, fuse.DirEntry{Name: name, Mode: syscall.S_IFREG})
	}
	return fs.NewListDirStream(ents), 0
}

func (n *fileNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0o644
	out.Size = uint64(n.f.vol.Size(n.name))
	out.Nlink = 1
	return 0
}

func (n *fileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, 0, 0
}

func (n *fileNode) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	got, err := n.f.vol.ReadAt(n.name, off, dest, n.f.fetch)
	if err != nil {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(dest[:got]), 0
}

func (n *fileNode) Write(ctx context.Context, fh fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	m, err := n.f.vol.WriteAt(n.name, off, data, nowUnix())
	if err != nil {
		return 0, syscall.EIO
	}
	return uint32(m), 0
}

func New(vol *volume.Volume, fetch volume.Fetcher) *FS {
	return &FS{vol: vol, fetch: fetch}
}

type Server struct {
	srv *fuse.Server
}

func Mount(mountpoint string, vol *volume.Volume, fetch volume.Fetcher) (*Server, error) {
	f := New(vol, fetch)
	root := &rootNode{f: f}
	srv, err := fs.Mount(mountpoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{AllowOther: true, Name: "qvfs"},
	})
	if err != nil {
		return nil, err
	}
	return &Server{srv: srv}, nil
}

func (s *Server) Close() {
	s.srv.Unmount()
}

func (s *Server) Wait() {
	s.srv.Wait()
}
