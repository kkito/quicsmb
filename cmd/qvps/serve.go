package main

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"quicsmb/internal/auth"
	"quicsmb/internal/mount"
	"quicsmb/internal/volume"
	"quicsmb/internal/xfer"
)

var fsRef *mount.FS

func SetFSRef(f *mount.FS) { fsRef = f }

type fetchResp struct {
	data []byte
	err  error
}

func fetchViaHome(file string, index uint64) ([]byte, error) {
	h := getHome()
	if h == nil {
		return nil, io.ErrUnexpectedEOF // qhome 未连接：调用方转 EIO
	}
	resp := make(chan fetchResp, 1)
	h.pendMu.Lock()
	k := pendKey(file, index)
	h.pend[k] = resp
	h.pendMu.Unlock()
	if err := h.write(xfer.Msg{Op: xfer.OpFetchReq, File: file, Index: index}); err != nil {
		h.pendMu.Lock()
		delete(h.pend, k)
		h.pendMu.Unlock()
		return nil, err
	}
	select {
	case r := <-resp:
		return r.data, r.err
	case <-time.After(60 * time.Second):
		h.pendMu.Lock()
		delete(h.pend, k)
		h.pendMu.Unlock()
		return nil, io.ErrUnexpectedEOF
	}
}

// pushViaHome 把脏块推给家里落盘，等 PushAck（设计 §3）。
func pushViaHome(m xfer.Msg) error {
	h := getHome()
	if h == nil {
		return io.ErrUnexpectedEOF
	}
	ack := make(chan error, 1)
	h.ackMu.Lock()
	k := pendKey(m.File, m.Index)
	h.acks[k] = ack
	h.ackMu.Unlock()
	if err := h.write(m); err != nil {
		h.ackMu.Lock()
		delete(h.acks, k)
		h.ackMu.Unlock()
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-time.After(60 * time.Second):
		h.ackMu.Lock()
		delete(h.acks, k)
		h.ackMu.Unlock()
		return io.ErrUnexpectedEOF
	}
}

func serveQUIC(c *quic.Conn, key []byte, vol *volume.Volume) {
	defer c.CloseWithError(0, "")
	for {
		st, err := c.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go serveStream(st, key, vol)
	}
}

// homeConn 是当前 qhome 连接的写端（VPS→家里唯一出口，stream 写需串行）。
// pendKey 区分文件：同块号不同文件并发回源不串味（P0-4）。
func pendKey(file string, index uint64) string {
	return file + "\x00" + string(rune(index>>32)) + string(rune(index))
}

type homeConn struct {
	st       *quic.Stream
	mu       sync.Mutex
	pendMu   sync.Mutex
	pend     map[string]chan fetchResp // FetchResp 等待者（按 file#index）
	ackMu    sync.Mutex
	acks     map[string]chan error // PushAck 等待者（按 file#index）
	listMu   sync.Mutex
	listCh   chan listResp // OpListResp 等待者（同时只允许一个在途）
	dirMu    sync.Mutex
	dirPend  map[string]chan dirResp // OpReaddirResp 等待者（按目录）
	statMu   sync.Mutex
	statPend map[string]chan statResp // OpStatResp 等待者（按路径）
}

type dirResp struct {
	data []byte
	err  error
}

type statResp struct {
	data []byte
	err  error
}

type listResp struct {
	data []byte
	err  error
}

var homeMu sync.Mutex
var home *homeConn

func setHome(h *homeConn) {
	homeMu.Lock()
	defer homeMu.Unlock()
	home = h
}

func getHome() *homeConn {
	homeMu.Lock()
	defer homeMu.Unlock()
	return home
}

func (h *homeConn) write(m xfer.Msg) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return xfer.Encode(h.st, m)
}

