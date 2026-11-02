// Command qvps 是 VPS 侧 daemon：QUIC server + volume 缓存 + FUSE 挂载 + Web 面板。
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
	"quicsmb/internal/mount"
	"quicsmb/internal/quic"
	"quicsmb/internal/session"
	"quicsmb/internal/volume"
	"quicsmb/internal/web"
)

type qvfsMgr struct {
	reg *session.Registry
	vol *volume.Volume
}

func (m *qvfsMgr) List() []session.Entry         { return m.reg.List() }
func (m *qvfsMgr) Logs(l int) []session.LogEntry { return m.reg.Logs(l) }
func (m *qvfsMgr) Count() int                    { return m.reg.Count() }
func (m *qvfsMgr) Totals() (uint64, uint64)      { return m.reg.Totals() }

func (m *qvfsMgr) Qvfs() map[string]any {
	var used int64
	var n, dirty int
	var rate float64
	if m.vol != nil {
		used, n = m.vol.Usage()
		rate = m.vol.HitRate()
		dirty = m.vol.DirtyCount()
	}
	return map[string]any{"blocks": n, "usedBytes": used, "dirty": dirty, "hitRate": rate}
}

func main() {
	var mountpoint, root, addr string
	var port int
	flag.StringVar(&mountpoint, "mount", "", "FUSE 挂载点（为空则只跑 QUIC+面板）")
	flag.StringVar(&root, "root", "/var/lib/qvfs", "缓存/元数据目录")
	flag.StringVar(&addr, "web", ":8080", "面板监听地址")
	flag.IntVar(&port, "port", 4433, "QUIC 监听端口")
	flag.Parse()

	key := []byte(os.Getenv("QVFS_KEY"))
	if len(key) == 0 {
		key = []byte("dev-key-change-me")
	}

	meta, err := metastore.Load(root+"/dirty.json", chunk.DefaultBlockSize)
	if err != nil {
		log.Fatalf("metastore: %v", err)
	}
	vol := volume.New(chunk.DefaultBlockSize, cache.New(20<<30, chunk.DefaultBlockSize), meta)

	fetch := func(file string, index uint64) ([]byte, error) {
		return make([]byte, chunk.DefaultBlockSize), nil
	}

	var mnt *mount.Server
	if mountpoint != "" {
		mnt, err = mount.Mount(mountpoint, vol, fetch)
		if err != nil {
			log.Fatalf("mount: %v", err)
		}
		defer mnt.Close()
		log.Printf("qvps: mounted %s", mountpoint)
	}

	lis, err := quic.Listen(port, "qvfs/1", "", "")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := lis.Accept(context.Background())
			if err != nil {
				return
			}
			go serveQUIC(c, key, vol)
		}
	}()

	m := &qvfsMgr{reg: session.New(1024), vol: vol}
	mux := web.NewRouter(m, "qvfs", "qvps")
	log.Printf("qvps: web %s quic %d", addr, port)
	log.Fatal(http.ListenAndServe(addr, mux))
}
