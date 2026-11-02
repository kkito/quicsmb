package xfer

import (
	"bytes"
	"testing"
)

func TestDirRoundTrip(t *testing.T) {
	es := []DirEnt{{Name: "proj", IsDir: true}, {Name: "a.txt", Size: 26}}
	p, err := EncodeDir(es)
	if err != nil {
		t.Fatal(err)
	}
	// 走完整 Msg 帧（ReaddirReq/Resp + StatReq/Resp）
	for _, op := range []Op{OpReaddirReq, OpReaddirResp, OpStatReq, OpStatResp} {
		var buf bytes.Buffer
		if err := Encode(&buf, Msg{Op: op, File: "sub", Data: p}); err != nil {
			t.Fatal(err)
		}
		m, err := Decode(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if m.Op != op || m.File != "sub" {
			t.Fatalf("msg = %+v", m)
		}
	}
	got, err := DecodeDir(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].IsDir || got[1].Size != 26 {
		t.Fatalf("dir = %+v", got)
	}
	// 空目录 + 截断
	p0, _ := EncodeDir(nil)
	if got, err := DecodeDir(p0); err != nil || len(got) != 0 {
		t.Fatalf("empty = %+v %v", got, err)
	}
	for _, bad := range [][]byte{{}, {0}, {0, 0, 0, 1, 0}} {
		if _, err := DecodeDir(bad); err == nil {
			t.Fatalf("want error for %v", bad)
		}
	}
	// stat 编解码
	sp := EncodeStat(true, 0)
	if isDir, sz, err := DecodeStat(sp); err != nil || !isDir || sz != 0 {
		t.Fatalf("stat = %v %d %v", isDir, sz, err)
	}
	sp2 := EncodeStat(false, 123)
	if isDir, sz, err := DecodeStat(sp2); err != nil || isDir || sz != 123 {
		t.Fatalf("stat = %v %d %v", isDir, sz, err)
	}
	if _, _, err := DecodeStat([]byte{1, 2}); err == nil {
		t.Fatal("want stat len error")
	}
}