// homeLoop 是 qhome 连接的常驻循环：VPS 侧 fetch 经此发 FetchReq 读大盘，
// 回写调度经此发 PushReq 落盘；读泵按 Index 分发 FetchResp/PushAck。
// （设计 §2: 回源与回写都由 VPS 发起、家里响应）
func homeLoop(st *quic.Stream, vol *volume.Volume) {
	h := &homeConn{st: st, pend: map[string]chan fetchResp{}, acks: map[string]chan error{}, dirPend: map[string]chan dirResp{}, statPend: map[string]chan statResp{}}
	setHome(h)
	defer setHome(nil)
	for {
		m, err := xfer.Decode(st)
		if err != nil {
			return
		}
		switch m.Op {
		case xfer.OpFetchResp:
			h.pendMu.Lock()
			w := h.pend[pendKey(m.File, m.Index)]
			delete(h.pend, pendKey(m.File, m.Index))
			h.pendMu.Unlock()
			if w != nil {
				w <- fetchResp{data: m.Data}
			}
		case xfer.OpPushAck:
			h.ackMu.Lock()
			w := h.acks[pendKey(m.File, m.Index)]
			delete(h.acks, pendKey(m.File, m.Index))
			h.ackMu.Unlock()
			if w != nil {
				w <- nil
			}
		case xfer.OpListResp:
			h.listMu.Lock()
			w := h.listCh
			h.listCh = nil
			h.listMu.Unlock()
			if w != nil {
				w <- listResp{data: m.Data}
			}
		case xfer.OpReaddirResp:
			h.dirMu.Lock()
			w := h.dirPend[m.File]
			delete(h.dirPend, m.File)
			h.dirMu.Unlock()
			if w != nil {
				w <- dirResp{data: m.Data}
			}
		case xfer.OpStatResp:
			h.statMu.Lock()
			ws := h.statPend[m.File]
			delete(h.statPend, m.File)
			h.statMu.Unlock()
			if ws != nil {
				ws <- statResp{data: m.Data}
			}
		case xfer.OpStatfsReq:
			// 家里连上后上报的大盘容量（之前漏接：df 一直显示兜底 1G）
			log.Printf("qvps: statfs from home: %d bytes", len(m.Data))
			if len(m.Data) == 16 {
				total := binary.BigEndian.Uint64(m.Data[0:8])
				free := binary.BigEndian.Uint64(m.Data[8:16])
				vol.SetCapacity(total, free)
				if fsRef != nil {
					fsRef.SetCapacity(total, free)
				}
			}
		case xfer.OpTruncReq:
			// 家里侧不需要（VPS 只发不收）
		}
	}
}

func serveStream(st *quic.Stream, key []byte, vol *volume.Volume) {
	defer st.Close()
	// 首帧：auth.Seal 握手
	hs, err := xfer.Decode(st)
	if err != nil {
		return
	}
	if hs.Op != xfer.OpFetchReq {
		return
	}
	msg, ok := auth.Open(key, hs.Data)
	if !ok {
		return // 黑洞：零回包
	}
	if string(msg) == "qhome-hello" {
		homeLoop(st, vol)
		return
	}
	for {
		m, err := xfer.Decode(st)
		if err != nil {
			if err != io.EOF {
				log.Printf("qvps: decode: %v", err)
			}
			return
		}
		switch m.Op {
		case xfer.OpFetchReq:
			if m.File == "" {
				continue // 握手帧已消费，无需回包
			}
			buf := make([]byte, blockSize())
			if _, err := vol.ReadAt(m.File, int64(m.Index)*int64(blockSize()), buf, fetchViaHome); err != nil {
				return // 回源失败：直接关流，FUSE 侧已得 EIO（设计 §5）
			}
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchResp, File: m.File, Index: m.Index, Data: buf}); err != nil {
				return
			}
		case xfer.OpPushReq:
			// 外部/探针推送：落 volume 并标脏，回写调度器稍后推家里（与 FUSE 写一致）。
			if _, err := vol.WriteAt(m.File, int64(m.Index)*int64(blockSize()), m.Data, time.Now().Unix()); err != nil {
				return
			}
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpPushAck, File: m.File, Index: m.Index}); err != nil {
				return
			}
		case xfer.OpStatfsReq:
			if len(m.Data) == 16 {
				total := binary.BigEndian.Uint64(m.Data[0:8])
				free := binary.BigEndian.Uint64(m.Data[8:16])
				vol.SetCapacity(total, free)
				if fsRef != nil {
					fsRef.SetCapacity(total, free)
				}
			}
		}
	}
}

