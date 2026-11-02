package xfer

import (
	"bytes"
	"testing"
)

// 超限拒绝 + 坏帧头（补 Decode 分支覆盖）。
func TestEncodeLimitsAndDecodeBadFrames(t *testing.T) {
	if err := Encode(&bytes.Buffer{}, Msg{File: string(make([]byte, MaxFileLen+1))}); err == nil {
		t.Fatal("want file-too-long")
	}
	if err := Encode(&bytes.Buffer{}, Msg{Data: make([]byte, MaxDataLen+1)}); err == nil {
		t.Fatal("want data-too-large")
	}
	// 坏 op
	if _, err := Decode(bytes.NewReader([]byte{99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})); err == nil {
		t.Fatal("want bad op")
	}
	// fileLen 超限
	var hdr [15]byte
	hdr[0] = byte(OpFetchReq)
	hdr[1], hdr[2] = byte(MaxFileLen>>8)+1, 0
	if _, err := Decode(bytes.NewReader(hdr[:])); err == nil {
		t.Fatal("want file-too-long")
	}
	// dataLen 超限
	var hdr2 [15]byte
	hdr2[0] = byte(OpFetchReq)
	hdr2[11], hdr2[12], hdr2[13], hdr2[14] = 0xFF, 0xFF, 0xFF, 0xFF
	if _, err := Decode(bytes.NewReader(hdr2[:])); err == nil {
		t.Fatal("want data-too-large")
	}
	// 截断帧
	if _, err := Decode(bytes.NewReader([]byte{1, 0})); err == nil {
		t.Fatal("want truncated")
	}
}
