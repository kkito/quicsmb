package disk

import (
	"path/filepath"
	"testing"
)

func TestAbsAndWriteErrorPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	r := New(root)
	if r.Abs() != root {
		t.Fatalf("abs = %q want %q", r.Abs(), root)
	}
	// 读坏路径（目录被文件占用）→ error
	if err := r.WriteAt("x", 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAt("x/sub", 0, 1); err == nil {
		t.Fatal("want error reading under a file")
	}
	if err := r.WriteAt("x/sub", 0, []byte("y")); err == nil {
		t.Fatal("want error writing under a file")
	}
}

func TestWriteAtMkdirAllError(t *testing.T) {
	// root 下先占一个文件，再在其下建目录 → MkdirAll 失败
	r := New(filepath.Join(t.TempDir(), "root"))
	if err := r.WriteAt("blocker", 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteAt("blocker/sub/f", 0, []byte("y")); err == nil {
		t.Fatal("want MkdirAll error")
	}
}
