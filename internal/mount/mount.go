// Package mount 把 volume 暴露为 FUSE 文件系统（go-fuse Node API）。
// 按需目录：Readdir/Lookup/Getattr 实时问家里（经 Resolver），
// 只读当前进的这一层，跟家里全盘文件数无关。
// 本地新建/已同步的文件走 volume（读缓存/回源、写标脏即时回写）。
// 暂不支持：rename、unlink（后续按需加）。
package mount

import (
	"context"
	"strings"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"quicsmb/internal/volume"
)

// DirEnt 是 Resolver 返回的单级目录项。
type DirEnt struct {
	Name  string
	Size  int64
	IsDir bool
}

// Resolver 按需解析远端目录/文件状态（qvps 侧经 QUIC 问家里实现）。
type Resolver interface {
	// Readdir 列单级目录（""=根），只读一层。
	Readdir(dir string) ([]DirEnt, error)
	// Stat 查单个路径：ok=false 表示远端不存在（或家里离线）。
	Stat(name string) (isDir bool, size int64, ok bool)
}

type FS struct {
	vol   *volume.Volume
	fetch volume.Fetcher
	res    Resolver

	// Statfs 容量信息（字节）。0 表示未同步，此时 df 回退到报缓存配额。
	totalBytes uint64
	freeBytes  uint64
}

func (f *FS) SetCapacity(total, free uint64) {
	f.totalBytes = total
	f.freeBytes = free
}

type fileNode struct {
	fs.Inode
	f    *FS
	name string
}

// dirNode 是子目录节点：名字为相对 root 的目录路径（""=root）。
type dirNode struct {
	fs.Inode
	f   *FS
	dir string
}

// childrenOf 列 dir 下的直接子项（文件 + 子目录），来源：
// volume 本地文件（新建/写过/重启恢复）+ Resolver 远端实时列表。
func (f *FS) childrenOf(dir string) (files, dirs map[string]bool) {
	files = map[string]bool{}
	dirs = map[string]bool{}
	prefix := dir
	if prefix != "" {
		prefix += "/"
	}
	for name := range f.vol.Files() {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if i := strings.IndexByte(rest, '/'); i < 0 {
			if rest != "" {
				files[rest] = true
			}
		} else if rest[:i] != "" {
			dirs[rest[:i]] = true
		}
	}
	if f.res != nil {
		if des, err := f.res.Readdir(dir); err == nil {
			for _, de := range des {
				if de.IsDir {
					dirs[de.Name] = true
				} else {
					// 本地写过的新版本 size 以 volume 为准（SetFile 只放大不缩小，
					// 且 Readdir 进来时顺手把远端 size 记下来供 Getattr 用）
						full := de.Name
						if dir != "" {
							full = dir + "/" + de.Name
						}
						if _, known := f.vol.Files()[full]; !known {
							f.vol.SetFile(full, de.Size)
						}
						files[de.Name] = true
				}
			}
		}
	}
	return files, dirs
}

func (f *FS) fillFileEntry(name string, out *fuse.EntryOut) {
	sz := f.vol.Size(name)
	if sz == 0 && f.res != nil {
		if isDir, rsz, ok := f.res.Stat(name); ok && !isDir && rsz > 0 {
			f.vol.SetFile(name, rsz)
			sz = rsz
		}
	}
	out.SetAttrTimeout(0)
	out.SetEntryTimeout(0)
	out.Attr.FromStat(&syscall.Stat_t{
		Mode:  syscall.S_IFREG | 0o644,
		Size:  sz,
		Nlink: 1,
	})
}

func (f *FS) fillDirEntry(out *fuse.EntryOut) {
	out.SetAttrTimeout(0)
	out.SetEntryTimeout(0)
	out.Attr.FromStat(&syscall.Stat_t{
		Mode:  syscall.S_IFDIR | 0o755,
		Nlink: 2,
	})
}

