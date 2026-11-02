package main

import (
	"context"
	"io"
	"log"

	"github.com/quic-go/quic-go"

	"quicsmb/internal/auth"
	"quicsmb/internal/cache"
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
			k := cache.Key{File: m.File, Index: m.Index}
			_ = k
			b := make([]byte, chunk.DefaultBlockSize)
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchResp, File: m.File, Index: m.Index, Data: b}); err != nil {
				return
			}
		case xfer.OpPushReq:
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpPushAck, File: m.File, Index: m.Index}); err != nil {
				return
			}
		}
	}
}
