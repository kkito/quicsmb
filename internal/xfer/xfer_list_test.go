package xfer

import (
	"bytes"
	"testing"
)

func TestListRoundTrip(t *testing.T) {
	es := []FileEntry{{Name: "a/b.bin", Size: 5}, {Name: "top.txt", Size: 0}}
	p, err := EncodeList(es)
	if err != nil {
		t.Fatal(err)
	}
	// 走完整 Msg 帧
	if err := Encode(&bytes.Buffer{}, Msg{Op: OpListReq}); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeList(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a/b.bin" || got[0].Size != 5 {
		t.Fatalf("list = %+v", got)
	}
	// 空表
	p0, _ := EncodeList(nil)
	if got, err := DecodeList(p0); err != nil || len(got) != 0 {
		t.Fatalf("empty = %+v %v", got, err)
	}
	// 截断/超限
	for _, bad := range [][]byte{{}, {0, 0}, {0, 0, 0, 1, 0}, {0, 0, 0, 1, 0, 5}} {
		if _, err := DecodeList(bad); err == nil {
			t.Fatalf("want error for %v", bad)
		}
	}
	long := FileEntry{Name: string(make([]byte, MaxFileLen+1))}
	if _, err := EncodeList([]FileEntry{long}); err == nil {
		t.Fatal("want name-too-long")
	}
}

func TestTruncReqOpRoundTrip(t *testing.T) {
	// Op 8 编解码走通（含 8 字节 size payload）
	var buf bytes.Buffer
	if err := Encode(&buf, Msg{Op: OpTruncReq, File: "a.txt", Data: []byte{0, 0, 0, 0, 0, 0, 0, 26}}); err != nil {
		t.Fatal(err)
	}
	m, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if m.Op != OpTruncReq || m.File != "a.txt" || len(m.Data) != 8 {
		t.Fatalf("msg = %+v", m)
	}
}