func blockSize() int { return 512 << 10 }

// truncViaHome 通知家里把文件截到真实大小（PushReq 是整块补零写）。
func truncViaHome(file string, size int64) error {
	h := getHome()
	if h == nil {
		return io.ErrUnexpectedEOF
	}
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], uint64(size))
	return h.write(xfer.Msg{Op: xfer.OpTruncReq, File: file, Data: payload[:]})
}

// requestFileList 向家里索要文件列表（目录同步），60s 超时。
func requestFileList() ([]xfer.FileEntry, error) {
	h := getHome()
	if h == nil {
		return nil, io.ErrUnexpectedEOF
	}
	ch := make(chan listResp, 1)
	h.listMu.Lock()
	h.listCh = ch
	h.listMu.Unlock()
	if err := h.write(xfer.Msg{Op: xfer.OpListReq}); err != nil {
		h.listMu.Lock()
		h.listCh = nil
		h.listMu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		return xfer.DecodeList(r.data)
	case <-time.After(60 * time.Second):
		h.listMu.Lock()
		h.listCh = nil
		h.listMu.Unlock()
		return nil, io.ErrUnexpectedEOF
	}
}

// syncFileList 拉家里列表 → volume.SetFile；失败只记日志，下轮继续。
func syncFileList(vol *volume.Volume) {
	list, err := requestFileList()
	if err != nil {
		log.Printf("qvps: filelist: %v", err)
		return
	}
	for _, e := range list {
		vol.SetFile(e.Name, e.Size)
	}
}

// readdirViaHome 按需问家里“某一级目录下有什么”，只读一层，跟全盘文件数无关。
func readdirViaHome(dir string) ([]xfer.DirEnt, error) {
	h := getHome()
	if h == nil {
		return nil, io.ErrUnexpectedEOF
	}
	ch := make(chan dirResp, 1)
	h.dirMu.Lock()
	h.dirPend[dir] = ch
	h.dirMu.Unlock()
	if err := h.write(xfer.Msg{Op: xfer.OpReaddirReq, File: dir}); err != nil {
		h.dirMu.Lock()
		delete(h.dirPend, dir)
		h.dirMu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		return xfer.DecodeDir(r.data)
	case <-time.After(60 * time.Second):
		h.dirMu.Lock()
		delete(h.dirPend, dir)
		h.dirMu.Unlock()
		return nil, io.ErrUnexpectedEOF
	}
}

// statViaHome 按需问家里单个路径的状态（Lookup/Getattr 用）。
func statViaHome(name string) (isDir bool, size int64, ok bool) {
	h := getHome()
	if h == nil {
		return false, 0, false
	}
	ch := make(chan statResp, 1)
	h.statMu.Lock()
	h.statPend[name] = ch
	h.statMu.Unlock()
	if err := h.write(xfer.Msg{Op: xfer.OpStatReq, File: name}); err != nil {
		h.statMu.Lock()
		delete(h.statPend, name)
		h.statMu.Unlock()
		return false, 0, false
	}
	select {
	case r := <-ch:
		isDir, size, err := xfer.DecodeStat(r.data)
		if err != nil {
			return false, 0, false
		}
		return isDir, size, true
	case <-time.After(60 * time.Second):
		h.statMu.Lock()
		delete(h.statPend, name)
		h.statMu.Unlock()
		return false, 0, false
	}
}
