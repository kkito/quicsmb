package session

import (
	"io"
	"log"
	"net"
	"testing"

	"quicsmb/internal/tunnel"
)

type stubStream struct {
	closed bool
}

func (s *stubStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *stubStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *stubStream) Close() error                { s.closed = true; return nil }
func (s *stubStream) CancelWrite(uint64)          {}
func (s *stubStream) CancelRead(uint64)           {}

func newSess(t *testing.T) *tunnel.Session {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return tunnel.Start(a, &stubStream{}, log.New(io.Discard, "", 0))
}

func TestAddAndCount(t *testing.T) {
	r := New(10)
	s := newSess(t)
	if !r.Add(s, "client", "peer") {
		t.Fatal("Add should succeed")
	}
	if r.Count() != 1 {
		t.Fatalf("count = %d", r.Count())
	}
	r.Remove(s.ID())
	if r.Count() != 0 {
		t.Fatal("count should be 0 after remove")
	}
}

func TestMaxSessions(t *testing.T) {
	r := New(1)
	s1 := newSess(t)
	s2 := newSess(t)
	if !r.Add(s1, "client", "a") {
		t.Fatal("first Add should succeed")
	}
	if r.Add(s2, "client", "b") {
		t.Fatal("second Add should be rejected")
	}
}

func TestUnlimitedWhenZero(t *testing.T) {
	r := New(0)
	for i := 0; i < 5; i++ {
		if !r.Add(newSess(t), "client", "p") {
			t.Fatal("unlimited registry should accept")
		}
	}
}

func TestListAndLogs(t *testing.T) {
	r := New(10)
	s := newSess(t)
	_ = r.Add(s, "server", "peer1")
	entries := r.List()
	if len(entries) != 1 || entries[0].Peer != "peer1" {
		t.Fatalf("list = %+v", entries)
	}
	r.PushLog("connect", "hello")
	logs := r.Logs(10)
	if len(logs) != 1 || logs[0].Summary != "hello" {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestTotals(t *testing.T) {
	r := New(0)
	r.AddCounters(100, 200)
	rx, tx := r.Totals()
	if rx != 200 || tx != 100 {
		t.Fatalf("rx=%d tx=%d", rx, tx)
	}
}
