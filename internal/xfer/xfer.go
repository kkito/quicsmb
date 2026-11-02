// Package xfer 定义 VPS↔家里块传输帧：请求/响应编解码跑在 QUIC stream 上。
// 帧格式：op(1)|fileLen u16be|file|index u64be|dataLen u32be|data。
package xfer

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Op byte

const (
	OpFetchReq  Op = 1
	OpFetchResp Op = 2
	OpPushReq   Op = 3
	OpPushAck   Op = 4
)

const (
	MaxFileLen = 1024
	MaxDataLen = 8 << 20
)

type Msg struct {
	Op    Op
	File  string
	Index uint64
	Data  []byte
}

func Encode(w io.Writer, m Msg) error {
	if len(m.File) > MaxFileLen {
		return fmt.Errorf("file too long: %d", len(m.File))
	}
	if len(m.Data) > MaxDataLen {
		return fmt.Errorf("data too large: %d", len(m.Data))
	}
	var hdr [1 + 2 + 8 + 4]byte
	hdr[0] = byte(m.Op)
	binary.BigEndian.PutUint16(hdr[1:3], uint16(len(m.File)))
	binary.BigEndian.PutUint64(hdr[3:11], m.Index)
	binary.BigEndian.PutUint32(hdr[11:15], uint32(len(m.Data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write([]byte(m.File)); err != nil {
		return err
	}
	if len(m.Data) > 0 {
		if _, err := w.Write(m.Data); err != nil {
			return err
		}
	}
	return nil
}

func Decode(r io.Reader) (Msg, error) {
	var m Msg
	var hdr [1 + 2 + 8 + 4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return m, err
	}
	op := Op(hdr[0])
	if op != OpFetchReq && op != OpFetchResp && op != OpPushReq && op != OpPushAck {
		return m, fmt.Errorf("bad op: %d", hdr[0])
	}
	m.Op = op
	fl := int(binary.BigEndian.Uint16(hdr[1:3]))
	if fl > MaxFileLen {
		return m, fmt.Errorf("file too long: %d", fl)
	}
	m.Index = binary.BigEndian.Uint64(hdr[3:11])
	dl := int(binary.BigEndian.Uint32(hdr[11:15]))
	if dl > MaxDataLen {
		return m, fmt.Errorf("data too large: %d", dl)
	}
	fb := make([]byte, fl)
	if _, err := io.ReadFull(r, fb); err != nil {
		return m, err
	}
	m.File = string(fb)
	if dl > 0 {
		m.Data = make([]byte, dl)
		if _, err := io.ReadFull(r, m.Data); err != nil {
			return m, err
		}
	}
	return m, nil
}
