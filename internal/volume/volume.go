// Package volume 组装文件读写：cache 命中优先，未命中经 Fetcher 回源，
// 写走读-改-写整块并标脏。fetch 失败返回 error（调用方转 EIO）。
package volume

import (
	"fmt"
	"sync"

	"quicsmb/internal/writeback"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
)

type Fetcher func(file string, index uint64) ([]byte, error)

type Volume struct {
	mu        sync.RWMutex
	blockSize int
	cache     *cache.Cache
	meta      *metastore.Store
	sizes     map[string]int64
	capTotal  uint64
	capFree   uint64
}

// Files 快照：当前已知文件名→size（目录同步 + 本地创建 + 落盘恢复的并集）。
func (v *Volume) Files() map[string]int64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make(map[string]int64, len(v.sizes))
	for k, sz := range v.sizes {
		out[k] = sz
	}
	return out
}

// SetFile 记录远端文件（目录同步用；只放大不缩小，避免回写中的本地版本被覆盖）。
func (v *Volume) SetFile(file string, size int64) {
	if file == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if size > v.sizes[file] {
		v.sizes[file] = size
	}
}

func New(blockSize int, c *cache.Cache, meta *metastore.Store) *Volume {
	v := &Volume{blockSize: blockSize, cache: c, meta: meta, sizes: map[string]int64{}}
	for f, sz := range meta.Files() {
		v.sizes[f] = sz
	}
	c.SetIsDirty(func(k cache.Key) bool { return meta.IsDirty(k.File, k.Index) })
	return v
}

func (v *Volume) Size(file string) int64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.sizes[file]
}

func (v *Volume) block(file string, index uint64, fetch Fetcher) ([]byte, error) {
	k := chunk.ChunkID{File: file, Index: index}
	if b, ok := v.cache.Get(k); ok {
		return b, nil
	}
	b, err := fetch(file, index)
	if err != nil {
		return nil, fmt.Errorf("fetch %s#%d: %w", file, index, err)
	}
	if len(b) != v.blockSize {
		nb := make([]byte, v.blockSize)
		copy(nb, b)
		b = nb
	}
	v.cache.Put(k, b)
	return b, nil
}

func (v *Volume) ReadAt(file string, off int64, p []byte, fetch Fetcher) (int, error) {
	done := 0
	for done < len(p) {
		idx := chunk.IndexAt(off+int64(done), v.blockSize)
		b, err := v.block(file, idx, fetch)
		if err != nil {
			if done == 0 {
				return 0, err
			}
			return done, nil
		}
		boff := int((off + int64(done)) % int64(v.blockSize))
		m := copy(p[done:], b[boff:])
		done += m
		if m == 0 {
			break
		}
	}
	return done, nil
}

func (v *Volume) WriteAt(file string, off int64, p []byte, nowUnix int64) (int, error) {
	done := 0
	for done < len(p) {
		idx := chunk.IndexAt(off+int64(done), v.blockSize)
		k := chunk.ChunkID{File: file, Index: idx}
		var cur []byte
		if b, ok := v.cache.Get(k); ok {
			cur = b
		} else {
			cur = make([]byte, v.blockSize)
		}
		boff := int((off + int64(done)) % int64(v.blockSize))
		m := copy(cur[boff:], p[done:])
		v.cache.Put(k, cur)
		v.meta.MarkDirty(file, idx, chunk.Checksum(cur), nowUnix)
		done += m
		if m == 0 {
			break
		}
	}
	if end := off + int64(done); end > v.Size(file) {
		v.mu.Lock()
		if end > v.sizes[file] {
			v.sizes[file] = end
		}
		v.meta.SetFile(file, v.sizes[file])
		v.mu.Unlock()
	}
	return done, nil
}

func (v *Volume) Usage() (int64, int) { return v.cache.Usage() }

func (v *Volume) SetCapacity(total, free uint64) {
	v.capTotal, v.capFree = total, free
}

func (v *Volume) Capacity() (uint64, uint64) { return v.capTotal, v.capFree }
func (v *Volume) HitRate() float64 { return v.cache.HitRate() }

// DirtyBlock 是待回写的一块脏数据快照（调用方推送后 MarkClean）。
type DirtyBlock struct {
	File  string
	Index uint64
	Data  []byte
}

func (v *Volume) FlushDue(pol writeback.Policy, now writeback.Clock, limit int) []DirtyBlock {
	entries := v.meta.List()
	views := make([]writeback.DirtyEntryView, len(entries))
	for i, e := range entries {
		views[i] = writeback.DirtyEntryView{DirtyUnix: e.DirtyUnix, Failures: e.Failures}
	}
	var out []DirtyBlock
	for _, i := range writeback.SelectDue(views, pol, now, limit) {
		e := entries[i]
		b, ok := v.cache.Get(chunk.ChunkID{File: e.File, Index: e.Index})
		if !ok {
			continue // 块已被淘汰：跳过，留待下轮（校验和复核是后续工作）
		}
		out = append(out, DirtyBlock{File: e.File, Index: e.Index, Data: b})
	}
	return out
}
func (v *Volume) DirtyCount() int     { return len(v.meta.List()) }

func (v *Volume) Truncate(file string, size int64) {
	if size < 0 {
		size = 0
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sizes[file] = size
	v.meta.SetFile(file, size)
}

func (v *Volume) TouchDirty(file string, nowUnix int64) error {
	for idx := range v.dirtyBlocks(file) {
		v.meta.MarkDirty(file, idx, 0, nowUnix)
	}
	// 空洞文件（比如纯 O_TRUNC）：至少标第 0 块，保证回写能感知到它。
	if len(v.dirtyBlocks(file)) == 0 && file != "" {
		v.meta.MarkDirty(file, 0, 0, nowUnix)
	}
	return nil
}

func (v *Volume) dirtyBlocks(file string) map[uint64]bool {
	out := map[uint64]bool{}
	for _, e := range v.meta.List() {
		if e.File == file {
			out[e.Index] = true
		}
	}
	return out
}
