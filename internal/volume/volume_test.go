package volume

import (
	"bytes"
	"fmt"
	"testing"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
)

func testVol(t *testing.T) (*Volume, *int) {
	t.Helper()
	meta, err := metastore.Load(t.TempDir()+"/d.json", 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	v := New(512<<10, cache.New(4*512<<10, 512<<10), meta)
	_ = v
	_ = calls
	return v, calls
}

func TestReadFetchesOnceThenCached(t *testing.T) {
	v, _ := testVol(t)
	n := 0
	fetch := func(file string, index uint64) ([]byte, error) {
		n++
		b := make([]byte, 512<<10)
		b[0] = byte(index) + 1
		return b, nil
	}
	buf := make([]byte, 10)
	if _, err := v.ReadAt("f", 0, buf, fetch); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ReadAt("f", 0, buf, fetch); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fetch calls = %d, want 1", n)
	}
	if buf[0] != 1 {
		t.Fatalf("buf[0] = %d", buf[0])
	}
}

func TestWriteMarksDirtyAndReadBack(t *testing.T) {
	v, _ := testVol(t)
	if _, err := v.WriteAt("f", 5, []byte("hello"), 1000); err != nil {
		t.Fatal(err)
	}
	if len(v.meta.List()) != 1 {
		t.Fatalf("dirty = %+v", v.meta.List())
	}
	buf := make([]byte, 5)
	nofetch := func(file string, index uint64) ([]byte, error) {
		return nil, fmt.Errorf("must not fetch")
	}
	if _, err := v.ReadAt("f", 5, buf, nofetch); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, []byte("hello")) {
		t.Fatalf("buf = %q", buf)
	}
}

func TestReadModifyWriteAcrossBlocks(t *testing.T) {
	v, _ := testVol(t)
	bs := 512 << 10
	payload := []byte("CROSS")
	off := int64(bs - 2)
	if _, err := v.WriteAt("f", off, payload, 2000); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	nofetch := func(file string, index uint64) ([]byte, error) {
		return nil, fmt.Errorf("must not fetch")
	}
	if _, err := v.ReadAt("f", off, buf, nofetch); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("cross = %q", buf)
	}
	// 块0尾2字节 + 块1头3字节
	b0, err := v.ReadAt("f", int64(bs-2), make([]byte, 2), nofetch)
	_ = b0
	if err != nil {
		t.Fatal(err)
	}
	if len(v.meta.List()) != 2 {
		t.Fatalf("want 2 dirty blocks, got %+v", v.meta.List())
	}
	_ = chunk.DefaultBlockSize
}

func TestFetchFailureIsEIO(t *testing.T) {
	v, _ := testVol(t)
	bad := func(file string, index uint64) ([]byte, error) {
		return nil, fmt.Errorf("home unreachable")
	}
	if _, err := v.ReadAt("missing", 0, make([]byte, 4), bad); err == nil {
		t.Fatal("want EIO-style error")
	}
}
