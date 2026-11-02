package volume

import (
	"testing"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
	"quicsmb/internal/writeback"
)

// FlushDue 只吐到期脏块（TDD: 先红）
func TestFlushDue_SelectsDueBlocks(t *testing.T) {
	meta := metastore.New(t.TempDir()+"/d.json", chunk.DefaultBlockSize)
	vol := New(chunk.DefaultBlockSize, cache.New(20<<30, chunk.DefaultBlockSize), meta)
	now := int64(1000000)
	if _, err := vol.WriteAt("f.bin", 0, []byte("hello"), now-100000); err != nil {
		t.Fatal(err)
	}
	pol := writeback.Policy{DelaySec: 10, WinStartMin: 0, WinEndMin: 24 * 60, MaxFailures: 10}
	due := vol.FlushDue(pol, func() int64 { return now }, 32)
	if len(due) != 1 || due[0].File != "f.bin" || due[0].Index != 0 {
		t.Fatalf("want 1 due f.bin#0, got %+v", due)
	}
	if string(due[0].Data[:5]) != "hello" {
		t.Fatalf("data mismatch: %q", due[0].Data[:5])
	}
	// 未到期不吐
	pol.DelaySec = 72 * 3600
	if due := vol.FlushDue(pol, func() int64 { return now }, 32); len(due) != 0 {
		t.Fatalf("want 0 due before delay, got %d", len(due))
	}
}
