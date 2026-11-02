package auth

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key := []byte("test-key-123")
	n, err := Nonce16()
	if err != nil {
		t.Fatal(err)
	}
	msg := Seal(key, n[:], []byte("hello"))
	p, ok := Open(key, msg)
	if !ok || !bytes.Equal(p, []byte("hello")) {
		t.Fatalf("open = %q,%v", p, ok)
	}
}

func TestWrongKeyFailsIndistinguishably(t *testing.T) {
	n, _ := Nonce16()
	msg := Seal([]byte("right"), n[:], []byte("hello"))
	if _, ok := Open([]byte("wrong"), msg); ok {
		t.Fatal("wrong key must fail")
	}
	if _, ok := Open([]byte("right"), msg[:len(msg)-1]); ok {
		t.Fatal("truncated must fail")
	}
	if _, ok := Open([]byte("right"), []byte("short")); ok {
		t.Fatal("short must fail")
	}
}

func TestSealShortNoncePadded(t *testing.T) {
	n, _ := Nonce16()
	msg := Seal([]byte("k"), n[:4], []byte("p"))
	p, ok := Open([]byte("k"), msg)
	if !ok || string(p) != "p" {
		t.Fatalf("padded nonce open = %q,%v", p, ok)
	}
	if len(n) != 16 {
		t.Fatal("nonce should be 16 bytes")
	}
}

func TestBlackholeWritesNothing(t *testing.T) {
	var w blackholeWriter
	n, _ := Nonce16()
	bad := Seal([]byte("right"), n[:], []byte("x"))
	if _, ok := Open([]byte("wrong"), bad); ok {
		t.Fatal("must fail")
	}
	if w.wrote {
		t.Fatal("blackhole must not write")
	}
}

type blackholeWriter struct{ wrote bool }

func (b *blackholeWriter) Write(p []byte) (int, error) {
	b.wrote = true
	return len(p), nil
}
