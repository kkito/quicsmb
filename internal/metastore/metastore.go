// Package metastore 持久化脏块元数据：脏时间、校验和、失败计数、块大小一致性。
package metastore

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type DirtyEntry struct {
	File      string `json:"file"`
	Index     uint64 `json:"index"`
	DirtyUnix int64  `json:"dirty_unix"`
	Sum       uint64 `json:"sum"`
	Failures  int    `json:"failures"`
}

type diskForm struct {
	BlockSize int              `json:"block_size"`
	Entries   []DirtyEntry     `json:"entries"`
	Files     map[string]int64 `json:"files,omitempty"`
}

type Store struct {
	mu        sync.Mutex
	path      string
	blockSize int
	items     map[string]*DirtyEntry
	files     map[string]int64
}

func key(file string, index uint64) string {
	return fmt.Sprintf("%s\x00%d", file, index)
}

func New(path string, blockSize int) *Store {
	return &Store{path: path, blockSize: blockSize, items: map[string]*DirtyEntry{}, files: map[string]int64{}}
}

// SetFile 记录文件 size（内存 + 落盘由 Save 统一做）。
func (s *Store) SetFile(file string, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if size > s.files[file] {
		s.files[file] = size
	}
}

// Files 快照：volume 启动恢复用。
func (s *Store) Files() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.files))
	for k, v := range s.files {
		out[k] = v
	}
	return out
}

// IsDirty 供 cache 淘汰保护查询。
func (s *Store) IsDirty(file string, index uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key(file, index)]
	return ok
}

func Load(path string, blockSize int) (*Store, error) {
	s := New(path, blockSize)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var f diskForm
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.BlockSize != 0 && f.BlockSize != blockSize {
		return nil, fmt.Errorf("block size changed: meta=%d want=%d (清空缓存后重启)", f.BlockSize, blockSize)
	}
	for _, e := range f.Entries {
		cp := e
		s.items[key(e.File, e.Index)] = &cp
	}
	s.files = f.Files
	if s.files == nil {
		s.files = map[string]int64{}
	}
	// 脏块反推文件存在（老版本 dirty.json 无 files 段时恢复列表）
	for _, e := range f.Entries {
		if _, ok := s.files[e.File]; !ok {
			s.files[e.File] = 0
		}
	}
	return s, nil
}

func (s *Store) MarkDirty(file string, index uint64, sum uint64, nowUnix int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(file, index)
	if e, ok := s.items[k]; ok {
		e.Sum = sum
		return
	}
	s.items[k] = &DirtyEntry{File: file, Index: index, DirtyUnix: nowUnix, Sum: sum}
}

func (s *Store) MarkClean(file string, index uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key(file, index))
}

func (s *Store) BumpFailure(file string, index uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.items[key(file, index)]; ok {
		e.Failures++
	}
}

func (s *Store) List() []DirtyEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DirtyEntry, 0, len(s.items))
	for _, e := range s.items {
		out = append(out, *e)
	}
	return out
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := diskForm{BlockSize: s.blockSize, Files: s.files}
	for _, e := range s.items {
		f.Entries = append(f.Entries, *e)
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
