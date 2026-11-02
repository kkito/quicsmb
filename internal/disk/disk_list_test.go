package disk

import (
	"path/filepath"
	"testing"
)

func TestListRecursive(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	if err := r.WriteAt("a/b.bin", 0, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteAt("top.txt", 0, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, e := range list {
		got[e.Name] = e.Size
	}
	if got["a/b.bin"] != 5 || got["top.txt"] != 2 {
		t.Fatalf("list = %+v", list)
	}
	// 空目录不报错
	r2 := New(filepath.Join(t.TempDir(), "empty"))
	if list, err := r2.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty = %+v %v", list, err)
	}
}
