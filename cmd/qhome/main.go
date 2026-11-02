// Command qhome 是家里侧 daemon：QUIC client 主动拨 VPS，回源读大盘、回写落盘。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"quicsmb/internal/auth"
	"quicsmb/internal/chunk"
	"quicsmb/internal/disk"
	"quicsmb/internal/quic"
	"quicsmb/internal/xfer"
)

func main() {
	var server string
	var port int
	var root string
	flag.StringVar(&server, "server", "127.0.0.1", "VPS 地址")
	flag.IntVar(&port, "port", 4433, "VPS QUIC 端口")
	flag.StringVar(&root, "root", "/data/qvfs", "大盘根目录")
	flag.Parse()

	key := []byte(os.Getenv("QVFS_KEY"))
	if len(key) == 0 {
		key = []byte("dev-key-change-me")
	}
	d := disk.New(root)

	backoff := time.Second
	for {
		if err := run(context.Background(), server, port, key, d); err != nil {
			log.Printf("qhome: %v; retry in %s", err, backoff)
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
	}
}

func run(ctx context.Context, server string, port int, key []byte, d *disk.Root) error {
	c, err := quic.Dial(ctx, server, port, "qvfs/1", true, 10*time.Second)
	if err != nil {
		return err
	}
	defer c.CloseWithError(0, "")
	st, err := quic.OpenStream(ctx, c)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := auth.Nonce16()
	if err != nil {
		return err
	}
	hello := auth.Seal(key, n[:], []byte("qhome-hello"))
	if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchReq, File: "", Index: 0, Data: hello}); err != nil {
		return err
	}
	bs := chunk.DefaultBlockSize
	for {
		m, err := xfer.Decode(st)
		if err != nil {
			return err
		}
		switch m.Op {
		case xfer.OpFetchReq:
			data, err := d.ReadAt(m.File, int64(m.Index)*int64(bs), bs)
			if err != nil {
				return err
			}
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchResp, File: m.File, Index: m.Index, Data: data}); err != nil {
				return err
			}
		case xfer.OpPushReq:
			if err := d.WriteAt(m.File, int64(m.Index)*int64(bs), m.Data); err != nil {
				return err
			}
			if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpPushAck, File: m.File, Index: m.Index}); err != nil {
				return err
			}
		}
	}
}
