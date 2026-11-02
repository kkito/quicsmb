// Package disk 是家里大盘的区间读写：防路径逃逸，稀疏读零填充。
package disk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Root struct {
	root string
}

func New(root string) *Root {
	return &Root{root: root}
}

func (r *Root) resolve(file string) (string, error) {
	if file == "" || strings.HasPrefix(file, "/") {
		return "", errors.New("bad path: " + file)
	}
	clean := filepath.Clean(file)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "..\\") {
		return "", errors.New("path escape: " + file)
	}
	return filepath.Join(r.root, clean), nil
}

func (r *Root) ReadAt(file string, off int64, n int) ([]byte, error) {
	p, err := r.resolve(file)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	got := 0
	for got < n {
		m, err := f.ReadAt(out[got:], off+int64(got))
		got += m
		if err != nil {
			break
		}
	}
	return out, nil
}

func (r *Root) WriteAt(file string, off int64, data []byte) error {
	p, err := r.resolve(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(data, off)
	return err
}

func (r *Root) Abs() string { return r.root }

func (r *Root) Size(file string) int64 {
	p, err := r.resolve(file)
	if err != nil {
		return 0
	}
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

// FileEntry 是目录同步的一项：相对路径 + 大小。
type FileEntry struct {
	Name string
	Size int64
}

// List 递归扫 root 下所有常规文件，返回相对路径（slash 分隔）+ 大小。
// 忽略符号链接与非常规文件，避免逃逸。
func (r *Root) List() ([]FileEntry, error) {
	var out []FileEntry
	err := filepath.WalkDir(r.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 单个坏项跳过，不中断整表
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(r.root, p)
		if err != nil {
			return nil
		}
		out = append(out, FileEntry{Name: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	return out, err
}

// Truncate 把文件截到 size（回写时把块补零撑大的部分裁掉）。
func (r *Root) Truncate(file string, size int64) error {
	p, err := r.resolve(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}
