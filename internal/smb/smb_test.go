package smb

import (
	"encoding/binary"
	"testing"
)

// negotiateReq 构造一个最小 NetBIOS+SMB2 NEGOTIATE 请求包。
func negotiateReq() []byte {
	h := make([]byte, 64)
	h[0], h[1], h[2], h[3] = 0xFE, 'S', 'M', 'B'
	binary.LittleEndian.PutUint16(h[12:14], CmdNegotiate)
	pkt := append([]byte{0x00, 0x00, 0x00, 0x40}, h...)
	return append(pkt, make([]byte, 6)...)
}

func TestSniffNegotiate(t *testing.T) {
	info, ok := Sniff(negotiateReq())
	if !ok || !info.Valid {
		t.Fatalf("sniff failed: %+v %v", info, ok)
	}
	if info.Cmd != CmdNegotiate {
		t.Fatalf("cmd = %04x", info.Cmd)
	}
	if CommandName(info.Cmd) != "NEGOTIATE" {
		t.Fatalf("name = %q", CommandName(info.Cmd))
	}
}

func TestSniffTooShort(t *testing.T) {
	if _, ok := Sniff([]byte{0x00, 0x01}); ok {
		t.Fatal("want false for short buf")
	}
}

func TestSniffBadMagic(t *testing.T) {
	b := negotiateReq()
	b[4] = 0x00
	if _, ok := Sniff(b); ok {
		t.Fatal("want false for bad magic")
	}
}

func TestCommandNames(t *testing.T) {
	if CommandName(CmdRead) != "READ" || CommandName(CmdWrite) != "WRITE" {
		t.Fatal("command names wrong")
	}
	if CommandName(0xFFFF) != "UNKNOWN" {
		t.Fatal("unknown should be UNKNOWN")
	}
}

func TestDialectNames(t *testing.T) {
	if DialectName(0x0311) != "SMB 3.1.1" {
		t.Fatal("dialect 0x0311 wrong")
	}
	if DialectName(0x0300) != "SMB 3.0" {
		t.Fatal("dialect 0x0300 wrong")
	}
}
