// Command qvps 是 VPS 侧 daemon：QUIC server + volume 缓存 + FUSE 挂载 + Web 面板。
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"fmt"
	"os"
	"strconv"
	"time"

	"quicsmb/internal/writeback"
	"quicsmb/internal/xfer"

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

	var mnt *mount.Server
	if mountpoint != "" {
		mnt, err = mount.Mount(mountpoint, vol, fetchViaHome)
		if err != nil {
			log.Fatalf("mount: %v", err)
		}
		defer mnt.Close()
		SetFSRef(mount.FSRef(mnt))
		log.Printf("qvps: mounted %s", mountpoint)
	}

	lis, err := quic.Listen(port, "qvfs/1", "", "")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go writebackLoop(vol, meta, writebackPolicyFromEnv())
	go syncLoop(vol)
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

func writebackPolicyFromEnv() writeback.Policy {
	p := writeback.DefaultPolicy()
	if s := os.Getenv("QVFS_DELAY_SEC"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			p.DelaySec = n
		}
	}
	if s := os.Getenv("QVFS_WIN"); s != "" {
			var a, b int
			if n, _ := fmt.Sscanf(s, "%d-%d", &a, &b); n == 2 {
				p.WinStartMin, p.WinEndMin = a, b
			}
		_ = a
		}
	return p
}

// writebackLoop 每分钟扫脏块（设计 §3）：到期且在窗口内 → 经 home 通道推家里落盘。
func writebackLoop(vol *volume.Volume, meta *metastore.Store, pol writeback.Policy) {
	interval := time.Minute
	if s := os.Getenv("QVFS_TICK_SEC"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for range tick.C {
		now := time.Now().Unix()
			for _, b := range vol.FlushDue(pol, func() int64 { return now }, 32) {
				if err := pushBlock(b.File, b.Index, b.Data); err != nil {
					meta.BumpFailure(b.File, b.Index)
					log.Printf("qvps: push %s#%d: %v", b.File, b.Index, err)
					continue
				}
				meta.MarkClean(b.File, b.Index)
			}
			if err := meta.Save(); err != nil {
				log.Printf("qvps: metastore save: %v", err)
			}
		}
}

func pushBlock(file string, index uint64, data []byte) error {
	return pushViaHome(xfer.Msg{Op: xfer.OpPushReq, File: file, Index: index, Data: data})
}
// syncLoop 等家里连上后拉文件列表 + 容量；之后每 5 分钟重同步一次。
// （P1-5 目录同步 + P2-12 容量刷新）
func syncLoop(vol *Volume) {
	for {
		time.Sleep(30 * time.Second)
		if getHome() == nil {
			continue
		}
		syncFileList(vol)
	}
}

type Volume = volume.Volume
