// Package tunnel 提供 TCP <-> QUIC stream 中继：一个 session = 一条 TCP 连接 +
// 一条 QUIC 双向 stream，双向 pump，统计字节数，首包 SMB2 嗅探记日志。
// 对应 C 版 src/proxy/tunnel.c。
package tunnel

import (
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"quicsmb/internal/smb"
)

// Stream 是 QUIC 双向 stream 的最小接口（quic.Stream 满足，单测可 fake）。
type Stream interface {
	io.Reader
	io.Writer
	Close() error
	CancelWrite(code uint64)
	CancelRead(code uint64)
}

// Stats 是 session 计数快照。
type Stats struct {
	ID        uint64
	BytesUp   uint64
	BytesDown uint64
	Command   string
	Dialect   string
}

var nextID atomic.Uint64

// Session 是一次 TCP<->stream 中继。
type Session struct {
	id         uint64
	tcp        net.Conn
	stream     Stream
	log        *log.Logger
	bytesUp    atomic.Uint64
	bytesDown  atomic.Uint64
	mu         sync.Mutex
	sniffed    bool
	cmd        string
	dialect    string
	once       sync.Once
	done       chan struct{}
}

// Start 启动双向 pump，返回 session。调用者不应再直接用 tcp/stream。
func Start(tcp net.Conn, stream Stream, logger *log.Logger) *Session {
	if logger == nil {
		logger = log.Default()
	}
	s := &Session{
		id:     nextID.Add(1),
		tcp:    tcp,
		stream: stream,
		log:    logger,
		done:   make(chan struct{}),
	}
	go s.pumpTCPtoStream()
	go s.pumpStreamToTCP()
	return s
}

// ID 返回 session id。
func (s *Session) ID() uint64 { return s.id }

// Stats 返回计数快照。
func (s *Session) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		ID:        s.id,
		BytesUp:   s.bytesUp.Load(),
		BytesDown: s.bytesDown.Load(),
		Command:   s.cmd,
		Dialect:   s.dialect,
	}
}

// Done 在双向都结束时关闭。
func (s *Session) Done() <-chan struct{} { return s.done }

// pumpTCPtoStream 把 TCP 字节搬进 stream；任一方向 EOF 即半关闭对端写方向。
func (s *Session) pumpTCPtoStream() {
	defer s.teardown()
	buf := make([]byte, 65536)
	for {
		n, err := s.tcp.Read(buf)
		if n > 0 {
			s.sniff(buf[:n])
			if _, werr := writeFull(s.stream, buf[:n]); werr != nil {
				return
			}
			s.bytesUp.Add(uint64(n))
		}
		if err != nil {
			_ = s.stream.Close() // 半关闭写方向
			return
		}
	}
}

func (s *Session) pumpStreamToTCP() {
	defer s.teardown()
	buf := make([]byte, 65536)
	for {
		n, err := s.stream.Read(buf)
		if n > 0 {
			if _, werr := writeFull(s.tcp, buf[:n]); werr != nil {
				return
			}
			s.bytesDown.Add(uint64(n))
		}
		if err != nil {
			if c, ok := s.tcp.(interface{ CloseWrite() error }); ok {
				_ = c.CloseWrite()
			} else {
				_ = s.tcp.Close()
			}
			return
		}
	}
}

func (s *Session) teardown() {
	s.once.Do(func() { close(s.done) })
}

func (s *Session) sniff(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sniffed || len(b) < 68 {
		return
	}
	if info, ok := smb.Sniff(b); ok && info.Valid {
		s.sniffed = true
		s.cmd = smb.CommandName(info.Cmd)
		s.dialect = smb.DialectName(info.Dialect)
		s.log.Printf("sess=%d smb %s cmd=%s", s.id, s.dialect, s.cmd)
	}
}

func writeFull(w io.Writer, b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		n, err := w.Write(b)
		total += n
		if err != nil {
			return total, err
		}
		b = b[n:]
	}
	return total, nil
}