func (d *dirNode) fullName(name string) string {
	if d.dir == "" {
		return name
	}
	return d.dir + "/" + name
}

func (d *dirNode) lookupChild(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if ch := d.GetChild(name); ch != nil {
		if fn, ok := ch.Operations().(*fileNode); ok {
			d.f.fillFileEntry(fn.name, out)
		} else {
			d.f.fillDirEntry(out)
		}
		return ch, 0
	}
	full := d.fullName(name)
	files, dirs := d.f.childrenOf(d.dir)
	if !files[name] && !dirs[name] && d.f.res != nil {
		// 本地没见过：实时问家里（家里离线则 ok=false，继续 ENOENT）
		if isDir, _, ok := d.f.res.Stat(full); ok {
			if isDir {
				dirs[name] = true
			} else {
				files[name] = true
			}
		}
	}
	if files[name] {
		ch := d.NewPersistentInode(ctx, &fileNode{f: d.f, name: full}, fs.StableAttr{Mode: syscall.S_IFREG})
		d.AddChild(name, ch, true)
		d.f.fillFileEntry(full, out)
		return ch, 0
	}
	if dirs[name] {
		ch := d.NewPersistentInode(ctx, &dirNode{f: d.f, dir: full}, fs.StableAttr{Mode: syscall.S_IFDIR})
		d.AddChild(name, ch, true)
		d.f.fillDirEntry(out)
		return ch, 0
	}
	return nil, syscall.ENOENT
}

func (d *dirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return d.lookupChild(ctx, name, out)
}

func (d *dirNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	ch := d.GetChild(name)
	if ch == nil {
		full := d.fullName(name)
		ch = d.NewPersistentInode(ctx, &fileNode{f: d.f, name: full}, fs.StableAttr{Mode: syscall.S_IFREG})
		d.AddChild(name, ch, true)
	}
	d.f.fillFileEntry(d.fullName(name), out)
	return ch, nil, 0, 0
}

func (d *dirNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0o755
	out.Nlink = 2
	return 0
}

func (d *dirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	files, dirs := d.f.childrenOf(d.dir)
	var ents []fuse.DirEntry
	for name := range d.Children() {
		if op := d.GetChild(name).Operations(); op != nil {
			if _, ok := op.(*dirNode); ok {
				ents = append(ents, fuse.DirEntry{Name: name, Mode: syscall.S_IFDIR})
			} else {
				ents = append(ents, fuse.DirEntry{Name: name, Mode: syscall.S_IFREG})
			}
			delete(files, name)
			delete(dirs, name)
		}
	}
	for name := range files {
		ents = append(ents, fuse.DirEntry{Name: name, Mode: syscall.S_IFREG})
	}
	for name := range dirs {
		ents = append(ents, fuse.DirEntry{Name: name, Mode: syscall.S_IFDIR})
	}
	return fs.NewListDirStream(ents), 0
}

// rootNode 是挂载入口（dir "" 的别名），供 Mount 使用。
type rootNode = dirNode

func (n *fileNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	sz := n.f.vol.Size(n.name)
	if sz == 0 && n.f.res != nil {
		if isDir, rsz, ok := n.f.res.Stat(n.name); ok && !isDir && rsz > 0 {
			n.f.vol.SetFile(n.name, rsz)
			sz = rsz
		}
	}
	out.Mode = syscall.S_IFREG | 0o644
	out.Size = uint64(sz)
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

// NewWithResolver 带远端按需解析器（qvps 传入经 QUIC 问家里的实现）。
func NewWithResolver(vol *volume.Volume, fetch volume.Fetcher, res Resolver) *FS {
	return &FS{vol: vol, fetch: fetch, res: res}
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

// MountWithResolver 同 Mount，另带远端按需解析器。
func MountWithResolver(mountpoint string, vol *volume.Volume, fetch volume.Fetcher, res Resolver) (*Server, error) {
	f := NewWithResolver(vol, fetch, res)
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
