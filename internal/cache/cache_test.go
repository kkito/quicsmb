package cache

import (
	"testing"

	"quicsmb/internal/chunk"
)

func TestLRUEvictsOldest(t *testing.T) {
	c := New(2*512<<10, 512<<10)
	c.Put(chunk.ChunkID{File: "f", Index: 0}, make([]byte, 512<<10))
	c.Put(chunk.ChunkID{File: "f", Index: 1}, make([]byte, 512<<10))
	c.Put(chunk.ChunkID{File: "f", Index: 2}, make([]byte, 512<<10))
	if _, ok := c.Get(chunk.ChunkID{File: "f", Index: 0}); ok {
		t.Fatal("block 0 should be evicted")
	}
	if _, ok := c.Get(chunk.ChunkID{File: "f", Index: 2}); !ok {
		t.Fatal("block 2 should exist")
	}
	if used, n := c.Usage(); n != 2 || used != 2*512<<10 {
		t.Fatalf("usage = %d,%d", used, n)
	}
}

func TestHitRateAndHeat(t *testing.T) {
	c := New(4*512<<10, 512<<10)
	k := chunk.ChunkID{File: "f", Index: 0}
	c.Put(k, []byte("x"))
	_, _ = c.Get(k)
	if _, ok := c.Get(chunk.ChunkID{File: "f", Index: 9}); ok {
		t.Fatal("miss should be false")
	}
	if r := c.HitRate(); r != 0.5 {
		t.Fatalf("hitrate = %v", r)
	}
	if c.Heat(k) != 1 {
		t.Fatalf("heat = %d", c.Heat(k))
	}
	top := c.HeatTop(1)
	if len(top) != 1 || top[0] != k {
		t.Fatalf("top = %+v", top)
	}
}
