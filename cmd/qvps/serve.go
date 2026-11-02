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

// fetchStream 是 qfetch 二进制的“家里回源 uniq 通道”：VPS 侧 FUSE 读未命中时
// 把 FetchReq 编进此 stream，qhome 从大盘读回 FetchResp。
// 无连接时返回 error，volume 转 EIO（设计 §5）。
var fetchStreamMu sync.Mutex
var fetchStream *quic.Stream

func setFetchStream(st *quic.Stream) {
	fetchStreamMu.Lock()
	defer fetchStreamMu.Unlock()
	fetchStream = st
}

func fetchViaHome(file string, index uint64) ([]byte, error) {
	h := getHome()
	if h == nil {
		return nil, io.ErrUnexpectedEOF // qhome 未连接：调用方转 EIO
	}
	resp := make(chan fetchResp, 1)
	h.pendMu.Lock()
	h.pend[index] = resp
	h.pendMu.Unlock()
	if err := h.write(xfer.Msg{Op: xfer.OpFetchReq, File: file, Index: index}); err != nil {
		h.pendMu.Lock()
		delete(h.pend, index)
			h.pendMu.Unlock()
		return nil, err
	}
	select {
	case r := <-resp:
		return r.data, r.err
	case <-time.After(60 * time.Second):
		h.pendMu.Lock()
		delete(h.pend, index)
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
	h.acks[m.Index] = ack
	h.ackMu.Unlock()
	if err := h.write(m); err != nil {
		h.ackMu.Lock()
		delete(h.acks, m.Index)
			h.ackMu.Unlock()
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-time.After(60 * time.Second):
		h.ackMu.Lock()
		delete(h.acks, m.Index)
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
type homeConn struct {
	st     *quic.Stream
	mu     sync.Mutex
	pendMu sync.Mutex
	pend   map[uint64]chan fetchResp // FetchResp 等待者（按块号）
	ackMu  sync.Mutex
	acks   map[uint64]chan error     // PushAck 等待者（按块号）
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
	h := &homeConn{st: st, pend: map[uint64]chan fetchResp{}, acks: map[uint64]chan error{}}
	setFetchStream(st)
	setHome(h)
	defer setHome(nil)
	defer setFetchStream(nil)
	for {
		m, err := xfer.Decode(st)
		if err != nil {
			return
		}
		switch m.Op {
		case xfer.OpFetchResp:
			h.pendMu.Lock()
			w := h.pend[m.Index]
			delete(h.pend, m.Index)
			h.pendMu.Unlock()
			if w != nil {
				w <- fetchResp{data: m.Data}
			}
		case xfer.OpPushAck:
			h.ackMu.Lock()
			w := h.acks[m.Index]
			delete(h.acks, m.Index)
			h.ackMu.Unlock()
			if w != nil {
				w <- nil
			}
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
