// Package disk 是家里大盘的区间读写：防路径逃逸，稀疏读零填充。
package disk

import (
	"errors"
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
