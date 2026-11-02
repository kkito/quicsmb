// Package xfer 定义 VPS↔家里块传输帧：请求/响应编解码跑在 QUIC stream 上。
// 帧格式：op(1)|fileLen u16be|file|index u64be|dataLen u32be|data。
package xfer

import (
	"bytes"
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
	// OpStatfsReq 家里上报大盘容量（Data 载 statfsPayload: total u64be + free u64be）。
	OpStatfsReq Op = 5
	// OpListReq VPS 索要文件列表；OpListResp 家里回 Data=listPayload。
	OpListReq  Op = 6
	OpListResp Op = 7
	// OpTruncReq VPS 通知家里把文件截断到真实大小（Data = size u64be）。
	// PushReq 推的是整块（512KB 补零），不截的话家里文件会被补零撑大。
	OpTruncReq Op = 8
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
	if op != OpFetchReq && op != OpFetchResp && op != OpPushReq && op != OpPushAck && op != OpStatfsReq && op != OpListReq && op != OpListResp && op != OpTruncReq {
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

// FileEntry 与 disk.FileEntry 对齐（xfer 不依赖 disk，避免循环引用）。
type FileEntry struct {
	Name string
	Size int64
}

// EncodeList 把文件列表编成 listPayload：
// count u32be + 每项 nameLen u16be + name + size u64be。name 超 1024B 报错。
func EncodeList(es []FileEntry) ([]byte, error) {
	if len(es) > 1<<20 {
		return nil, fmt.Errorf("too many files: %d", len(es))
	}
	var b bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(es)))
	b.Write(hdr[:])
	for _, e := range es {
		if len(e.Name) > MaxFileLen {
			return nil, fmt.Errorf("name too long: %d", len(e.Name))
		}
		var h2 [2 + 8]byte
		binary.BigEndian.PutUint16(h2[0:2], uint16(len(e.Name)))
		binary.BigEndian.PutUint64(h2[2:10], uint64(e.Size))
		b.Write(h2[:])
		b.WriteString(e.Name)
	}
	return b.Bytes(), nil
}

// DecodeList 解 listPayload，超限/截断报错。
func DecodeList(p []byte) ([]FileEntry, error) {
	if len(p) < 4 {
		return nil, fmt.Errorf("list too short: %d", len(p))
	}
	n := int(binary.BigEndian.Uint32(p[0:4]))
	if n > 1<<20 {
		return nil, fmt.Errorf("too many files: %d", n)
	}
	p = p[4:]
	out := make([]FileEntry, 0, n)
	for i := 0; i < n; i++ {
		if len(p) < 10 {
			return nil, fmt.Errorf("list entry %d truncated", i)
		}
		nl := int(binary.BigEndian.Uint16(p[0:2]))
		if nl > MaxFileLen {
			return nil, fmt.Errorf("name too long: %d", nl)
		}
		sz := int64(binary.BigEndian.Uint64(p[2:10]))
		p = p[10:]
		if len(p) < nl {
			return nil, fmt.Errorf("list entry %d name truncated", i)
		}
		out = append(out, FileEntry{Name: string(p[:nl]), Size: sz})
		p = p[nl:]
	}
	return out, nil
}
