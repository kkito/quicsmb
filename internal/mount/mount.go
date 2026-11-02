// Package mount 把 volume 暴露为 FUSE 文件系统（go-fuse Node API）。
// Create 具现文件（直接在 root 下建节点，写走 volume 标脏），读写走 volume。
// 暂不支持：子目录、rename、unlink（后续按需加）。
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

	// Statfs 容量信息（字节）。0 表示未同步，此时 df 回退到报缓存配额。
	totalBytes uint64
	freeBytes  uint64
}

func (f *FS) SetCapacity(total, free uint64) {
	f.totalBytes = total
	f.freeBytes = free
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

func (r *rootNode) fillEntry(name string, out *fuse.EntryOut) {
	out.SetAttrTimeout(0)
	out.SetEntryTimeout(0)
	out.Attr.FromStat(&syscall.Stat_t{
		Mode:  syscall.S_IFREG | 0o644,
		Size:  r.f.vol.Size(name),
		Nlink: 1,
	})
}

func (r *rootNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if ch := r.GetChild(name); ch != nil {
		r.fillEntry(name, out)
		return ch, 0
	}
	return nil, syscall.ENOENT
}

func (r *rootNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	ch := r.GetChild(name)
	if ch == nil {
		ch = r.NewPersistentInode(ctx, &fileNode{f: r.f, name: name}, fs.StableAttr{Mode: syscall.S_IFREG})
		r.AddChild(name, ch, true)
	}
	r.fillEntry(name, out)
	return ch, nil, 0, 0
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

func (n *fileNode) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	// 只支持 truncate 缩小（写后定长场景）；放大走 Write 自然扩展。
	if in.Valid&fuse.FATTR_SIZE != 0 {
		cur := n.f.vol.Size(n.name)
		if int64(in.Size) < cur {
			n.f.vol.Truncate(n.name, int64(in.Size))
		}
		if err := n.f.vol.TouchDirty(n.name, nowUnix()); err != nil {
			return syscall.EIO
		}
	}
	out.Mode = syscall.S_IFREG | 0o644
	out.Size = uint64(n.f.vol.Size(n.name))
	out.Nlink = 1
	return 0
}

func (r *rootNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	const bsize = 4096
	out.Bsize = bsize
	out.Frsize = bsize
	total := r.f.totalBytes
	free := r.f.freeBytes
	if total == 0 {
		// 未同步上游容量前：按缓存用量兜底，避免 df 显示全 0。
		used, _ := r.f.vol.Usage()
		total = uint64(used) + 1<<30
		free = 1 << 30
	}
	out.Blocks = total / bsize
	if free > total {
		free = total
	}
	out.Bfree = free / bsize
	out.Bavail = free / bsize
	out.Files = 1 << 20
	out.Ffree = 1 << 19
	out.NameLen = 255
	return 0
}

func New(vol *volume.Volume, fetch volume.Fetcher) *FS {
	return &FS{vol: vol, fetch: fetch}
}

type Server struct {
	srv *fuse.Server
	fs  *FS
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
	return &Server{srv: srv, fs: f}, nil
}

func FSRef(s *Server) *FS { return s.fs }

func (s *Server) Close() {
	s.srv.Unmount()
}

func (s *Server) Wait() {
	s.srv.Wait()
}
