package xfer

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, m := range []Msg{
		{Op: OpFetchReq, File: "a.bin", Index: 3},
		{Op: OpFetchResp, File: "a.bin", Index: 3, Data: []byte("payload-data")},
		{Op: OpPushReq, File: "d/e", Index: 0, Data: make([]byte, 100)},
		{Op: OpPushAck, File: "d/e", Index: 0},
	} {
		var buf bytes.Buffer
		if err := Encode(&buf, m); err != nil {
			t.Fatal(err)
		}
		back, err := Decode(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if back.Op != m.Op || back.File != m.File || back.Index != m.Index || !bytes.Equal(back.Data, m.Data) {
			t.Fatalf("roundtrip mismatch: %+v vs %+v", back, m)
		}
	}
}

func TestTruncatedFails(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, Msg{Op: OpFetchResp, File: "f", Index: 1, Data: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if _, err := Decode(bytes.NewReader(b[:len(b)-2])); err == nil {
		t.Fatal("truncated must fail")
	}
}

func TestOversizeRejected(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, Msg{Op: OpPushReq, File: "f", Data: make([]byte, 8<<20 + 1)}); err == nil {
		t.Fatal("oversize data must be rejected")
	}
	if err := Encode(&buf, Msg{Op: OpFetchReq, File: string(make([]byte, 2000))}); err == nil {
		t.Fatal("oversize file must be rejected")
	}
}

func TestBadOpFails(t *testing.T) {
	if _, err := Decode(bytes.NewReader([]byte{99, 0, 0})); err == nil {
		t.Fatal("bad op must fail")
	}
}
