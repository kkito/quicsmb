// Package cache 是内存 LRU 块缓存：配额淘汰、命中率、访问热度。
package cache

import (
	"container/list"
	"sort"
	"sync"

	"quicsmb/internal/chunk"
)

type Key = chunk.ChunkID

type entry struct {
	key  Key
	data []byte
	heat uint64
}

type Cache struct {
	mu    sync.Mutex
	cap   int64
	used  int64
	ll    *list.List
	items map[Key]*list.Element
	hits  uint64
	total uint64
}

func New(capBytes int64, blockSize int) *Cache {
	_ = blockSize
	return &Cache{cap: capBytes, ll: list.New(), items: map[Key]*list.Element{}}
}

func (c *Cache) Put(k Key, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.used += int64(len(data)) - int64(len(el.Value.(*entry).data))
		el.Value.(*entry).data = append([]byte(nil), data...)
		c.ll.MoveToFront(el)
		return
	}
	cp := append([]byte(nil), data...)
	el := c.ll.PushFront(&entry{key: k, data: cp})
	c.items[k] = el
	c.used += int64(len(cp))
	for c.used > c.cap {
		if !c.evictOldestLocked() {
			break
		}
	}
}

func (c *Cache) Get(k Key) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total++
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	c.hits++
	e := el.Value.(*entry)
	e.heat++
	c.ll.MoveToFront(el)
	return append([]byte(nil), e.data...), true
}

func (c *Cache) EvictOldest() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictOldestLocked()
}

func (c *Cache) evictOldestLocked() bool {
	back := c.ll.Back()
	if back == nil {
		return false
	}
	e := back.Value.(*entry)
	delete(c.items, e.key)
	c.used -= int64(len(e.data))
	c.ll.Remove(back)
	return true
}

func (c *Cache) Usage() (int64, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used, len(c.items)
}

func (c *Cache) HitRate() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.total == 0 {
		return 0
	}
	return float64(c.hits) / float64(c.total)
}

func (c *Cache) Heat(k Key) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		return el.Value.(*entry).heat
	}
	return 0
}

func (c *Cache) HeatTop(n int) []Key {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := make([]*entry, 0, len(c.items))
	for _, el := range c.items {
		all = append(all, el.Value.(*entry))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].heat > all[j].heat })
	if n > len(all) {
		n = len(all)
	}
	out := make([]Key, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, all[i].key)
	}
	return out
}
