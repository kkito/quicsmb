package tunnel

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"testing"
	"time"
)

// fakeStream 是内存双向 stream：Read 从 in 读（空则阻塞到 Close），Write 进 out。
type fakeStream struct {
	mu     chan struct{}
	done    chan struct{}
	in     *bytes.Buffer
	out    *bytes.Buffer
	closed bool
}

func newFakePair(in []byte) *fakeStream {
	return &fakeStream{done: make(chan struct{}), in: bytes.NewBuffer(in), out: &bytes.Buffer{}}
}

func (f *fakeStream) Read(p []byte) (int, error) {
	for {
		if f.in.Len() > 0 {
			return f.in.Read(p)
		}
		select {
		case <-f.done:
			return 0, io.EOF
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (f *fakeStream) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeStream) Close() error {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	f.closed = true
	return nil
}
func (f *fakeStream) CancelWrite(uint64)          {}
func (f *fakeStream) CancelRead(uint64)           {}

// errStream 模拟损坏的 stream。
type errStream struct{ *fakeStream }

func newErrStream() *errStream { return &errStream{newFakePair(nil)} }

func (e *errStream) Write(p []byte) (int, error) { return 0, errors.New("broken") }

func TestPumpBothDirections(t *testing.T) {
	tcpA, tcpB := net.Pipe()
	defer tcpA.Close()
	defer tcpB.Close()
	st := newFakePair([]byte("from-quic"))

	s := Start(tcpA, st, log.New(io.Discard, "", 0))
	if s.ID() == 0 {
		t.Fatal("session id should be nonzero")
	}

	// tcp -> stream
	if _, err := tcpB.Write([]byte("hello-tcp")); err != nil {
		t.Fatal(err)
	}
	// stream -> tcp
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = tcpB.Close()
	}()
	buf := make([]byte, 64)
	n, _ := tcpB.Read(buf)
	if string(buf[:n]) != "from-quic" {
		t.Fatalf("tcp got %q", buf[:n])
	}
	<-s.Done()
	stats := s.Stats()
	if stats.BytesDown == 0 {
		t.Fatal("BytesDown should be > 0")
	}
	// tcp->stream 方向：stream out 应收到 hello-tcp
	deadline := time.Now().Add(2 * time.Second)
	for st.out.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if st.out.String() != "hello-tcp" {
		t.Fatalf("stream got %q", st.out.String())
	}
	if stats.BytesUp == 0 && s.Stats().BytesUp == 0 {
		t.Fatal("BytesUp should be > 0")
	}
}

func TestSniffLogsSMB(t *testing.T) {
	var logBuf bytes.Buffer
	tcpA, tcpB := net.Pipe()
	defer tcpA.Close()
	defer tcpB.Close()
	st := newFakePair(nil)
	s := Start(tcpA, st, log.New(&logBuf, "", 0))

	h := make([]byte, 64)
	h[0], h[1], h[2], h[3] = 0xFE, 'S', 'M', 'B'
	pkt := append([]byte{0x00, 0x00, 0x00, 0x40}, h...)
	pkt = append(pkt, make([]byte, 6)...)
	if _, err := tcpB.Write(pkt); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for logBuf.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	_ = tcpB.Close()
	_ = st.Close()
	<-s.Done()
	if !bytes.Contains(logBuf.Bytes(), []byte("NEGOTIATE")) {
		t.Fatalf("log should contain NEGOTIATE: %q", logBuf.String())
	}
	if got := s.Stats().Command; got != "NEGOTIATE" {
		t.Fatalf("command = %q", got)
	}
}

func TestBrokenStreamDoesNotHang(t *testing.T) {
	tcpA, tcpB := net.Pipe()
	defer tcpB.Close()
	es := newErrStream()
	s := Start(tcpA, es, log.New(io.Discard, "", 0))
	_, _ = tcpB.Write([]byte("x"))
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session should finish on broken stream")
	}
}
