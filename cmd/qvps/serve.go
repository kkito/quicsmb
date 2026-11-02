package main

import (
	"context"
	"io"
	"log"

	"github.com/quic-go/quic-go"

	"quicsmb/internal/auth"
	"quicsmb/internal/chunk"
	"quicsmb/internal/volume"
	"quicsmb/internal/xfer"
)

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
	if _, ok := auth.Open(key, hs.Data); !ok {
		return // 黑洞：零回包
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
			buf := make([]byte, chunk.DefaultBlockSize)
			// e2e/单机模式：无上游回源，未命中回填零块（VPS→家里回源是下一步工作）。
			fetch := func(file string, index uint64) ([]byte, error) {
				return make([]byte, chunk.DefaultBlockSize), nil
			}
			if _, err := vol.ReadAt(m.File, int64(m.Index)*int64(chunk.DefaultBlockSize), buf, fetch); err != nil {
				return
			}
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchResp, File: m.File, Index: m.Index, Data: buf}); err != nil {
				return
			}
		case xfer.OpPushReq:
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpPushAck, File: m.File, Index: m.Index}); err != nil {
				return
			}
		}
	}
}
