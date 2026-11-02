package cache

import (
	"testing"

	"quicsmb/internal/chunk"
)

func TestEvictSkipsDirty(t *testing.T) {
	c := New(4, 1) // 只能装 2 块
	c.Put(chunk.ChunkID{File: "a", Index: 0}, []byte("01"))
	c.Put(chunk.ChunkID{File: "b", Index: 0}, []byte("23"))
	c.SetIsDirty(func(k Key) bool { return k.File == "a" || k.File == "b" })
	// 全脏：再塞也淘汰不掉，超限保留
	c.Put(chunk.ChunkID{File: "c", Index: 0}, []byte("45"))
	if _, ok := c.Get(chunk.ChunkID{File: "a", Index: 0}); !ok {
		t.Fatal("dirty a must survive")
	}
	// b 变干净后可被淘汰
	c.SetIsDirty(func(k Key) bool { return k.File == "a" })
	c.Put(chunk.ChunkID{File: "d", Index: 0}, []byte("67"))
	if _, ok := c.Get(chunk.ChunkID{File: "b", Index: 0}); ok {
		t.Fatal("clean b should be evicted first")
	}
	if _, ok := c.Get(chunk.ChunkID{File: "a", Index: 0}); !ok {
		t.Fatal("dirty a must still survive")
	}
}
